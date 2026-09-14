// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/types/conversion/tfjson"
)

func TestTransformFieldsFlattensSchemaTypeObjects(t *testing.T) {
	objectSchema := func(fields map[string]*schema.Schema) *schema.Schema {
		return &schema.Schema{
			Type:     tfjson.SchemaTypeObject,
			MaxItems: 1,
			Elem:     &schema.Resource{Schema: fields},
		}
	}
	r := config.DefaultResource("test_resource", &schema.Resource{Schema: map[string]*schema.Schema{
		"object_block": objectSchema(map[string]*schema.Schema{
			"inner_object": objectSchema(map[string]*schema.Schema{
				"value_field": {Type: schema.TypeString},
			}),
		}),
		"parent_list": {
			Type: schema.TypeList,
			Elem: &schema.Resource{Schema: map[string]*schema.Schema{
				"object_block": objectSchema(map[string]*schema.Schema{
					"value_field": {Type: schema.TypeString},
				}),
			}},
		},
	}}, nil, nil)

	tests := map[string]struct {
		params map[string]any
		want   map[string]any
	}{
		"NestedObjects": {
			params: map[string]any{
				"object_block": []any{map[string]any{
					"inner_object": []any{map[string]any{"value_field": "value"}},
				}},
			},
			want: map[string]any{
				"objectBlock": map[string]any{
					"innerObject": map[string]any{"valueField": "value"},
				},
			},
		},
		"ObjectInsideList": {
			params: map[string]any{
				"parent_list": []any{
					map[string]any{"object_block": []any{map[string]any{"value_field": "one"}}},
					map[string]any{"object_block": []any{map[string]any{"value_field": "two"}}},
				},
			},
			want: map[string]any{
				"parentList": []any{
					map[string]any{"objectBlock": map[string]any{"valueField": "one"}},
					map[string]any{"objectBlock": map[string]any{"valueField": "two"}},
				},
			},
		},
		"AlreadyObject": {
			params: map[string]any{
				"object_block": map[string]any{
					"inner_object": map[string]any{"value_field": "value"},
				},
			},
			want: map[string]any{
				"objectBlock": map[string]any{
					"innerObject": map[string]any{"valueField": "value"},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			transformFields(r, tt.params, nil, "", false, "")
			if diff := cmp.Diff(tt.want, tt.params); diff != "" {
				t.Errorf("transformFields(...): -want, +got:\n%s", diff)
			}
		})
	}
}
