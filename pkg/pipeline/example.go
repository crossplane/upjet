/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pipeline

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
	"sigs.k8s.io/yaml"

	"github.com/crossplane/terrajet/pkg/config"
	"github.com/crossplane/terrajet/pkg/resource/json"
	tjtypes "github.com/crossplane/terrajet/pkg/types"
)

var (
	reRef = regexp.MustCompile(`\${(.+)}`)
)

type pavedWithManifest struct {
	manifestPath string
	paved        *fieldpath.Paved
}

// ResourceExample represents the scraped example HCL configuration
// for a Terraform resource
type ResourceExample struct {
	Manifest   string            `yaml:"manifest"`
	References map[string]string `yaml:"references,omitempty"`
}

// Resource represents the scraped metadata for a Terraform resource
type Resource struct {
	SubCategory      string            `yaml:"subCategory"`
	Description      string            `yaml:"description,omitempty"`
	Name             string            `yaml:"name"`
	TitleName        string            `yaml:"titleName"`
	Examples         []ResourceExample `yaml:"examples,omitempty"`
	ArgumentDocs     map[string]string `yaml:"argumentDocs"`
	ImportStatements []string          `yaml:"importStatements"`
}

// ProviderMetadata metadata for a Terraform native provider
type ProviderMetadata struct {
	Name      string               `yaml:"name"`
	Resources map[string]*Resource `yaml:"resources"`
}

// NewProviderMetadataFromFile loads metadata from the specified YAML-formatted file
func NewProviderMetadataFromFile(path string) (*ProviderMetadata, error) {
	buff, err := ioutil.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read metadata file %q", path)
	}

	metadata := &ProviderMetadata{}
	return metadata, errors.Wrap(yaml.Unmarshal(buff, metadata), "failed to unmarshal provider metadata")
}

// ExampleGenerator represents a pipeline for generating example manifests.
// Generates example manifests for Terraform resources under examples-generated.
type ExampleGenerator struct {
	rootDir      string
	resourceMeta map[string]*Resource
	resources    map[string]*pavedWithManifest
}

// NewExampleGenerator returns a configured ExampleGenerator
func NewExampleGenerator(rootDir string, resourceMeta map[string]*Resource) *ExampleGenerator {
	return &ExampleGenerator{
		rootDir:      rootDir,
		resourceMeta: resourceMeta,
		resources:    make(map[string]*pavedWithManifest),
	}
}

// StoreExamples stores the generated example manifests under examples-generated in
// their respective API groups.
func (eg *ExampleGenerator) StoreExamples() error {
	for n, pm := range eg.resources {
		if err := eg.resolveReferences(pm.paved.UnstructuredContent()); err != nil {
			return errors.Wrapf(err, "cannot resolve references for resource: %s", n)
		}
		buff, err := yaml.Marshal(pm.paved.UnstructuredContent())
		if err != nil {
			return errors.Wrapf(err, "cannot marshal example manifest for resource: %s", n)
		}
		manifestDir := filepath.Dir(pm.manifestPath)
		if err := os.MkdirAll(manifestDir, 0750); err != nil {
			return errors.Wrapf(err, "cannot mkdir %s", manifestDir)
		}

		b := bytes.Buffer{}
		b.WriteString("# This example manifest is auto-generated, and has not been tested.\n")
		b.WriteString("# Please make the necessary adjustments before using it.\n")
		b.Write(buff)
		// no sensitive info in the example manifest
		if err := ioutil.WriteFile(pm.manifestPath, b.Bytes(), 0644); err != nil { // nolint:gosec
			return errors.Wrapf(err, "cannot write example manifest file %s for resource %s", pm.manifestPath, n)
		}
	}
	return nil
}

func (eg *ExampleGenerator) resolveReferences(params map[string]interface{}) error { // nolint:gocyclo
	for k, v := range params {
		switch t := v.(type) {
		case map[string]interface{}:
			if err := eg.resolveReferences(t); err != nil {
				return err
			}

		case []interface{}:
			for _, e := range t {
				eM, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				if err := eg.resolveReferences(eM); err != nil {
					return err
				}
			}

		case string:
			g := reRef.FindStringSubmatch(t)
			if len(g) != 2 {
				continue
			}
			path := strings.Split(g[1], ".")
			// expected reference format is <resource type>.<resource name>.<field name>
			if len(path) < 3 {
				continue
			}
			pm := eg.resources[path[0]]
			if pm == nil || pm.paved == nil {
				continue
			}
			pathStr := strings.Join(append([]string{"spec", "forProvider"}, path[2:]...), ".")
			s, err := pm.paved.GetString(pathStr)
			if fieldpath.IsNotFound(err) {
				continue
			}
			if err != nil {
				return errors.Wrapf(err, "cannot get string value from paved: %s", pathStr)
			}
			params[k] = s
		}
	}
	return nil
}

// Generate generates an example manifest for the specified Terraform resource.
func (eg *ExampleGenerator) Generate(group, version string, r *config.Resource, fieldTransformations map[string]tjtypes.Transformation) error {
	rm := eg.resourceMeta[r.Name]
	if rm == nil || len(rm.Examples) == 0 {
		return nil
	}
	var exampleParams map[string]interface{}
	if err := json.TFParser.Unmarshal([]byte(rm.Examples[0].Manifest), &exampleParams); err != nil {
		return errors.Wrapf(err, "cannot unmarshal example manifest for resource: %s", r.Name)
	}
	transformRefFields(exampleParams, r.ExternalName.OmittedFields, fieldTransformations, "")

	example := map[string]interface{}{
		"apiVersion": fmt.Sprintf("%s/%s", group, version),
		"kind":       r.Kind,
		"metadata": map[string]interface{}{
			"name": "example",
		},
		"spec": map[string]interface{}{
			"forProvider": exampleParams,
			"providerConfigRef": map[string]interface{}{
				"name": "example",
			},
		},
	}
	manifestDir := filepath.Join(eg.rootDir, "examples-generated", strings.ToLower(strings.Split(group, ".")[0]))
	eg.resources[r.Name] = &pavedWithManifest{
		manifestPath: filepath.Join(manifestDir, fmt.Sprintf("%s.yaml", strings.ToLower(r.Kind))),
		paved:        fieldpath.Pave(example),
	}
	return nil
}

func getHierarchicalName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return fmt.Sprintf("%s.%s", prefix, name)
}

func transformRefFields(params map[string]interface{}, omittedFields []string, t map[string]tjtypes.Transformation, namePrefix string) { // nolint:gocyclo
	for _, hn := range omittedFields {
		for n := range params {
			if hn == getHierarchicalName(namePrefix, n) {
				delete(params, n)
				break
			}
		}
	}

	for n, v := range params {
		switch pT := v.(type) {
		case map[string]interface{}:
			transformRefFields(pT, omittedFields, t, getHierarchicalName(namePrefix, n))

		case []interface{}:
			for _, e := range pT {
				eM, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				transformRefFields(eM, omittedFields, t, getHierarchicalName(namePrefix, n))
			}
		}
	}

	for hn, transform := range t {
		for n, v := range params {
			if hn == getHierarchicalName(namePrefix, n) {
				delete(params, n)
				if transform.IsRef {
					if !transform.IsSensitive {
						params[transform.TransformedName] = getRefField(v,
							map[string]interface{}{
								"name": "example",
							})
					} else {
						secretName, secretKey := getSecretRef(v)
						params[transform.TransformedName] = getRefField(v,
							map[string]interface{}{
								"name":      secretName,
								"namespace": "crossplane-system",
								"key":       secretKey,
							})
					}
				} else {
					params[transform.TransformedName] = v
				}
				break
			}
		}
	}
}

func getRefField(v interface{}, ref map[string]interface{}) interface{} {
	switch v.(type) {
	case []interface{}:
		return []interface{}{
			ref,
		}

	default:
		return ref
	}
}

func getSecretRef(v interface{}) (string, string) {
	secretName := "example-secret"
	secretKey := "example-key"
	s, ok := v.(string)
	if !ok {
		return secretName, secretKey
	}
	g := reRef.FindStringSubmatch(s)
	if len(g) != 2 {
		return secretName, secretKey
	}
	parts := strings.Split(g[1], ".")
	if len(parts) < 3 {
		return secretName, secretKey
	}
	secretName = fmt.Sprintf("example-%s", strings.Join(strings.Split(parts[0], "_")[1:], "-"))
	secretKey = fmt.Sprintf("attribute.%s", strings.Join(parts[2:], "."))
	return secretName, secretKey
}
