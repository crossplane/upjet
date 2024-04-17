// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"slices"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
)

type conversionMode int

const (
	toEmbeddedObject conversionMode = iota
	toSingletonList
)

// String returns a string representation of the conversion mode.
func (m conversionMode) String() string {
	switch m {
	case toSingletonList:
		return "toSingletonList"
	case toEmbeddedObject:
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

// convert performs conversion between singleton lists and embedded objects
// while passing the CRD parameters to the Terraform layer and while reading
// state from the Terraform layer at runtime. The paths where the conversion
// will be performed are specified using paths and the conversion mode (whether
// an embedded object will be converted into a singleton list or a singleton
// list will be converted into an embedded object) is determined by the mode
// parameter.
func convert(params map[string]any, paths []string, mode conversionMode) (map[string]any, error) { //nolint:gocyclo // easier to follow as a unit
	switch mode {
	case toSingletonList:
		slices.Sort(paths)
	case toEmbeddedObject:
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
		switch len(exp) {
		case 0:
			continue
		case 1:
			v, err := pv.GetValue(exp[0])
			if err != nil {
				return nil, errors.Wrapf(err, "cannot get the value at the field path %s with the conversion mode set to %q", exp[0], mode)
			}
			switch mode {
			case toSingletonList:
				if err := setValue(pv, []any{v}, exp[0]); err != nil {
					return nil, errors.Wrapf(err, "cannot set the singleton list's value at the field path %s", exp[0])
				}
			case toEmbeddedObject:
				s, ok := v.([]any)
				if !ok || len(s) > 1 {
					// if len(s) is 0, then it's not a slice
					return nil, errors.Errorf("singleton list, at the field path %s, must have a length of 1 but it has a length of %d", exp[0], len(s))
				}
				var newVal any = map[string]any{}
				if len(s) > 0 {
					newVal = s[0]
				}
				if err := setValue(pv, newVal, exp[0]); err != nil {
					return nil, errors.Wrapf(err, "cannot set the embedded object's value at the field path %s", exp[0])
				}
			}
		default:
			return nil, errors.Errorf("unexpected number of expansions (%d) for the wildcard field path expression %s", len(exp), fp)
		}
	}
	return params, nil
}
