/*
Copyright 2022 Upbound Inc.
*/

package examples

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"
	"sigs.k8s.io/yaml"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/registry/reference"
	"github.com/upbound/upjet/pkg/resource/json"
	tjtypes "github.com/upbound/upjet/pkg/types"
	"github.com/upbound/upjet/pkg/types/name"
)

var (
	reFile = regexp.MustCompile(`file\("(.+)"\)`)
)

const (
	labelExampleName       = "testing.upbound.io/example-name"
	annotationExampleGroup = "meta.upbound.io/example-id"
	defaultExampleName     = "example"
	defaultNamespace       = "upbound-system"
)

// Generator represents a pipeline for generating example manifests.
// Generates example manifests for Terraform resources under examples-generated.
type Generator struct {
	reference.Injector
	rootDir         string
	configResources map[string]*config.Resource
	resources       map[string]*reference.PavedWithManifest
}

// NewGenerator returns a configured Generator
func NewGenerator(rootDir, modulePath, shortName string, configResources map[string]*config.Resource) *Generator {
	return &Generator{
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
func (eg *Generator) StoreExamples() error { // nolint:gocyclo
	for rn, pm := range eg.resources {
		manifestDir := filepath.Dir(pm.ManifestPath)
		if err := os.MkdirAll(manifestDir, 0750); err != nil {
			return errors.Wrapf(err, "cannot mkdir %s", manifestDir)
		}
		var buff bytes.Buffer
		if err := eg.writeManifest(&buff, pm, &reference.ResolutionContext{
			WildcardNames: true,
			Context:       eg.resources,
		}); err != nil {
			return errors.Wrapf(err, "cannot store example manifest for resource: %s", rn)
		}
		if r, ok := eg.configResources[reference.NewRefPartsFromResourceName(rn).Resource]; ok && r.MetaResource != nil {
			re := r.MetaResource.Examples[0]
			context, err := reference.PrepareLocalResolutionContext(re, reference.NewRefParts(reference.NewRefPartsFromResourceName(rn).Resource, re.Name).GetResourceName(false))
			if err != nil {
				return errors.Wrapf(err, "cannot prepare local resolution context for resource: %s", rn)
			}
			dKeys := make([]string, 0, len(re.Dependencies))
			for k := range re.Dependencies {
				dKeys = append(dKeys, k)
			}
			sort.Strings(dKeys)
			for _, dn := range dKeys {
				dr, ok := eg.resources[reference.NewRefPartsFromResourceName(dn).GetResourceName(true)]
				if !ok {
					continue
				}
				var exampleParams map[string]any
				if err := json.TFParser.Unmarshal([]byte(re.Dependencies[dn]), &exampleParams); err != nil {
					return errors.Wrapf(err, "cannot unmarshal example manifest for resource: %s", dr.Config.Name)
				}
				// e.g. meta.upbound.io/example-id: ec2/v1beta1/instance
				eGroup := fmt.Sprintf("%s/%s/%s", strings.ToLower(r.ShortGroup), r.Version, strings.ToLower(r.Kind))
				pmd := paveCRManifest(exampleParams, dr.Config,
					reference.NewRefPartsFromResourceName(dn).ExampleName, dr.Group, dr.Version, eGroup)
				if err := eg.writeManifest(&buff, pmd, context); err != nil {
					return errors.Wrapf(err, "cannot store example manifest for %s dependency: %s", rn, dn)
				}
			}
		}
		// no sensitive info in the example manifest
		if err := ioutil.WriteFile(pm.ManifestPath, buff.Bytes(), 0600); err != nil {
			return errors.Wrapf(err, "cannot write example manifest file %s for resource %s", pm.ManifestPath, rn)
		}
	}
	return nil
}

func paveCRManifest(exampleParams map[string]any, r *config.Resource, eName, group, version, eGroup string) *reference.PavedWithManifest {
	delete(exampleParams, "depends_on")
	delete(exampleParams, "lifecycle")
	transformFields(r, exampleParams, r.ExternalName.OmittedFields, "")
	metadata := map[string]any{
		"labels": map[string]string{
			labelExampleName: eName,
		},
		"annotations": map[string]string{
			annotationExampleGroup: eGroup,
		},
	}
	example := map[string]any{
		"apiVersion": fmt.Sprintf("%s/%s", group, version),
		"kind":       r.Kind,
		"metadata":   metadata,
		"spec": map[string]any{
			"forProvider": exampleParams,
		},
	}
	if len(r.MetaResource.ExternalName) != 0 {
		metadata["annotations"].(map[string]string)[xpmeta.AnnotationKeyExternalName] = r.MetaResource.ExternalName
	}
	return &reference.PavedWithManifest{
		Paved:        fieldpath.Pave(example),
		ParamsPrefix: []string{"spec", "forProvider"},
		Config:       r,
		Group:        group,
		Version:      version,
	}
}

func dns1123Name(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "_", "-")
}

func (eg *Generator) writeManifest(writer io.Writer, pm *reference.PavedWithManifest, resolutionContext *reference.ResolutionContext) error {
	if err := eg.ResolveReferencesOfPaved(pm, resolutionContext); err != nil {
		return errors.Wrap(err, "cannot resolve references of resource")
	}
	labels, err := pm.Paved.GetValue("metadata.labels")
	if err != nil {
		return errors.Wrap(err, `cannot get "metadata.labels" from paved`)
	}
	pm.ExampleName = dns1123Name(labels.(map[string]string)[labelExampleName])
	if err := pm.Paved.SetValue("metadata.name", pm.ExampleName); err != nil {
		return errors.Wrapf(err, `cannot set "metadata.name" for resource %q:%s`, pm.Config.Name, pm.ExampleName)
	}
	u := pm.Paved.UnstructuredContent()
	buff, err := yaml.Marshal(u)
	if err != nil {
		return errors.Wrap(err, "cannot marshal example resource manifest")
	}
	if _, err := writer.Write(buff); err != nil {
		return errors.Wrap(err, "cannot write resource manifest to the underlying stream")
	}
	_, err = writer.Write([]byte("\n---\n\n"))
	return errors.Wrap(err, "cannot write YAML document separator to the underlying stream")
}

// Generate generates an example manifest for the specified Terraform resource.
func (eg *Generator) Generate(group, version string, r *config.Resource) error {
	rm := eg.configResources[r.Name].MetaResource
	if rm == nil || len(rm.Examples) == 0 {
		return nil
	}
	groupPrefix := strings.ToLower(strings.Split(group, ".")[0])
	// e.g. gvk = ec2/v1beta1/instance
	gvk := fmt.Sprintf("%s/%s/%s", groupPrefix, version, strings.ToLower(r.Kind))
	pm := paveCRManifest(rm.Examples[0].Paved.UnstructuredContent(), r, rm.Examples[0].Name, group, version, gvk)
	manifestDir := filepath.Join(eg.rootDir, "examples-generated", groupPrefix)
	pm.ManifestPath = filepath.Join(manifestDir, fmt.Sprintf("%s.yaml", strings.ToLower(r.Kind)))
	eg.resources[fmt.Sprintf("%s.%s", r.Name, reference.Wildcard)] = pm
	return nil
}

func getHierarchicalName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return fmt.Sprintf("%s.%s", prefix, name)
}

func isStatus(r *config.Resource, attr string) bool {
	s := config.GetSchema(r.TerraformResource, attr)
	if s == nil {
		return false
	}
	return tjtypes.IsObservation(s)
}

func transformFields(r *config.Resource, params map[string]any, omittedFields []string, namePrefix string) { // nolint:gocyclo
	for n := range params {
		hName := getHierarchicalName(namePrefix, n)
		if isStatus(r, hName) {
			delete(params, n)
			continue
		}
		for _, hn := range omittedFields {
			if hn == hName {
				delete(params, n)
				break
			}
		}
	}

	for n, v := range params {
		switch pT := v.(type) {
		case map[string]any:
			transformFields(r, pT, omittedFields, getHierarchicalName(namePrefix, n))

		case []any:
			for _, e := range pT {
				eM, ok := e.(map[string]any)
				if !ok {
					continue
				}
				transformFields(r, eM, omittedFields, getHierarchicalName(namePrefix, n))
			}
		}
	}

	for n, v := range params {
		fieldPath := getHierarchicalName(namePrefix, n)
		sch := config.GetSchema(r.TerraformResource, fieldPath)
		if sch == nil {
			continue
		}
		// At this point, we confirmed that the field is part of the schema,
		// so we'll need to perform at least name change on it.
		delete(params, n)
		fn := name.NewFromSnake(n)
		switch {
		case sch.Sensitive:
			secretName, secretKey := getSecretRef(v)
			params[fn.LowerCamelComputed+"SecretRef"] = getRefField(v, map[string]any{
				"name":      secretName,
				"namespace": defaultNamespace,
				"key":       secretKey,
			})
		case r.References[fieldPath] != config.Reference{}:
			switch v.(type) {
			case []any:
				l := sch.Type == schema.TypeList || sch.Type == schema.TypeSet
				ref := name.ReferenceFieldName(fn, l, r.References[fieldPath].RefFieldName)
				params[ref.LowerCamelComputed] = getNameRefField(v)
			default:
				sel := name.SelectorFieldName(fn, r.References[fieldPath].SelectorFieldName)
				params[sel.LowerCamelComputed] = getSelectorField(v)
			}
		default:
			params[fn.LowerCamelComputed] = v
		}
	}
}

func getNameRefField(v any) any {
	arr := v.([]any)
	refArr := make([]map[string]any, len(arr))
	for i, r := range arr {
		refArr[i] = map[string]any{
			"name": defaultExampleName,
		}
		if parts := reference.MatchRefParts(fmt.Sprintf("%v", r)); parts != nil {
			refArr[i]["name"] = parts.ExampleName
		}
	}
	return refArr
}

func getSelectorField(refVal any) any {
	ref := map[string]string{
		labelExampleName: defaultExampleName,
	}
	if parts := reference.MatchRefParts(fmt.Sprintf("%v", refVal)); parts != nil {
		ref[labelExampleName] = parts.ExampleName
	}
	return map[string]any{
		"matchLabels": ref,
	}
}

func getRefField(v any, ref map[string]any) any {
	switch v.(type) {
	case []any:
		return []any{
			ref,
		}

	default:
		return ref
	}
}

func getSecretRef(v any) (string, string) {
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
