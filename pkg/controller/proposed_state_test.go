// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestProposedNewAttributes_NullPriorNestedAttribute is a regression test for a
// panic that occurred when a SingleNestedAttribute's prior value was a typed
// null (e.g. the TF provider returned null for an unset optional addon).
//
// proposedNewAttributes previously called priorMap[name].Type() where priorMap
// was empty (prior.As silently fails on null), yielding a zero-value
// tftypes.Value whose Type() returns nil, causing tftypes.NewValue to panic.
func TestProposedNewAttributes_NullPriorNestedAttribute(t *testing.T) {
	addonAttrTypes := map[string]tftypes.Type{
		"external_traffic_policy": tftypes.String,
		"replicas":                tftypes.Number,
	}
	addonObjType := tftypes.Object{AttributeTypes: addonAttrTypes}
	topObjType := tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"addon": addonObjType,
		},
	}

	schema := rschema.Schema{
		Attributes: map[string]rschema.Attribute{
			"addon": rschema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]rschema.Attribute{
					"external_traffic_policy": rschema.StringAttribute{
						Optional: true,
						Computed: true,
					},
					"replicas": rschema.NumberAttribute{
						Optional: true,
						Computed: true,
					},
				},
			},
		},
	}

	// prior: resource exists, but addon attribute is a typed null (unset optional
	// nested attribute as returned by a TF provider Read).
	prior := tftypes.NewValue(topObjType, map[string]tftypes.Value{
		"addon": tftypes.NewValue(addonObjType, nil),
	})

	// config: user has now specified the addon.
	config := tftypes.NewValue(topObjType, map[string]tftypes.Value{
		"addon": tftypes.NewValue(addonObjType, map[string]tftypes.Value{
			"external_traffic_policy": tftypes.NewValue(tftypes.String, nil),
			"replicas":                tftypes.NewValue(tftypes.Number, nil),
		}),
	})

	// Must not panic.
	result := proposedState(schema, prior, config)

	if !result.IsKnown() || result.IsNull() {
		t.Errorf("expected a known non-null result, got %s", result)
	}
}
