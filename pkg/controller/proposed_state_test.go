// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestProposedNewAttributes(t *testing.T) {
	addonAttrTypes := map[string]tftypes.Type{
		"external_traffic_policy": tftypes.String,
		"replicas":                tftypes.Number,
	}
	addonObjType := tftypes.Object{AttributeTypes: addonAttrTypes}
	topObjType := tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{"addon": addonObjType},
	}
	schema := rschema.Schema{
		Attributes: map[string]rschema.Attribute{
			"addon": rschema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]rschema.Attribute{
					"external_traffic_policy": rschema.StringAttribute{Optional: true, Computed: true},
					"replicas":                rschema.NumberAttribute{Optional: true, Computed: true},
				},
			},
		},
	}

	type args struct {
		prior  tftypes.Value
		config tftypes.Value
	}
	type want struct {
		result tftypes.Value
	}

	// cmp.Comparer via tftypes.Value.Equal handles unexported fields.
	tfValueCmp := cmp.Comparer(func(a, b tftypes.Value) bool { return a.Equal(b) })

	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NullPriorNestedAttributeWithNonNullConfig": {
			reason: "Must not panic and must return a known non-null result when prior nested attribute is a typed null but config sets it.",
			args: args{
				prior: tftypes.NewValue(topObjType, map[string]tftypes.Value{
					// Typed null: TF provider returned null for an unset optional addon.
					"addon": tftypes.NewValue(addonObjType, nil),
				}),
				config: tftypes.NewValue(topObjType, map[string]tftypes.Value{
					"addon": tftypes.NewValue(addonObjType, map[string]tftypes.Value{
						"external_traffic_policy": tftypes.NewValue(tftypes.String, nil),
						"replicas":                tftypes.NewValue(tftypes.Number, nil),
					}),
				}),
			},
			want: want{
				// Optional+Computed children with null config keep prior value.
				// Prior children are typed nulls derived from the schema (not priorMap).
				result: tftypes.NewValue(topObjType, map[string]tftypes.Value{
					"addon": tftypes.NewValue(addonObjType, map[string]tftypes.Value{
						"external_traffic_policy": tftypes.NewValue(tftypes.String, nil),
						"replicas":                tftypes.NewValue(tftypes.Number, nil),
					}),
				}),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var result tftypes.Value
			panicked := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicked = true
					}
				}()
				result = proposedNew(schema, tc.args.prior.Copy(), tc.args.config.Copy())
			}()

			if panicked {
				t.Errorf("\n%s\nproposedState(...): unexpected panic", tc.reason)
				return
			}
			if diff := cmp.Diff(tc.want.result, result, tfValueCmp); diff != "" {
				t.Errorf("\n%s\nproposedState(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
