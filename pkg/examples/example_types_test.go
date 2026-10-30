// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/crossplane/upjet/v2/pkg/config"
)

func TestTransformFieldsConvertsToSchemaTypes(t *testing.T) {
	r := &config.Resource{
		TerraformResource: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"size":                   {Type: schema.TypeInt, Optional: true},
				"pending_window_in_days": {Type: schema.TypeInt, Optional: true},
				"ratio":                  {Type: schema.TypeFloat, Optional: true},
				"enabled":                {Type: schema.TypeBool, Optional: true},
				"description":            {Type: schema.TypeString, Optional: true},
				"ports": {
					Type:     schema.TypeList,
					Optional: true,
					Elem:     &schema.Schema{Type: schema.TypeInt},
				},
				"zone_ids": {
					Type:     schema.TypeSet,
					Optional: true,
					Elem:     &schema.Schema{Type: schema.TypeString},
				},
				"weights": {
					Type:     schema.TypeMap,
					Optional: true,
					Elem:     &schema.Schema{Type: schema.TypeFloat},
				},
				"data_disk": {
					Type:     schema.TypeList,
					Optional: true,
					Elem: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"size":      {Type: schema.TypeInt, Optional: true},
							"encrypted": {Type: schema.TypeBool, Optional: true},
						},
					},
				},
			},
		},
	}

	cases := map[string]struct {
		reason string
		params map[string]any
		want   map[string]any
	}{
		"QuotedPrimitives": {
			reason: "Quoted ints, floats and bools should be converted to the type the schema declares.",
			params: map[string]any{
				"size":                   "30",
				"pending_window_in_days": "7",
				"ratio":                  "0.5",
				"enabled":                "true",
			},
			want: map[string]any{
				"size":                int64(30),
				"pendingWindowInDays": int64(7),
				"ratio":               0.5,
				"enabled":             true,
			},
		},
		"StringsStayStrings": {
			reason: "A numeric-looking value in a string field should stay a string.",
			params: map[string]any{
				"description": "30",
				"zone_ids":    []any{"1", "2"},
			},
			want: map[string]any{
				"description": "30",
				"zoneIds":     []any{"1", "2"},
			},
		},
		"UnparseableValuesUnchanged": {
			reason: "Values that are not a valid literal of the schema type, such as interpolations, should be left alone.",
			params: map[string]any{
				"size":    "${var.size}",
				"ratio":   "half",
				"enabled": "1",
			},
			want: map[string]any{
				"size":    "${var.size}",
				"ratio":   "half",
				"enabled": "1",
			},
		},
		"AlreadyTyped": {
			reason: "Values that already have the right type should be unchanged.",
			params: map[string]any{
				"size":    float64(30),
				"enabled": false,
			},
			want: map[string]any{
				"size":    float64(30),
				"enabled": false,
			},
		},
		"CollectionsOfPrimitives": {
			reason: "Elements of lists, sets and maps of primitives should be converted one by one.",
			params: map[string]any{
				"ports":   []any{"80", "443", "${var.port}"},
				"weights": map[string]any{"a": "0.25", "b": "1"},
			},
			want: map[string]any{
				"ports":   []any{int64(80), int64(443), "${var.port}"},
				"weights": map[string]any{"a": 0.25, "b": float64(1)},
			},
		},
		"NestedBlocks": {
			reason: "Fields inside nested blocks should be converted too.",
			params: map[string]any{
				"data_disk": []any{
					map[string]any{"size": "40", "encrypted": "false"},
				},
			},
			want: map[string]any{
				"dataDisk": []any{
					map[string]any{"size": int64(40), "encrypted": false},
				},
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			transformFields(r, tc.params, nil, "", false, defaultNamespace)
			if diff := cmp.Diff(tc.want, tc.params); diff != "" {
				t.Errorf("\n%s\ntransformFields(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
