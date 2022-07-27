/*
Copyright 2022 Upbound Inc.
*/

package pipeline

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/pkg/errors"
	"sigs.k8s.io/yaml"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/registry/reference"
	tjtypes "github.com/upbound/upjet/pkg/types"
)

var (
	reFile = regexp.MustCompile(`file\("(.+)"\)`)
)

// ExampleGenerator represents a pipeline for generating example manifests.
// Generates example manifests for Terraform resources under examples-generated.
type ExampleGenerator struct {
	reference.Injector
	rootDir         string
	configResources map[string]*config.Resource
	resources       map[string]*reference.PavedWithManifest
}

// NewExampleGenerator returns a configured ExampleGenerator
func NewExampleGenerator(rootDir, modulePath, shortName string, configResources map[string]*config.Resource) *ExampleGenerator {
	return &ExampleGenerator{
		Injector: reference.Injector{
			ModulePath:        modulePath,
			ProviderShortName: shortName,
		},
		rootDir:         rootDir,
		configResources: configResources,
		resources:       make(map[string]*reference.PavedWithManifest),
	}
}

// StoreExamples stores the generated example manifests under examples-generated in
// their respective API groups.
func (eg *ExampleGenerator) StoreExamples() error {
	for n, pm := range eg.resources {
		if err := eg.ResolveReferencesOfPaved(pm, eg.resources); err != nil {
			return errors.Wrapf(err, "cannot resolve references for resource: %s", n)
		}
		u := pm.Paved.UnstructuredContent()
		delete(u["spec"].(map[string]interface{})["forProvider"].(map[string]interface{}), "depends_on")
		buff, err := yaml.Marshal(u)
		if err != nil {
			return errors.Wrapf(err, "cannot marshal example manifest for resource: %s", n)
		}
		manifestDir := filepath.Dir(pm.ManifestPath)
		if err := os.MkdirAll(manifestDir, 0750); err != nil {
			return errors.Wrapf(err, "cannot mkdir %s", manifestDir)
		}
		// no sensitive info in the example manifest
		if err := ioutil.WriteFile(pm.ManifestPath, buff, 0600); err != nil {
			return errors.Wrapf(err, "cannot write example manifest file %s for resource %s", pm.ManifestPath, n)
		}
	}
	return nil
}

// Generate generates an example manifest for the specified Terraform resource.
func (eg *ExampleGenerator) Generate(group, version string, r *config.Resource, fieldTransformations map[string]tjtypes.Transformation) error {
	rm := eg.configResources[r.Name].MetaResource
	if rm == nil || len(rm.Examples) == 0 {
		return nil
	}
	exampleParams := rm.Examples[0].Paved.UnstructuredContent()
	transformFields(exampleParams, r.ExternalName.OmittedFields, fieldTransformations, "")

	metadata := map[string]interface{}{
		"name": "example",
	}
	if len(rm.ExternalName) != 0 {
		metadata["annotations"] = map[string]string{
			xpmeta.AnnotationKeyExternalName: rm.ExternalName,
		}
	}
	example := map[string]interface{}{
		"apiVersion": fmt.Sprintf("%s/%s", group, version),
		"kind":       r.Kind,
		"metadata":   metadata,
		"spec": map[string]interface{}{
			"forProvider": exampleParams,
		},
	}
	manifestDir := filepath.Join(eg.rootDir, "examples-generated", strings.ToLower(strings.Split(group, ".")[0]))
	eg.resources[r.Name] = &reference.PavedWithManifest{
		ManifestPath: filepath.Join(manifestDir, fmt.Sprintf("%s.yaml", strings.ToLower(r.Kind))),
		Paved:        fieldpath.Pave(example),
		ParamsPrefix: []string{"spec", "forProvider"},
	}
	return nil
}

func getHierarchicalName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return fmt.Sprintf("%s.%s", prefix, name)
}

func transformFields(params map[string]interface{}, omittedFields []string, t map[string]tjtypes.Transformation, namePrefix string) { // nolint:gocyclo
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
			transformFields(pT, omittedFields, t, getHierarchicalName(namePrefix, n))

		case []interface{}:
			for _, e := range pT {
				eM, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				transformFields(eM, omittedFields, t, getHierarchicalName(namePrefix, n))
			}
		}
	}

	for n, v := range params {
		hName := getHierarchicalName(namePrefix, n)
		for hn, transform := range t {
			if hn != hName {
				continue
			}
			delete(params, n)
			switch {
			case !transform.IsRef:
				params[transform.TransformedName] = v
			case transform.IsSensitive:
				secretName, secretKey := getSecretRef(v)
				params[transform.TransformedName] = getRefField(v,
					map[string]interface{}{
						"name":      secretName,
						"namespace": "crossplane-system",
						"key":       secretKey,
					})
			default:
				params[transform.TransformedName] = getRefField(v,
					map[string]interface{}{
						"name": "example",
					})
			}
			break
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
	g := reference.ReRef.FindStringSubmatch(s)
	if len(g) != 2 {
		return secretName, secretKey
	}
	f := reFile.FindStringSubmatch(g[1])
	switch {
	case len(f) == 2: // then a file reference
		_, file := filepath.Split(f[1])
		secretKey = fmt.Sprintf("attribute.%s", file)
	default:
		parts := strings.Split(g[1], ".")
		if len(parts) < 3 {
			return secretName, secretKey
		}
		secretName = fmt.Sprintf("example-%s", strings.Join(strings.Split(parts[0], "_")[1:], "-"))
		secretKey = fmt.Sprintf("attribute.%s", strings.Join(parts[2:], "."))
	}
	return secretName, secretKey
}
