// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

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

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/config/conversion"
)

// ConvertSingletonListToEmbeddedObject generates the example manifests for
// the APIs with converted singleton lists in their new API versions with the
// embedded objects. All manifests under `startPath` are scanned and the
// header at the specified path `licenseHeaderPath` is used for the converted
// example manifests.
func ConvertSingletonListToEmbeddedObject(pc *config.Provider, startPath, licenseHeaderPath string) error {
	resourceRegistry := prepareResourceRegistry(pc)

	var license string
	var lErr error
	if licenseHeaderPath != "" {
		license, lErr = getLicenseHeader(licenseHeaderPath)
		if lErr != nil {
			return errors.Wrap(lErr, "failed to get license header")
		}
	}

	err := filepath.Walk(startPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return errors.Wrapf(err, "walk failed: %s", startPath)
		}

		var convertedFileContent string
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".yaml") {
			log.Printf("Converting: %s\n", path)
			content, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				return errors.Wrapf(err, "failed to read the %s file", path)
			}

			examples, err := decodeExamples(string(content))
			if err != nil {
				return errors.Wrap(err, "failed to decode examples")
			}

			rootResource := resourceRegistry[fmt.Sprintf("%s/%s", examples[0].GroupVersionKind().Kind, examples[0].GroupVersionKind().Group)]
			if rootResource == nil {
				log.Printf("Warning: Skipping %s because the corresponding resource could not be found in the provider", path)
				return nil
			}

			newPath := strings.ReplaceAll(path, examples[0].GroupVersionKind().Version, rootResource.Version)
			if path == newPath {
				return nil
			}
			annotationValue := strings.ToLower(fmt.Sprintf("%s/%s/%s", rootResource.ShortGroup, rootResource.Version, rootResource.Kind))
			for _, e := range examples {
				if resource, ok := resourceRegistry[fmt.Sprintf("%s/%s", e.GroupVersionKind().Kind, e.GroupVersionKind().Group)]; ok {
					conversionPaths := resource.CRDListConversionPaths()
					if conversionPaths != nil && e.GroupVersionKind().Version != resource.Version {
						for i, cp := range conversionPaths {
							// Here, for the manifests to be converted, only `forProvider
							// is converted, assuming the `initProvider` field is empty in the
							// spec.
							conversionPaths[i] = "spec.forProvider." + cp
						}
						converted, err := conversion.Convert(e.Object, conversionPaths, conversion.ToEmbeddedObject, nil)
						if err != nil {
							return errors.Wrapf(err, "failed to convert example to embedded object in manifest %s", path)
						}
						e.Object = converted
						e.SetGroupVersionKind(k8sschema.GroupVersionKind{
							Group:   e.GroupVersionKind().Group,
							Version: resource.Version,
							Kind:    e.GetKind(),
						})
					}
					annotations := e.GetAnnotations()
					if annotations == nil {
						annotations = make(map[string]string)
						log.Printf("Missing annotations: %s", path)
					}
					annotations["meta.upbound.io/example-id"] = annotationValue
					e.SetAnnotations(annotations)
				}
			}
			convertedFileContent = license + "\n\n"
			if err := writeExampleContent(path, convertedFileContent, examples, newPath); err != nil {
				return errors.Wrap(err, "failed to write example content")
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("Error walking the path %q: %v\n", startPath, err)
	}
	return nil
}

func writeExampleContent(path string, convertedFileContent string, examples []*unstructured.Unstructured, newPath string) error {
	for i, e := range examples {
		var convertedData []byte
		e := e
		convertedData, err := yaml.Marshal(&e)
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
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return errors.Wrap(err, "failed to create directory")
	}
	f, err := os.Create(filepath.Clean(newPath))
	if err != nil {
		return errors.Wrap(err, "failed to create file")
	}
	if _, err := f.WriteString(convertedFileContent); err != nil {
		return errors.Wrap(err, "failed to write to file")
	}
	log.Printf("Converted: %s\n", path)
	return nil
}

func getLicenseHeader(licensePath string) (string, error) {
	licenseData, err := os.ReadFile(licensePath)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read license file: %s", licensePath)
	}

	return string(licenseData), nil
}

func prepareResourceRegistry(pc *config.Provider) map[string]*config.Resource {
	reg := map[string]*config.Resource{}
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
