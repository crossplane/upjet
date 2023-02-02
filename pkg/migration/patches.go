// Copyright 2023 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package migration

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	xpv1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	regexSlice = regexp.MustCompile(`(.+)\[\d+]`)
	// this regex captures some malformed expressions
	// (like mismatches in quotes, etc.)
	regexMap     = regexp.MustCompile(`(.+)\[["|']?\D+["|']?]`)
	regexJSONTag = regexp.MustCompile(`([^,]+)?(,.+)?`)
)

const (
	jsonTagInlined = ",inline"
)

// removeInvalidPatches removes the (inherited) patches from
// a (split) migration target composed template. The migration target composed
// templates inherit patches from migration source templates by default, and
// this function is responsible for removing patches (including references to
// patch sets) that do not conform to the target composed template's schema.
func removeInvalidPatches(c runtime.ObjectCreater, gvkSource, gvkTarget schema.GroupVersionKind, patchSets []xpv1.PatchSet, targetTemplate *xpv1.ComposedTemplate) error {
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
		switch p.Type { // nolint:exhaustive
		case xpv1.PatchTypePatchSet:
			ps := getNamedPatchSet(p.PatchSetName, patchSets)
			if ps == nil {
				// something is wrong with the patchset ref,
				// we will just remove the ref
				continue
			}
			// assert each of the patches in the set
			// conform the target schema
			patches = ps.Patches
		default:
			patches = []xpv1.Patch{p}
		}
		keep := true
		for _, p := range patches {
			ok, err := assertPatchSchemaConformance(p, source, target)
			if err != nil {
				return errors.Wrap(err, "failed to check whether the patch conforms to the target schema")
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
	switch p.Type { // nolint:exhaustive
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

// assertNameAndTypeAtPath asserts that the migration source and target
// templates both have the same kind for the type at the specified path.
// Also validates the specific path is valid for the source.
func assertNameAndTypeAtPath(source, target reflect.Type, pathComponents []string) (bool, error) { // nolint:gocyclo
	if len(pathComponents) < 1 {
		return compareKinds(source, target), nil
	}

	pathComponent := pathComponents[0]
	if len(pathComponent) == 0 {
		return false, errors.Errorf("failed to compare source and target structs. Invalid path: %s", strings.Join(pathComponents, "."))
	}
	m := regexMap.FindStringSubmatch(pathComponents[0])
	if m == nil {
		// if not a map index expression, check for slice indexing expression
		m = regexSlice.FindStringSubmatch(pathComponents[0])
	}
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
func getFieldWithSerializedName(t reflect.Type, name string) (*reflect.StructField, error) { // nolint:gocyclo
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
