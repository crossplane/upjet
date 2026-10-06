// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"slices"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
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
func preserveInitProviderExclusiveParams(tr resource.Terraformed, params, state map[string]any, cfg *config.Resource) error {
	forProviderParams, err := tr.GetParameters()
	if err != nil {
		return errors.Wrap(err, "cannot get spec.forProvider parameters")
	}
	forProviderParams, err = cfg.ApplyTFConversions(forProviderParams, config.ToTerraform)
	if err != nil {
		return errors.Wrap(err, "cannot apply tf conversions to spec.forProvider parameters")
	}
	initParams, err := tr.GetInitParameters()
	if err != nil {
		return errors.Wrap(err, "cannot get spec.initProvider parameters")
	}
	initParams, err = cfg.ApplyTFConversions(initParams, config.ToTerraform)
	if err != nil {
		return errors.Wrap(err, "cannot apply tf conversions to spec.initProvider parameters")
	}

	pavedParams := fieldpath.Pave(params)
	pavedState := fieldpath.Pave(state)
	paths := resource.GetTerraformIgnoreChanges(forProviderParams, initParams)
	// Remove higher list indices first, so that a removal doesn't shift later paths.
	slices.Reverse(paths)
	for _, p := range paths {
		v, err := pavedState.GetValue(p)
		if err != nil {
			if err := pavedParams.DeleteField(p); err != nil && !fieldpath.IsNotFound(err) {
				return errors.Wrapf(err, "cannot remove initProvider-exclusive parameter %q", p)
			}
			continue
		}
		err = pavedParams.SetValue(p, v)
		if err != nil {
			return errors.Wrapf(err, "cannot set initProvider-exclusive parameter %q from state", p)
		}
	}
	return nil
}
