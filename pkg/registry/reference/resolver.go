/*
Copyright 2022 Upbound Inc.
*/

package reference

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/registry"
	"github.com/upbound/upjet/pkg/resource/json"
)

const (
	// Wildcard denotes a wildcard resource name
	Wildcard = "*"
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

// MatchRefParts parses a Parts from an HCL reference string
func MatchRefParts(ref string) *Parts {
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

// GetResourceName returns the resource name or the wildcard
// for this Parts.
func (parts Parts) GetResourceName(wildcardName bool) string {
	name := parts.ExampleName
	if wildcardName || len(name) == 0 {
		name = Wildcard
	}
	return fmt.Sprintf("%s.%s", parts.Resource, name)
}

// NewRefParts initializes a new Parts from the specified
// resource and example names.
func NewRefParts(resource, exampleName string) Parts {
	return Parts{
		Resource:    resource,
		ExampleName: exampleName,
	}
}

// NewRefPartsFromResourceName initializes a new Parts
// from the specified <resource name>.<example name>
// string.
func NewRefPartsFromResourceName(rn string) Parts {
	parts := strings.Split(rn, ".")
	return Parts{
		Resource:    parts[0],
		ExampleName: parts[1],
	}
}

// PavedWithManifest represents an example manifest with a fieldpath.Paved
type PavedWithManifest struct {
	Paved        *fieldpath.Paved
	ManifestPath string
	ParamsPrefix []string
	refsResolved bool
	Config       *config.Resource
	Group        string
	Version      string
	ExampleName  string
}

// ResolutionContext represents a reference resolution context where
// wildcard or named references are used.
type ResolutionContext struct {
	WildcardNames bool
	Context       map[string]*PavedWithManifest
}

func paveExampleManifest(m string) (*PavedWithManifest, error) {
	var exampleParams map[string]any
	if err := json.TFParser.Unmarshal([]byte(m), &exampleParams); err != nil {
		return nil, errors.Wrapf(err, "cannot unmarshal example manifest: %s", m)
	}
	return &PavedWithManifest{
		Paved: fieldpath.Pave(exampleParams),
	}, nil
}

// ResolveReferencesOfPaved resolves references of a PavedWithManifest
// in the given resolution context.
func (rr *Injector) ResolveReferencesOfPaved(pm *PavedWithManifest, resolutionContext *ResolutionContext) error {
	if pm.refsResolved {
		return nil
	}
	pm.refsResolved = true
	return errors.Wrap(rr.resolveReferences(pm.Paved.UnstructuredContent(), resolutionContext), "failed to resolve references of paved")
}

func (rr *Injector) resolveReferences(params map[string]any, resolutionContext *ResolutionContext) error { // nolint:gocyclo
	for paramName, paramValue := range params {
		switch t := paramValue.(type) {
		case map[string]any:
			if err := rr.resolveReferences(t, resolutionContext); err != nil {
				return err
			}

		case []any:
			for _, e := range t {
				eM, ok := e.(map[string]any)
				if !ok {
					continue
				}
				if err := rr.resolveReferences(eM, resolutionContext); err != nil {
					return err
				}
			}

		case string:
			parts := MatchRefParts(t)
			if parts == nil {
				continue
			}
			pm := resolutionContext.Context[parts.GetResourceName(resolutionContext.WildcardNames)]
			if pm == nil || pm.Paved == nil {
				continue
			}
			if err := rr.ResolveReferencesOfPaved(pm, resolutionContext); err != nil {
				return errors.Wrapf(err, "cannot recursively resolve references for %q", parts.Resource)
			}
			pathStr := strings.Join(append(pm.ParamsPrefix, parts.Attribute), ".")
			s, err := pm.Paved.GetString(convertTFPathToFieldPath(pathStr))
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

func convertTFPathToFieldPath(path string) string {
	segments := strings.Split(path, ".")
	result := make([]string, 0, len(segments))
	for i, p := range segments {
		d, err := strconv.Atoi(p)
		switch {
		case err != nil:
			result = append(result, p)

		case i > 0:
			result[i-1] = fmt.Sprintf("%s[%d]", result[i-1], d)
		}
	}
	return strings.Join(result, ".")
}

// PrepareLocalResolutionContext returns a ResolutionContext that can be used
// for resolving references between a target resource and its dependencies
// that are exemplified together with the resource in Terraform registry.
func PrepareLocalResolutionContext(exampleMeta registry.ResourceExample, rootName string) (*ResolutionContext, error) {
	context := make(map[string]*PavedWithManifest, len(exampleMeta.Dependencies)+1)
	var err error
	for rn, m := range exampleMeta.Dependencies {
		// <Terraform resource>.<example name>
		context[rn], err = paveExampleManifest(m)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot pave example manifest for resource: %s", rn)
		}
	}
	context[rootName], err = paveExampleManifest(exampleMeta.Manifest)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot pave example manifest for resource: %s", rootName)
	}
	return &ResolutionContext{
		WildcardNames: false,
		Context:       context,
	}, nil
}
