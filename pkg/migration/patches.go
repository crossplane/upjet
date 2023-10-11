// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	xpv1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
)

var (
	regexIndex   = regexp.MustCompile(`(.+)\[(.+)]`)
	regexJSONTag = regexp.MustCompile(`([^,]+)?(,.+)?`)
)

const (
	jsonTagInlined = ",inline"
)

// isConverted looks up the specified name in the list of already converted
// patch sets.
func isConverted(convertedPS []string, psName string) bool {
	for _, n := range convertedPS {
		if psName == n {
			return true
		}
	}
	return false
}

// removeInvalidPatches removes the (inherited) patches from
// a (split) migration target composed template. The migration target composed
// templates inherit patches from migration source templates by default, and
// this function is responsible for removing patches (including references to
// patch sets) that do not conform to the target composed template's schema.
func (pg *PlanGenerator) removeInvalidPatches(gvkSource, gvkTarget schema.GroupVersionKind, patchSets []xpv1.PatchSet, targetTemplate *xpv1.ComposedTemplate, convertedPS []string) error { //nolint:gocyclo // complexity (11) just above the threshold (10)
	c := pg.registry.scheme
	source, err := c.New(gvkSource)
	if err != nil {
		return errors.Wrapf(err, "failed to instantiate a new source object with GVK: %s", gvkSource.String())
	}
	target, err := c.New(gvkTarget)
	if err != nil {
		return errors.Wrapf(err, "failed to instantiate a new target object with GVK: %s", gvkTarget.String())
	}

	newPatches := make([]xpv1.Patch, 0, len(targetTemplate.Patches))
	var patches []xpv1.Patch
	for _, p := range targetTemplate.Patches {
		s := source
		switch p.Type { //nolint:exhaustive
		case xpv1.PatchTypePatchSet:
			ps := getNamedPatchSet(p.PatchSetName, patchSets)
			if ps == nil {
				// something is wrong with the patchset ref,
				// we will just remove the ref
				continue
			}
			if isConverted(convertedPS, ps.Name) {
				// then do not use the source schema as the patch set
				// is already converted
				s = target
			}
			// assert each of the patches in the set
			// conform the target schema
			patches = ps.Patches
		default:
			patches = []xpv1.Patch{p}
		}
		keep := true
		for _, p := range patches {
			ok, err := assertPatchSchemaConformance(p, s, target)
			if err != nil {
				err := errors.Wrap(err, "failed to check whether the patch conforms to the target schema")
				if pg.ErrorOnInvalidPatchSchema {
					return err
				}
				log.Printf("Excluding the patch from the migration target because conformance checking has failed with: %v\n", err)
				// if we could not check the patch's schema conformance
				// and the plan generator is configured not to error,
				// assume the patch does not conform to the schema
				ok = false
			}
			if !ok {
				keep = false
				break
			}
		}
		if keep {
			newPatches = append(newPatches, p)
		}
	}
	targetTemplate.Patches = newPatches
	return nil
}

// assertPatchSchemaConformance asserts that the specified patch actually
// conforms the specified target schema. We also assert the patch conforms
// to the migration source schema, which prevents an invalid patch from being
// preserved after the conversion.
func assertPatchSchemaConformance(p xpv1.Patch, source, target any) (bool, error) {
	var targetPath *string
	// because this is defaulting logic and what we default can be overridden
	// later in the convert, the type switch is not exhaustive
	// TODO: consider processing other patch types
	switch p.Type { //nolint:exhaustive
	case xpv1.PatchTypeFromCompositeFieldPath, "": // the default type
		targetPath = p.ToFieldPath
	case xpv1.PatchTypeToCompositeFieldPath:
		targetPath = p.FromFieldPath
	}
	if targetPath == nil {
		return false, nil
	}
	ok, err := assertNameAndTypeAtPath(reflect.TypeOf(source), reflect.TypeOf(target), splitPathComponents(*targetPath))
	return ok, errors.Wrapf(err, "failed to assert patch schema for path: %s", *targetPath)
}

// splitPathComponents splits a fieldpath expression into its path components,
// e.g., `m[a.b.c].a.b.c` is split into `m[a.b.c]`, `a`, `b`, `c`.
func splitPathComponents(path string) []string {
	components := strings.Split(path, ".")
	result := make([]string, 0, len(components))
	indexedExpression := false
	for _, c := range components {
		switch {
		case indexedExpression:
			result[len(result)-1] = fmt.Sprintf("%s.%s", result[len(result)-1], c)
			if strings.Contains(c, "]") {
				indexedExpression = false
			}
		default:
			result = append(result, c)
			if strings.Contains(c, "[") && !strings.Contains(c, "]") {
				indexedExpression = true
			}
		}
	}
	return result
}

func isRawExtension(source, target reflect.Type) bool {
	reType := reflect.TypeOf(runtime.RawExtension{})
	rePtrType := reflect.TypeOf(&runtime.RawExtension{})
	return (source == reType && target == reType) || (source == rePtrType && target == rePtrType)
}

// assertNameAndTypeAtPath asserts that the migration source and target
// templates both have the same kind for the type at the specified path.
// Also validates the specific path is valid for the source.
func assertNameAndTypeAtPath(source, target reflect.Type, pathComponents []string) (bool, error) { //nolint:gocyclo
	if len(pathComponents) < 1 {
		return compareKinds(source, target), nil
	}
	// if both source and target are runtime.RawExtensions,
	// then stop traversing the type hierarchy.
	if isRawExtension(source, target) {
		return true, nil
	}

	pathComponent := pathComponents[0]
	if len(pathComponent) == 0 {
		return false, errors.Errorf("failed to compare source and target structs. Invalid path: %s", strings.Join(pathComponents, "."))
	}
	m := regexIndex.FindStringSubmatch(pathComponent)
	if m != nil {
		// then a map component or a slicing component
		pathComponent = m[1]
	}

	// assert the source and the target types
	fSource, err := getFieldWithSerializedName(source, pathComponent)
	if err != nil {
		return false, errors.Wrapf(err, "failed to assert source struct field kind at path: %s", strings.Join(pathComponents, "."))
	}
	if fSource == nil {
		// then source field could not be found
		return false, errors.Errorf("struct field %q does not exist for the source type %q at path: %s", pathComponent, source.String(), strings.Join(pathComponents, "."))
	}
	// now assert that this field actually exists for the target type
	// with the same type
	fTarget, err := getFieldWithSerializedName(target, pathComponent)
	if err != nil {
		return false, errors.Wrapf(err, "failed to assert target struct field kind at path: %s", strings.Join(pathComponents, "."))
	}
	if fTarget == nil || !fTarget.IsExported() || !compareKinds(fSource.Type, fTarget.Type) {
		return false, nil
	}

	nextSource, nextTarget := fSource.Type, fTarget.Type
	if m != nil {
		// parents are of map or slice type
		nextSource = nextSource.Elem()
		nextTarget = nextTarget.Elem()
	}
	return assertNameAndTypeAtPath(nextSource, nextTarget, pathComponents[1:])
}

// compareKinds compares the kinds of the specified types
// dereferencing (following) pointer types.
func compareKinds(s, t reflect.Type) bool {
	if s.Kind() == reflect.Pointer {
		s = s.Elem()
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return s.Kind() == t.Kind()
}

// getFieldWithSerializedName returns the field of a struct (if it exists)
// with the specified serialized (JSON) name. Returns a nil (and a nil error)
// if a field with the specified serialized name is not found
// in the specified type.
func getFieldWithSerializedName(t reflect.Type, name string) (*reflect.StructField, error) { //nolint:gocyclo
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, errors.Errorf("type is not a struct: %s", t.Name())
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		serializedName := f.Name
		inlined := false
		if fTag, ok := f.Tag.Lookup("json"); ok {
			if m := regexJSONTag.FindStringSubmatch(fTag); m != nil && len(m[1]) > 0 {
				serializedName = m[1]
			}
			if strings.HasSuffix(fTag, jsonTagInlined) {
				inlined = true
			}
		}
		if name == serializedName {
			return &f, nil
		}
		if inlined {
			inlinedType := f.Type
			if inlinedType.Kind() == reflect.Pointer {
				inlinedType = inlinedType.Elem()
			}
			if inlinedType.Kind() == reflect.Struct {
				sf, err := getFieldWithSerializedName(inlinedType, name)
				if err != nil {
					return nil, errors.Wrapf(err, "failed to search for field %q in inlined type: %s", name, inlinedType.String())
				}
				if sf != nil {
					return sf, nil
				}
			}
		}
	}
	return nil, nil // not found
}

// getNamedPatchSet returns the patch set with the specified name
// from the specified patch set slice. Returns nil if a patch set
// with the given name is not found.
func getNamedPatchSet(name *string, patchSets []xpv1.PatchSet) *xpv1.PatchSet {
	if name == nil {
		// if name is not specified, do not attempt to find a named patchset
		return nil
	}
	for _, ps := range patchSets {
		if *name == ps.Name {
			return &ps
		}
	}
	return nil
}

// getConvertedPatchSetNames returns the names of patch sets that have been
// converted by a PatchSetConverter.
func getConvertedPatchSetNames(newPatchSets, oldPatchSets []xpv1.PatchSet) []string {
	converted := make([]string, 0, len(newPatchSets))
	for _, n := range newPatchSets {
		found := false
		for _, o := range oldPatchSets {
			if o.Name != n.Name {
				continue
			}
			found = true
			if !reflect.DeepEqual(o, n) {
				converted = append(converted, n.Name)
			}
			break
		}
		if !found {
			converted = append(converted, n.Name)
		}
	}
	return converted
}

// convertToMap converts the given slice of patch sets to a map of
// patch sets keyed by their names.
func convertToMap(ps []xpv1.PatchSet) map[string]*xpv1.PatchSet {
	m := make(map[string]*xpv1.PatchSet, len(ps))
	for _, p := range ps {
		// Crossplane dereferences the last patch set with the same name,
		// so override with the last patch set with the same name.
		m[p.Name] = p.DeepCopy()
	}
	return m
}

// convertFromMap converts the specified map of patch sets back to a slice.
// If filterDeleted is set, previously existing patch sets in the Composition
// which have been removed from the map are also removed from the resulting
// slice, and eventually from the Composition. PatchSetConverters are
// allowed to remove patch sets, whereas Composition converters are
// not, as Composition converters have a local view of the patch sets and
// don't know about the other composed templates that may be sharing
// patch sets with them.
func convertFromMap(psMap map[string]*xpv1.PatchSet, oldPS []xpv1.PatchSet, filterDeleted bool) []xpv1.PatchSet {
	result := make([]xpv1.PatchSet, 0, len(psMap))
	for _, ps := range oldPS {
		if filterDeleted && psMap[ps.Name] == nil {
			// then patch set has been deleted
			continue
		}
		if psMap[ps.Name] == nil {
			result = append(result, ps)
			continue
		}
		result = append(result, *psMap[ps.Name])
		delete(psMap, ps.Name)
	}
	// add the new patch sets
	for _, ps := range psMap {
		if ps == nil {
			continue
		}
		result = append(result, *ps)
	}
	return result
}
