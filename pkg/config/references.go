/*
Copyright 2022 Upbound Inc.
*/

package config

import (
	"fmt"

	"github.com/pkg/errors"
)

const (
	extractorPackagePath      = "github.com/upbound/upjet/pkg/resource"
	extractResourceIDFuncPath = extractorPackagePath + ".ExtractResourceID()"
	fmtExtractParamFuncPath   = extractorPackagePath + `.ExtractParamPath("%s")`
)

// ReferenceResolver resolves references using provider metadata
type ReferenceResolver struct {
	ConfigResources   map[string]*Resource
	ModulePath        string
	ProviderShortName string
}

// NewReferenceResolver initializes a new ReferenceResolver
func NewReferenceResolver(modulePath string, configResources map[string]*Resource) *ReferenceResolver {
	return &ReferenceResolver{
		ConfigResources: configResources,
		ModulePath:      modulePath,
	}
}

func getExtractorFuncPath(sourceAttr string) string {
	switch sourceAttr {
	// value extractor from status.atProvider.id
	case "id":
		return extractResourceIDFuncPath
	// value extractor from spec.forProvider.<attr>
	default:
		return fmt.Sprintf(fmtExtractParamFuncPath, sourceAttr)
	}
}

func (rr *ReferenceResolver) setReferencesFromMetadata() error { // nolint:gocyclo
	for n, r := range rr.ConfigResources {
		m := rr.ConfigResources[n].MetaResource
		if m == nil {
			continue
		}

		for _, re := range m.Examples {
			pm, err := paveExampleManifest(re.Manifest)
			if err != nil {
				return errors.Wrapf(err, "cannot pave example manifest for resource: %s", n)
			}
			resolutionContext, err := prepareLocalResolutionContext(re)
			if err != nil {
				return errors.Wrapf(err, "cannot prepare local resolution context for resource: %s", n)
			}
			if err := rr.ResolveReferencesOfPaved(pm, resolutionContext); err != nil {
				return errors.Wrapf(err, "cannot resolve references of resource with local examples context: %s", n)
			}
			for targetAttr, ref := range re.References {
				// if a reference is already configured for the target attribute
				if _, ok := r.References[targetAttr]; ok {
					continue
				}
				parts := getRefParts(ref)
				if parts == nil {
					continue
				}
				resolved := false
				for _, p := range pm.paramsResolved {
					if p == targetAttr {
						resolved = true
						break
					}
				}
				if resolved && skipReference(rr.ConfigResources[n].SkipReferencesTo, parts) {
					continue
				}
				if _, ok := rr.ConfigResources[parts.resource]; !ok {
					continue
				}
				r.References[targetAttr] = Reference{
					TerraformName: parts.resource,
					Extractor:     getExtractorFuncPath(parts.attribute),
				}
			}
		}
	}
	return nil
}

func skipReference(skippedRefs []string, parts *refParts) bool {
	for _, p := range skippedRefs {
		if p == parts.getResourceAttr() {
			return true
		}
	}
	return false
}

func (rr *ReferenceResolver) getTypePath(tfName string) (string, error) {
	r := rr.ConfigResources[tfName]
	if r == nil {
		return "", errors.Errorf("cannot find configuration for Terraform resource: %s", tfName)
	}
	shortGroup := r.ShortGroup
	if len(shortGroup) == 0 {
		shortGroup = rr.ProviderShortName
	}
	return fmt.Sprintf("%s/%s/%s/%s.%s", rr.ModulePath, "apis", shortGroup, r.Version, r.Kind), nil
}

// SetReferenceTypes resolves reference types of configured references
// using their TerraformNames.
func (rr *ReferenceResolver) SetReferenceTypes() error {
	for _, r := range rr.ConfigResources {
		for attr, ref := range r.References {
			if ref.Type == "" && ref.TerraformName != "" {
				crdTypePath, err := rr.getTypePath(ref.TerraformName)
				if err != nil {
					return errors.Wrap(err, "cannot set reference types")
				}
				// TODO(aru): if type mapper cannot provide a mapping,
				// currently we remove the reference. Once,
				// we have type mapper implementations available
				// for all providers, then we can keep the refs
				// instead of removing them, and expect resulting
				// compile errors to be fixed by making the types
				// available to the type mapper.
				if crdTypePath == "" {
					delete(r.References, attr)
					continue
				}
				ref.Type = crdTypePath
				r.References[attr] = ref
			}
		}
	}
	return nil
}
