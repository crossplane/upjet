// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource"
)

// removeInitProviderExclusiveParams removes initProvider-exclusive fields
// from the merged params map so they are not sent in the TF config on
// updates.
// This is the Plugin Framework equivalent of filterInitExclusiveDiffs from the SDKv2 client.
func removeInitProviderExclusiveParams(tr resource.Terraformed, params map[string]any, cfg *config.Resource) error {
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

	for _, key := range getTerraformIgnoreChanges(forProviderParams, initParams) {
		deleteNestedParam(params, key)
	}
	return nil
}

// deleteNestedParam removes a dot-separated key path from a nested map[string]any.
// If any intermediate segment is missing or has an unexpected type, the function is a no-op.
func deleteNestedParam(current any, path string) {
	segments := strings.Split(path, ".")
	deleteSegments(current, segments)
}

func deleteSegments(current any, segments []string) {
	if len(segments) == 0 {
		return
	}

	switch v := current.(type) {
	case map[string]any:
		if len(segments) == 1 {
			delete(v, segments[0])
			return
		}
		next, ok := v[segments[0]]
		if !ok {
			return
		}
		deleteSegments(next, segments[1:])
	case []any: // numeric segments are array indices, e.g. "items.0.value"
		idx, err := strconv.Atoi(segments[0])
		if err != nil || idx < 0 || idx >= len(v) {
			return
		}
		deleteSegments(v[idx], segments[1:])
	}
}
