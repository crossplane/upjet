/*
Copyright 2022 Upbound Inc.
*/

package reference

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/registry"
	"github.com/upbound/upjet/pkg/resource/json"
)

var (
	// ReRef represents a regular expression for Terraform resource references
	// in scraped HCL example manifests.
	ReRef = regexp.MustCompile(`\${(.+)}`)
)

// Parts represents the components (resource name, example name &
// attribute name) parsed from an HCL reference.
type Parts struct {
	Resource    string
	ExampleName string
	Attribute   string
}

func matchRefParts(ref string) *Parts {
	g := ReRef.FindStringSubmatch(ref)
	if len(g) != 2 {
		return nil
	}
	return getRefParts(g[1])
}

func getRefParts(ref string) *Parts {
	parts := strings.Split(ref, ".")
	// expected reference format is <resource type>.<resource name>.<field name>
	if len(parts) < 3 {
		return nil
	}
	return &Parts{
		Resource:    parts[0],
		ExampleName: parts[1],
		Attribute:   strings.Join(parts[2:], "."),
	}
}

func (parts *Parts) getResourceAttr() string {
	return fmt.Sprintf("%s.%s", parts.Resource, parts.Attribute)
}

// PavedWithManifest represents an example manifest with a fieldpath.Paved
type PavedWithManifest struct {
	Paved        *fieldpath.Paved
	ManifestPath string
	ParamsPrefix []string
	refsResolved bool
}

func paveExampleManifest(m string) (*PavedWithManifest, error) {
	var exampleParams map[string]interface{}
	if err := json.TFParser.Unmarshal([]byte(m), &exampleParams); err != nil {
		return nil, errors.Wrapf(err, "cannot unmarshal example manifest: %s", m)
	}
	return &PavedWithManifest{
		Paved: fieldpath.Pave(exampleParams),
	}, nil
}

// ResolveReferencesOfPaved resolves references of a PavedWithManifest
// in the given resolution context.
func (rr *Injector) ResolveReferencesOfPaved(pm *PavedWithManifest, resolutionContext map[string]*PavedWithManifest) error {
	if pm.refsResolved {
		return nil
	}
	pm.refsResolved = true
	return errors.Wrap(rr.resolveReferences(pm.Paved.UnstructuredContent(), resolutionContext), "failed to resolve references of paved")
}

func (rr *Injector) resolveReferences(params map[string]interface{}, resolutionContext map[string]*PavedWithManifest) error { // nolint:gocyclo
	for paramName, paramValue := range params {
		switch t := paramValue.(type) {
		case map[string]interface{}:
			if err := rr.resolveReferences(t, resolutionContext); err != nil {
				return err
			}

		case []interface{}:
			for _, e := range t {
				eM, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				if err := rr.resolveReferences(eM, resolutionContext); err != nil {
					return err
				}
			}

		case string:
			parts := matchRefParts(t)
			if parts == nil {
				continue
			}
			pm := resolutionContext[parts.Resource]
			if pm == nil || pm.Paved == nil {
				continue
			}
			if err := rr.ResolveReferencesOfPaved(pm, resolutionContext); err != nil {
				return errors.Wrapf(err, "cannot recursively resolve references for %q", parts.Resource)
			}
			pathStr := strings.Join(append(pm.ParamsPrefix, parts.Attribute), ".")
			s, err := pm.Paved.GetString(pathStr)
			if fieldpath.IsNotFound(err) {
				continue
			}
			if err != nil {
				return errors.Wrapf(err, "cannot get string value from paved: %s", pathStr)
			}
			params[paramName] = s
		}
	}
	return nil
}

// PrepareLocalResolutionContext prepares a resolution context for resolving
// cross-resource references locally between a target resource and its
// dependencies given as examples in the Terraform registry.
func PrepareLocalResolutionContext(exampleMeta registry.ResourceExample) (map[string]*PavedWithManifest, error) {
	resolutionContext := make(map[string]*PavedWithManifest, len(exampleMeta.Dependencies))
	for rn, m := range exampleMeta.Dependencies {
		// <Terraform resource>.<example name>
		r := strings.Split(rn, ".")[0]
		var err error
		resolutionContext[r], err = paveExampleManifest(m)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot pave example manifest for resource: %s", r)
		}
	}
	return resolutionContext, nil
}
