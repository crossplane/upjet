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
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/config/conversion"
	"github.com/crossplane/upjet/v2/pkg/schema/traverser"
	"github.com/crossplane/upjet/v2/pkg/types/conversion/tfjson"
)

// schemaTypeObjectCollector is a schema traverser that collects the CRD field
// paths for SchemaTypeObject (NestingModeSingle) fields. These fields are
// represented as embedded objects (pointer-to-struct) in the CRD but may appear
// as single-element arrays in scraped Terraform example configurations.
type schemaTypeObjectCollector struct {
	traverser.NoopTraverser
	paths []string
}

func (c *schemaTypeObjectCollector) VisitResource(r *traverser.ResourceNode) error {
	if r.Schema.Type == tfjson.SchemaTypeObject {
		c.paths = append(c.paths, traverser.FieldPathWithWildcard(r.CRDPath))
	}
	return nil
}

// collectSchemaTypeObjectCRDPaths returns the CRD field paths for all
// SchemaTypeObject fields in the given resource's Terraform schema.
func collectSchemaTypeObjectCRDPaths(r *config.Resource) ([]string, error) {
	collector := &schemaTypeObjectCollector{}
	if err := traverser.Traverse(r.Name, r.TerraformResource, collector); err != nil {
		return nil, errors.Wrapf(err, "failed to traverse the schema of resource %s", r.Name)
	}
	return collector.paths, nil
}

// flattenSchemaTypeObjectExamples flattens single-element arrays at the given
// field paths to plain objects in the provided unstructured object.
// Unlike conversion.Convert, this function is lenient: it silently skips paths
// that don't exist or whose values are not single-element arrays.
// This is necessary because conversion.Convert returns an error for non-slice
// values, which is correct for runtime conversions but too strict for example
// manifests where the value may not be present or may already be an object.
func flattenSchemaTypeObjectExamples(obj map[string]any, paths []string) {
	if len(paths) == 0 {
		return
	}
	// Sort in lexical order so parents are flattened before children.
	// This is necessary because SchemaTypeObject paths do NOT use [*]
	// wildcards (unlike TypeList/TypeSet paths). Without wildcards,
	// child paths like "outer.inner" can't be resolved while "outer" is
	// still an array. Flattening parents first converts them to objects,
	// making child paths accessible.
	sortedPaths := append([]string(nil), paths...)
	sort.Strings(sortedPaths)
	pv := fieldpath.Pave(obj)
	for _, fp := range sortedPaths {
		exp, err := pv.ExpandWildcards(fp)
		if err != nil {
			// Path doesn't exist in the object, skip.
			continue
		}
		for _, e := range exp {
			flattenSingleElementArray(pv, e)
		}
	}
}

// flattenSingleElementArray replaces a single-element array at the given
// concrete field path with its sole element. It is a no-op if the value
// does not exist, is not a []any, or does not have exactly one element.
func flattenSingleElementArray(pv *fieldpath.Paved, path string) {
	v, err := pv.GetValue(path)
	if err != nil {
		return
	}
	s, ok := v.([]any)
	if !ok || len(s) != 1 {
		return
	}
	// Set the value to the single element by accessing the parent map
	// directly. This avoids type coercion that fieldpath.Paved.SetValue
	// might perform.
	segments := strings.Split(path, ".")
	key := segments[len(segments)-1]
	var parentVal any = pv.UnstructuredContent()
	if len(segments) > 1 {
		parentPath := strings.Join(segments[:len(segments)-1], ".")
		parentVal, err = pv.GetValue(parentPath)
		if err != nil {
			return
		}
	}
	parent, ok := parentVal.(map[string]any)
	if !ok {
		return
	}
	parent[key] = s[0]
}

// ApplyAPIConverters applies the registered converters to generated
// example manifests under the given root directory.
// All (generated) manifests under the `startPath` are scanned and the
// header at the specified path `licenseHeaderPath` is used for the converted
// example manifests.
func ApplyAPIConverters(pc *config.Provider, startPath, licenseHeaderPath string) error { //nolint:gocyclo // easier to follow as a unit
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

			annotationValue := strings.ToLower(fmt.Sprintf("%s/%s/%s", rootResource.ShortGroup, rootResource.Version, rootResource.Kind))
			for _, e := range examples {
				if resource, ok := resourceRegistry[fmt.Sprintf("%s/%s", e.GroupVersionKind().Kind, e.GroupVersionKind().Group)]; ok {
					if e.GroupVersionKind().Version == resource.Version {
						conversionPaths := resource.CRDListConversionPaths()
						// if the latest version has conversions, run the conversions on the
						// example manifest.
						// Please note that only the version being generated (latest version)
						// is processed.
						if conversionPaths != nil {
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
						}

						// Also flatten SchemaTypeObject (NestingModeSingle) fields.
						// These fields are generated as embedded objects in the CRD
						// but the HCL-to-JSON scraper wraps them in single-element
						// arrays per HCL spec.
						objectPaths, err := collectSchemaTypeObjectCRDPaths(resource)
						if err != nil {
							return errors.Wrapf(err, "failed to collect SchemaTypeObject paths for resource %s", resource.Name)
						}
						if len(objectPaths) > 0 {
							for i, op := range objectPaths {
								objectPaths[i] = "spec.forProvider." + op
							}
							flattenSchemaTypeObjectExamples(e.Object, objectPaths)
						}

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
			if err := writeExampleContent(path, convertedFileContent, examples, path); err != nil {
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
	if err := os.MkdirAll(dir, os.ModePerm); err != nil { //nolint:gosec
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
	licenseData, err := os.ReadFile(licensePath) //nolint:gosec // used only on generation time
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
