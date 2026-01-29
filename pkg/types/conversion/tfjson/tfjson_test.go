// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package tfjson

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	tfjson "github.com/hashicorp/terraform-json"
	schemav2 "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestTfJSONBlockTypeToV2Schema(t *testing.T) {
	type args struct {
		nb *tfjson.SchemaBlockType
	}
	type want struct {
		schema *schemav2.Schema
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"SchemaNestingModeSingleWithRequiredChildren": {
			reason: "Plugin Framework single block with required children should be Required=true, Optional=false, Computed=false.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeSingle,
					MinItems:    0,
					MaxItems:    0,
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"uid": {
								Required: true,
							},
							"folder_uid": {
								Optional: true,
							},
						},
					},
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeList,
					Required: true,
					Optional: false,
					Computed: false,
					MinItems: 1,
					MaxItems: 1,
					Elem: &schemav2.Resource{
						Schema: map[string]*schemav2.Schema{
							"uid": {
								Required: true,
							},
							"folder_uid": {
								Optional: true,
							},
						},
					},
				},
			},
		},
		"SchemaNestingModeSingleWithOnlyOptionalChildren": {
			reason: "Plugin Framework single block with only optional children should be Required=false, Optional=true, Computed=false. This was the bug - it was incorrectly marked Computed=true.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeSingle,
					MinItems:    0,
					MaxItems:    0,
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"overwrite": {
								Optional: true,
							},
						},
					},
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeList,
					Required: false,
					Optional: true,
					Computed: false,
					MinItems: 0,
					MaxItems: 1,
					Elem: &schemav2.Resource{
						Schema: map[string]*schemav2.Schema{
							"overwrite": {
								Optional: true,
							},
						},
					},
				},
			},
		},
		"SchemaNestingModeSingleEmptyBlock": {
			reason: "Single block with empty block definition should be Optional and not Computed.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeSingle,
					MinItems:    0,
					MaxItems:    0,
					Block:       &tfjson.SchemaBlock{},
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeList,
					Required: false,
					Optional: true,
					Computed: false,
					MinItems: 0,
					MaxItems: 1,
					Elem: &schemav2.Resource{
						Schema: map[string]*schemav2.Schema{},
					},
				},
			},
		},
		"SchemaNestingModeSingleNilBlock": {
			reason: "Single block with nil block definition should be Optional and not Computed.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeSingle,
					MinItems:    0,
					MaxItems:    0,
					Block:       nil,
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeList,
					Required: false,
					Optional: true,
					Computed: false,
					MinItems: 0,
					MaxItems: 1,
				},
			},
		},
		"SchemaNestingModeSingleNestedBlockWithRequiredChildren": {
			reason: "Single block containing a nested block with required children should be Required because hasRequiredChild recurses.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeSingle,
					MinItems:    0,
					MaxItems:    0,
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"optional_attr": {
								Optional: true,
							},
						},
						NestedBlocks: map[string]*tfjson.SchemaBlockType{
							"nested": {
								NestingMode: tfjson.SchemaNestingModeSingle,
								MinItems:    0,
								MaxItems:    0,
								Block: &tfjson.SchemaBlock{
									Attributes: map[string]*tfjson.SchemaAttribute{
										"required_attr": {
											Required: true,
										},
									},
								},
							},
						},
					},
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeList,
					Required: true,
					Optional: false,
					Computed: false,
					MinItems: 1,
					MaxItems: 1,
					Elem: &schemav2.Resource{
						Schema: map[string]*schemav2.Schema{
							"optional_attr": {
								Optional: true,
							},
							"nested": {
								Type:     schemav2.TypeList,
								Required: true,
								Optional: false,
								Computed: false,
								MinItems: 1,
								MaxItems: 1,
								Elem: &schemav2.Resource{
									Schema: map[string]*schemav2.Schema{
										"required_attr": {
											Required: true,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"SchemaNestingModeListMinMaxZero": {
			reason: "SDK v2 list block with MinItems=0, MaxItems=0 should be Computed=true. This is the existing behavior we want to preserve.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeList,
					MinItems:    0,
					MaxItems:    0,
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"name": {
								Optional: true,
							},
						},
					},
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeList,
					Required: false,
					Optional: true,
					Computed: true,
					MinItems: 0,
					MaxItems: 0,
					Elem: &schemav2.Resource{
						Schema: map[string]*schemav2.Schema{
							"name": {
								Optional: true,
							},
						},
					},
				},
			},
		},
		"SchemaNestingModeListWithMinItems": {
			reason: "SDK v2 list block with MinItems=1 should not be Computed.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeList,
					MinItems:    1,
					MaxItems:    0,
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"name": {
								Required: true,
							},
						},
					},
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeList,
					Required: false,
					Optional: false,
					Computed: false,
					MinItems: 1,
					MaxItems: 0,
					Elem: &schemav2.Resource{
						Schema: map[string]*schemav2.Schema{
							"name": {
								Required: true,
							},
						},
					},
				},
			},
		},
		"SchemaNestingModeSetMinMaxZero": {
			reason: "SDK v2 set block with MinItems=0, MaxItems=0 should be Computed=true.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeSet,
					MinItems:    0,
					MaxItems:    0,
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"value": {
								Optional: true,
							},
						},
					},
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeSet,
					Required: false,
					Optional: true,
					Computed: true,
					MinItems: 0,
					MaxItems: 0,
					Elem: &schemav2.Resource{
						Schema: map[string]*schemav2.Schema{
							"value": {
								Optional: true,
							},
						},
					},
				},
			},
		},
		"SchemaNestingModeMapMinMaxZero": {
			reason: "SDK v2 map block with MinItems=0, MaxItems=0 should be Computed=true.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeMap,
					MinItems:    0,
					MaxItems:    0,
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"key": {
								Optional: true,
							},
						},
					},
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeMap,
					Required: false,
					Optional: true,
					Computed: true,
					MinItems: 0,
					MaxItems: 0,
					Elem: &schemav2.Resource{
						Schema: map[string]*schemav2.Schema{
							"key": {
								Optional: true,
							},
						},
					},
				},
			},
		},
		"SchemaNestingModeGroupWithOptionalChildren": {
			reason: "SchemaNestingModeGroup should be treated like SchemaNestingModeSingle - not Computed, and Optional when no required children.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					NestingMode: tfjson.SchemaNestingModeGroup,
					MinItems:    0,
					MaxItems:    0,
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"setting": {
								Optional: true,
							},
						},
					},
				},
			},
			want: want{
				schema: &schemav2.Schema{
					Type:     schemav2.TypeList,
					Required: false,
					Optional: true,
					Computed: false,
					MinItems: 0,
					MaxItems: 1,
					Elem: &schemav2.Resource{
						Schema: map[string]*schemav2.Schema{
							"setting": {
								Optional: true,
							},
						},
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tfJSONBlockTypeToV2Schema(tc.args.nb)
			if diff := cmp.Diff(tc.want.schema, got); diff != "" {
				t.Errorf("%s\ntfJSONBlockTypeToV2Schema(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestHasRequiredChild(t *testing.T) {
	type args struct {
		nb *tfjson.SchemaBlockType
	}
	type want struct {
		result bool
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NilBlock": {
			reason: "A block type with nil Block should return false.",
			args: args{
				nb: &tfjson.SchemaBlockType{Block: nil},
			},
			want: want{
				result: false,
			},
		},
		"EmptyBlock": {
			reason: "A block type with empty Block should return false.",
			args: args{
				nb: &tfjson.SchemaBlockType{Block: &tfjson.SchemaBlock{}},
			},
			want: want{
				result: false,
			},
		},
		"OnlyOptionalAttributes": {
			reason: "A block with only optional attributes should return false.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"optional1": {Optional: true},
							"optional2": {Optional: true},
						},
					},
				},
			},
			want: want{
				result: false,
			},
		},
		"HasRequiredAttribute": {
			reason: "A block with at least one required attribute should return true.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"required": {Required: true},
							"optional": {Optional: true},
						},
					},
				},
			},
			want: want{
				result: true,
			},
		},
		"NestedBlockWithRequiredChild": {
			reason: "A block with a nested block that has required children should return true.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"optional": {Optional: true},
						},
						NestedBlocks: map[string]*tfjson.SchemaBlockType{
							"nested": {
								Block: &tfjson.SchemaBlock{
									Attributes: map[string]*tfjson.SchemaAttribute{
										"required": {Required: true},
									},
								},
							},
						},
					},
				},
			},
			want: want{
				result: true,
			},
		},
		"DeeplyNestedRequiredChild": {
			reason: "A block with deeply nested required children should return true.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					Block: &tfjson.SchemaBlock{
						NestedBlocks: map[string]*tfjson.SchemaBlockType{
							"level1": {
								Block: &tfjson.SchemaBlock{
									NestedBlocks: map[string]*tfjson.SchemaBlockType{
										"level2": {
											Block: &tfjson.SchemaBlock{
												Attributes: map[string]*tfjson.SchemaAttribute{
													"required": {Required: true},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: want{
				result: true,
			},
		},
		"NilAttributeInMap": {
			reason: "Nil attributes in the map should be safely skipped.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					Block: &tfjson.SchemaBlock{
						Attributes: map[string]*tfjson.SchemaAttribute{
							"nil_attr": nil,
							"optional": {Optional: true},
						},
					},
				},
			},
			want: want{
				result: false,
			},
		},
		"NilNestedBlockInMap": {
			reason: "Nil nested blocks in the map should be safely skipped.",
			args: args{
				nb: &tfjson.SchemaBlockType{
					Block: &tfjson.SchemaBlock{
						NestedBlocks: map[string]*tfjson.SchemaBlockType{
							"nil_block": nil,
						},
					},
				},
			},
			want: want{
				result: false,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := hasRequiredChild(tc.args.nb)
			if got != tc.want.result {
				t.Errorf("%s\nhasRequiredChild(...) = %v, want %v", tc.reason, got, tc.want.result)
			}
		})
	}
}
