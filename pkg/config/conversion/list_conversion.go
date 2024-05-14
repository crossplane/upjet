// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
)

// ListConversionMode denotes the mode of the list-object API conversion, e.g.,
// conversion of embedded objects into singleton lists.
type ListConversionMode int

const (
	// ToEmbeddedObject represents a runtime conversion from a singleton list
	// to an embedded object, i.e., the runtime conversions needed while
	// reading from the Terraform state and updating the CRD
	// (for status, late-initialization, etc.)
	ToEmbeddedObject ListConversionMode = iota
	// ToSingletonList represents a runtime conversion from an embedded object
	// to a singleton list, i.e., the runtime conversions needed while passing
	// the configuration data to the underlying Terraform layer.
	ToSingletonList
)

const (
	errFmtMultiItemList = "singleton list, at the field path %s, must have a length of at most 1 but it has a length of %d"
	errFmtNonSlice      = "value at the field path %s must be []any, not %q"
)

// String returns a string representation of the conversion mode.
func (m ListConversionMode) String() string {
	switch m {
	case ToSingletonList:
		return "toSingletonList"
	case ToEmbeddedObject:
		return "toEmbeddedObject"
	default:
		return "unknown"
	}
}

// setValue sets the value, in pv, to v at the specified path fp.
// It's implemented on top of the fieldpath library by accessing
// the parent map in fp and directly setting v as a value in the
// parent map. We don't use fieldpath.Paved.SetValue because the
// JSON value validation performed by it potentially changes types.
func setValue(pv *fieldpath.Paved, v any, fp string) error {
	segments := strings.Split(fp, ".")
	p := fp
	var pm any = pv.UnstructuredContent()
	var err error
	if len(segments) > 1 {
		p = strings.Join(segments[:len(segments)-1], ".")
		pm, err = pv.GetValue(p)
		if err != nil {
			return errors.Wrapf(err, "cannot get the parent value at field path %s", p)
		}
	}
	parent, ok := pm.(map[string]any)
	if !ok {
		return errors.Errorf("parent at field path %s must be a map[string]any", p)
	}
	parent[segments[len(segments)-1]] = v
	return nil
}

// Convert performs conversion between singleton lists and embedded objects
// while passing the CRD parameters to the Terraform layer and while reading
// state from the Terraform layer at runtime. The paths where the conversion
// will be performed are specified using paths and the conversion mode (whether
// an embedded object will be converted into a singleton list or a singleton
// list will be converted into an embedded object) is determined by the mode
// parameter.
func Convert(params map[string]any, paths []string, mode ListConversionMode) (map[string]any, error) { //nolint:gocyclo // easier to follow as a unit
	switch mode {
	case ToSingletonList:
		slices.Sort(paths)
	case ToEmbeddedObject:
		sort.Slice(paths, func(i, j int) bool {
			return paths[i] > paths[j]
		})
	}

	pv := fieldpath.Pave(params)
	for _, fp := range paths {
		exp, err := pv.ExpandWildcards(fp)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot expand wildcards for the field path expression %s", fp)
		}
		for _, e := range exp {
			v, err := pv.GetValue(e)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot get the value at the field path %s with the conversion mode set to %q", e, mode)
			}
			switch mode {
			case ToSingletonList:
				if err := setValue(pv, []any{v}, e); err != nil {
					return nil, errors.Wrapf(err, "cannot set the singleton list's value at the field path %s", e)
				}
			case ToEmbeddedObject:
				var newVal any = nil
				if v != nil {
					newVal = map[string]any{}
					s, ok := v.([]any)
					if !ok {
						// then it's not a slice
						return nil, errors.Errorf(errFmtNonSlice, e, reflect.TypeOf(v))
					}
					if len(s) > 1 {
						return nil, errors.Errorf(errFmtMultiItemList, e, len(s))
					}
					if len(s) > 0 {
						newVal = s[0]
					}
				}
				if err := setValue(pv, newVal, e); err != nil {
					return nil, errors.Wrapf(err, "cannot set the embedded object's value at the field path %s", e)
				}
			}
		}
	}
	return params, nil
}
