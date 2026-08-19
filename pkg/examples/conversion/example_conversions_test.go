// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	jsoniter "github.com/json-iterator/go"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/types/conversion/tfjson"
)

func TestCollectSchemaTypeObjectCRDPaths(t *testing.T) {
	type args struct {
		tfResource *schema.Resource
		name       string
	}
	type want struct {
		paths []string
		err   error
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"RootLevelSchemaTypeObject": {
			reason: "Should collect a root-level SchemaTypeObject field path.",
			args: args{
				tfResource: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"object_block": {
							Type:     tfjson.SchemaTypeObject,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"inner_field": {
										Type: schema.TypeString,
									},
								},
							},
						},
					},
				},
				name: "test_resource",
			},
			want: want{
				paths: []string{"objectBlock"},
			},
		},
		"NoSchemaTypeObjectFields": {
			reason: "Should return nil when there are no SchemaTypeObject fields.",
			args: args{
				tfResource: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"regular_list": {
							Type:     schema.TypeList,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"element": {
										Type: schema.TypeString,
									},
								},
							},
						},
					},
				},
				name: "test_resource",
			},
			want: want{
				paths: nil,
			},
		},
		"NestedSchemaTypeObjectInsideList": {
			reason: "Should collect a SchemaTypeObject nested inside a TypeList with wildcard in path.",
			args: args{
				tfResource: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"parent_list": {
							Type: schema.TypeList,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"nested_object": {
										Type:     tfjson.SchemaTypeObject,
										MaxItems: 1,
										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{
												"value": {
													Type: schema.TypeString,
												},
											},
										},
									},
								},
							},
						},
					},
				},
				name: "test_resource",
			},
			want: want{
				paths: []string{"parentList[*].nestedObject"},
			},
		},
		"MultipleSchemaTypeObjectFields": {
			reason: "Should collect multiple SchemaTypeObject field paths.",
			args: args{
				tfResource: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"block_a": {
							Type:     tfjson.SchemaTypeObject,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"field": {
										Type: schema.TypeString,
									},
								},
							},
						},
						"block_b": {
							Type:     tfjson.SchemaTypeObject,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"field": {
										Type: schema.TypeString,
									},
								},
							},
						},
					},
				},
				name: "test_resource",
			},
			want: want{
				// Order is non-deterministic due to map iteration
				paths: []string{"blockA", "blockB"},
			},
		},
		"NestedSchemaTypeObjectInsideSchemaTypeObject": {
			reason: "Should collect nested SchemaTypeObject fields without wildcards between them.",
			args: args{
				tfResource: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"outer_object": {
							Type:     tfjson.SchemaTypeObject,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"inner_object": {
										Type:     tfjson.SchemaTypeObject,
										MaxItems: 1,
										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{
												"value": {
													Type: schema.TypeString,
												},
											},
										},
									},
								},
							},
						},
					},
				},
				name: "test_resource",
			},
			want: want{
				paths: []string{"outerObject", "outerObject.innerObject"},
			},
		},
	}
	for n, tt := range tests {
		t.Run(n, func(t *testing.T) {
			r := config.DefaultResource(tt.args.name, tt.args.tfResource, nil, nil)
			got, err := collectSchemaTypeObjectCRDPaths(r)
			if diff := cmp.Diff(tt.want.err, err); diff != "" {
				t.Fatalf("\n%s\ncollectSchemaTypeObjectCRDPaths(): -wantErr, +gotErr:\n%s", tt.reason, diff)
			}
			if diff := cmp.Diff(tt.want.paths, got, cmp.Transformer("sort", sortStrings)); diff != "" {
				t.Errorf("\n%s\ncollectSchemaTypeObjectCRDPaths(): -want, +got:\n%s", tt.reason, diff)
			}
		})
	}
}

func TestFlattenSchemaTypeObjectExamples(t *testing.T) {
	type args struct {
		obj   map[string]any
		paths []string
	}
	type want struct {
		obj map[string]any
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"NilPaths": {
			reason: "Flattening with nil paths should be a no-op.",
			args: args{
				obj: map[string]any{"a": "b"},
			},
			want: want{
				obj: map[string]any{"a": "b"},
			},
		},
		"EmptyPaths": {
			reason: "Flattening with empty paths should be a no-op.",
			args: args{
				obj:   map[string]any{"a": "b"},
				paths: []string{},
			},
			want: want{
				obj: map[string]any{"a": "b"},
			},
		},
		"FlattenRootLevelSingletonArray": {
			reason: "Should flatten a single-element array at the root level to a plain object.",
			args: args{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"block": []any{
								map[string]any{"key": "value"},
							},
						},
					},
				},
				paths: []string{"spec.forProvider.block"},
			},
			want: want{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"block": map[string]any{"key": "value"},
						},
					},
				},
			},
		},
		"SkipNonExistentPath": {
			reason: "Should skip paths that don't exist in the object.",
			args: args{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"existing": "value",
						},
					},
				},
				paths: []string{"spec.forProvider.nonExistent"},
			},
			want: want{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"existing": "value",
						},
					},
				},
			},
		},
		"SkipAlreadyFlattenedValue": {
			reason: "Should skip values that are already objects (not arrays).",
			args: args{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"block": map[string]any{"key": "value"},
						},
					},
				},
				paths: []string{"spec.forProvider.block"},
			},
			want: want{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"block": map[string]any{"key": "value"},
						},
					},
				},
			},
		},
		"FlattenNestedInsideList": {
			reason: "Should flatten SchemaTypeObject fields nested inside list elements using wildcards.",
			args: args{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"items": []any{
								map[string]any{
									"nested": []any{
										map[string]any{"key": "v1"},
									},
								},
								map[string]any{
									"nested": []any{
										map[string]any{"key": "v2"},
									},
								},
							},
						},
					},
				},
				paths: []string{"spec.forProvider.items[*].nested"},
			},
			want: want{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"items": []any{
								map[string]any{
									"nested": map[string]any{"key": "v1"},
								},
								map[string]any{
									"nested": map[string]any{"key": "v2"},
								},
							},
						},
					},
				},
			},
		},
		"FlattenNestedParentAndChild": {
			reason: "Should flatten both parent and child SchemaTypeObject fields, processing children first.",
			args: args{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"outer": []any{
								map[string]any{
									"inner": []any{
										map[string]any{"key": "value"},
									},
								},
							},
						},
					},
				},
				paths: []string{"spec.forProvider.outer", "spec.forProvider.outer.inner"},
			},
			want: want{
				obj: map[string]any{
					"spec": map[string]any{
						"forProvider": map[string]any{
							"outer": map[string]any{
								"inner": map[string]any{"key": "value"},
							},
						},
					},
				},
			},
		},
	}
	for n, tt := range tests {
		t.Run(n, func(t *testing.T) {
			obj, err := roundTrip(tt.args.obj)
			if err != nil {
				t.Fatalf("Failed to preprocess tt.args.obj: %v", err)
			}
			wantObj, err := roundTrip(tt.want.obj)
			if err != nil {
				t.Fatalf("Failed to preprocess tt.want.obj: %v", err)
			}
			flattenSchemaTypeObjectExamples(obj, tt.args.paths)
			if diff := cmp.Diff(wantObj, obj); diff != "" {
				t.Errorf("\n%s\nflattenSchemaTypeObjectExamples(): -want, +got:\n%s", tt.reason, diff)
			}
		})
	}
}

func sortStrings(s []string) []string {
	if s == nil {
		return nil
	}
	result := make([]string, len(s))
	copy(result, s)
	for i := range result {
		for j := i + 1; j < len(result); j++ {
			if result[i] > result[j] {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

func roundTrip(m map[string]any) (map[string]any, error) {
	if len(m) == 0 {
		return m, nil
	}
	buff, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(m)
	if err != nil {
		return nil, err
	}
	var r map[string]any
	return r, jsoniter.ConfigCompatibleWithStandardLibrary.Unmarshal(buff, &r)
}
