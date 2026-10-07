// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"cmp"
	"slices"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource"
)

// preserveInitProviderExclusiveParams sets every initProvider-exclusive path
// in params to its value in the given Terraform state, or removes the path
// from params when the state has no value there.
// This is what Terraform does for `ignore_changes`: the attribute keeps its
// current value instead of being re-applied from initProvider or planned as removed.
// This is the TPF equivalent of filterInitExclusiveDiffs from SDK client.
// It returns whether params changed.
func preserveInitProviderExclusiveParams(tr resource.Terraformed, params, state map[string]any, cfg *config.Resource, tfType tftypes.Type) (bool, error) {
	paths, err := initProviderOnlyPaths(tr, cfg)
	if err != nil {
		return false, err
	}
	pavedParams := fieldpath.Pave(params)
	pavedState := fieldpath.Pave(state)
	changed := false
	for _, p := range paths {
		segments, err := fieldpath.Parse(p)
		// Leave the merged value of a path that cannot be resolved, for
		// example a map key with brackets, as it is.
		if err != nil || indexesIntoSet(tfType, segments) {
			continue
		}
		changed = true
		v, err := pavedState.GetValue(p)
		if err != nil {
			err = pavedParams.DeleteField(p)
			if err != nil && !fieldpath.IsNotFound(err) {
				return false, errors.Wrapf(err, "cannot remove initProvider-exclusive parameter %q", p)
			}
			continue
		}
		err = pavedParams.SetValue(p, v)
		if err != nil {
			return false, errors.Wrapf(err, "cannot set initProvider-exclusive parameter %q from state", p)
		}
	}
	return changed, nil
}

// initProviderOnlyPaths returns the paths of the fields that are in initProvider
// but not in forProvider, with Terraform conversions applied.
// The paths of one list are in descending index order, so that removing an
// element does not shift the elements of the paths that follow.
func initProviderOnlyPaths(tr resource.Terraformed, cfg *config.Resource) ([]string, error) {
	initParams, err := tr.GetInitParameters()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get spec.initProvider parameters")
	}
	if len(initParams) == 0 {
		return nil, nil
	}
	initParams, err = cfg.ApplyTFConversions(initParams, config.ToTerraform)
	if err != nil {
		return nil, errors.Wrap(err, "cannot apply tf conversions to spec.initProvider parameters")
	}
	forProviderParams, err := tr.GetParameters()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get spec.forProvider parameters")
	}
	forProviderParams, err = cfg.ApplyTFConversions(forProviderParams, config.ToTerraform)
	if err != nil {
		return nil, errors.Wrap(err, "cannot apply tf conversions to spec.forProvider parameters")
	}
	paths := resource.GetTerraformIgnoreChanges(forProviderParams, initParams)
	// For the elements of one list, longer paths first and then reverse
	// lexical order is descending index order.
	slices.SortFunc(paths, func(a, b string) int {
		return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(b, a))
	})
	return paths, nil
}

// indexesIntoSet reports whether the path steps into an element of a set.
// Set elements have no stable position, so an index taken from the spec
// does not identify the same element in the Terraform state.
func indexesIntoSet(t tftypes.Type, segments fieldpath.Segments) bool {
	for _, s := range segments {
		switch typ := t.(type) {
		case tftypes.Object:
			t = typ.AttributeTypes[s.Field]
		case tftypes.Map:
			t = typ.ElementType
		case tftypes.List:
			t = typ.ElementType
		case tftypes.Set:
			return true
		default:
			return false
		}
	}
	return false
}
