// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestSingletonListEmbedder(t *testing.T) {
	type args struct {
		resource *schema.Resource
		name     string
	}
	type want struct {
		err             error
		schemaOpts      SchemaElementOptions
		conversionPaths map[string]string
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"SuccessfulRootLevelSingletonListEmbedding": {
			reason: "Successfully embed a root-level singleton list in the resource schema.",
			args: args{
				resource: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"singleton_list": {
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
				schemaOpts: map[string]*SchemaElementOption{
					"singleton_list": {
						EmbeddedObject: true,
					},
				},
				conversionPaths: map[string]string{
					"singleton_list": "singletonList",
				},
			},
		},
		"NoEmbeddingForMultiItemList": {
			reason: "Do not embed a list with a MaxItems constraint greater than 1.",
			args: args{
				resource: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"multiitem_list": {
							Type:     schema.TypeList,
							MaxItems: 2,
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
				schemaOpts:      map[string]*SchemaElementOption{},
				conversionPaths: map[string]string{},
			},
		},
		"NoEmbeddingForNonList": {
			reason: "Do not embed a non-list schema.",
			args: args{
				resource: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"invalid": {
							Type: schema.TypeInvalid,
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
				schemaOpts:      map[string]*SchemaElementOption{},
				conversionPaths: map[string]string{},
			},
		},
		"SuccessfulNestedSingletonListEmbedding": {
			reason: "Successfully embed a nested singleton list in the resource schema.",
			args: args{
				resource: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"parent_list": {
							Type:     schema.TypeList,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"child_list": {
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
						},
					},
				},
				name: "test_resource",
			},
			want: want{
				schemaOpts: map[string]*SchemaElementOption{
					"parent_list": {
						EmbeddedObject: true,
					},
					"parent_list.child_list": {
						EmbeddedObject: true,
					},
				},
				conversionPaths: map[string]string{
					"parent_list":               "parentList",
					"parent_list[*].child_list": "parentList[*].childList",
				},
			},
		},
	}
	for n, tt := range tests {
		t.Run(n, func(t *testing.T) {
			e := &SingletonListEmbedder{}
			r := DefaultResource(tt.args.name, tt.args.resource, nil, nil)
			err := TraverseSchemas(tt.args.name, r, e)
			if diff := cmp.Diff(tt.want.err, err, test.EquateErrors()); diff != "" {
				t.Fatalf("\n%s\ntraverseSchemas(name, schema, ...): -wantErr, +gotErr:\n%s", tt.reason, diff)
			}
			if diff := cmp.Diff(tt.want.schemaOpts, r.SchemaElementOptions); diff != "" {
				t.Errorf("\n%s\ntraverseSchemas(name, schema, ...): -wantOptions, +gotOptions:\n%s", tt.reason, diff)
			}
			if diff := cmp.Diff(tt.want.conversionPaths, r.listConversionPaths); diff != "" {
				t.Errorf("\n%s\ntraverseSchemas(name, schema, ...): -wantPaths, +gotPaths:\n%s", tt.reason, diff)
			}
		})
	}
}
