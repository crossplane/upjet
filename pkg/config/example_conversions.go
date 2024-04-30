// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/crossplane/upjet/pkg/config/conversion"
)

func ConvertSingletonListToEmbeddedObject(pc *Provider, startPath string) error { //nolint:gocyclo
	resourceRegistry := prepareResourceRegistry(pc)
	err := filepath.Walk(startPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return errors.Wrap(err, "walk failed")
		}

		var convertedFileContent string
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".yaml") {
			log.Printf("Converting: %s\n", path)
			content, err := os.ReadFile(path) //nolint:gosec
			if err != nil {
				return errors.Wrap(err, "failed to read file")
			}

			examples, err := decodeExamples(string(content))
			if err != nil {
				return errors.Wrap(err, "failed to decode examples")
			}

			rootResource := resourceRegistry[fmt.Sprintf("%s/%s", examples[0].GroupVersionKind().Kind, examples[0].GroupVersionKind().Group)]
			if rootResource == nil {
				return nil
			}

			newPath := strings.Replace(path, examples[0].GroupVersionKind().Version, rootResource.Version, -1) //nolint:gocritic
			if path == newPath {
				return nil
			}
			annotationValue := strings.ToLower(fmt.Sprintf("%s/%s/%s", rootResource.ShortGroup, rootResource.Version, rootResource.Kind))
			for _, e := range examples {
				if resource, ok := resourceRegistry[fmt.Sprintf("%s/%s", e.GroupVersionKind().Kind, e.GroupVersionKind().Group)]; ok {
					conversionPaths := resource.CRDListConversionPaths()
					if conversionPaths != nil && e.GroupVersionKind().Version != resource.Version {
						for i, cp := range conversionPaths {
							conversionPaths[i] = "spec.forProvider." + cp
						}
						converted, err := conversion.Convert(e.Object, conversionPaths, conversion.ToEmbeddedObject)
						if err != nil {
							return errors.Wrap(err, "failed to convert example to embedded object")
						}
						e.Object = converted
						e.SetGroupVersionKind(k8sschema.GroupVersionKind{
							Group:   e.GroupVersionKind().Group,
							Version: resource.Version,
							Kind:    e.GetKind(),
						})
					}
					annotations := e.GetAnnotations()
					annotations["meta.upbound.io/example-id"] = annotationValue
					e.SetAnnotations(annotations)
				}
			}
			convertedFileContent = "# SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>\n#\n# SPDX-License-Identifier: CC0-1.0\n\n"
			for i, e := range examples {
				var convertedData []byte
				convertedData, err := yaml.Marshal(&e) //nolint:gosec
				if err != nil {
					return errors.Wrap(err, "failed to marshal example to yaml")
				}
				if i == len(examples)-1 {
					convertedFileContent += string(convertedData)
				} else {
					convertedFileContent += string(convertedData) + "\n---\n\n"
				}
			}
			dir := filepath.Dir(newPath)

			// Create all necessary directories if they do not exist
			err = os.MkdirAll(dir, os.ModePerm)
			if err != nil {
				return errors.Wrap(err, "failed to create directory")
			}
			f, err := os.Create(newPath) //nolint:gosec
			if err != nil {
				return errors.Wrap(err, "failed to create file")
			}
			if _, err := f.WriteString(convertedFileContent); err != nil {
				return errors.Wrap(err, "failed to write to file")
			}
			log.Printf("Converted: %s\n", path)
		}
		return nil
	})
	if err != nil {
		log.Printf("Error walking the path %q: %v\n", startPath, err)
	}
	return nil
}

func prepareResourceRegistry(pc *Provider) map[string]*Resource {
	reg := map[string]*Resource{}
	for _, r := range pc.Resources {
		reg[fmt.Sprintf("%s/%s.%s", r.Kind, r.ShortGroup, pc.RootGroup)] = r
	}
	return reg
}

func decodeExamples(content string) ([]*unstructured.Unstructured, error) {
	var manifests []*unstructured.Unstructured
	decoder := kyaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(content), 1024)
	for {
		u := &unstructured.Unstructured{}
		if err := decoder.Decode(&u); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, errors.Wrap(err, "cannot decode manifest")
		}
		if u != nil {
			manifests = append(manifests, u)
		}
	}
	return manifests, nil
}
