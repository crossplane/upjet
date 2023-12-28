// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/crossplane/upjet/pkg/registry"
)

func TestDefaultResource(t *testing.T) {
	type args struct {
		name              string
		sch               *schema.Resource
		frameworkResource *fwresource.Resource
		reg               *registry.Resource
		opts              []ResourceOption
	}

	cases := map[string]struct {
		reason string
		args   args
		want   *Resource
	}{
		"ThreeSectionsName": {
			reason: "It should return GVK properly for names with three sections",
			args: args{
				name: "aws_ec2_instance",
			},
			want: &Resource{
				Name:                           "aws_ec2_instance",
				ShortGroup:                     "ec2",
				Kind:                           "Instance",
				Version:                        "v1alpha1",
				ExternalName:                   NameAsIdentifier,
				References:                     map[string]Reference{},
				Sensitive:                      NopSensitive,
				UseAsync:                       true,
				SchemaElementOptions:           SchemaElementOptions{},
				ServerSideApplyMergeStrategies: ServerSideApplyMergeStrategies{},
			},
		},
		"TwoSectionsName": {
			reason: "It should return GVK properly for names with three sections",
			args: args{
				name: "aws_instance",
			},
			want: &Resource{
				Name:                           "aws_instance",
				ShortGroup:                     "aws",
				Kind:                           "Instance",
				Version:                        "v1alpha1",
				ExternalName:                   NameAsIdentifier,
				References:                     map[string]Reference{},
				Sensitive:                      NopSensitive,
				UseAsync:                       true,
				SchemaElementOptions:           SchemaElementOptions{},
				ServerSideApplyMergeStrategies: ServerSideApplyMergeStrategies{},
			},
		},
		"NameWithPrefixAcronym": {
			reason: "It should return prefix acronym in capital case",
			args: args{
				name: "aws_db_sql_server",
			},
			want: &Resource{
				Name:                           "aws_db_sql_server",
				ShortGroup:                     "db",
				Kind:                           "SQLServer",
				Version:                        "v1alpha1",
				ExternalName:                   NameAsIdentifier,
				References:                     map[string]Reference{},
				Sensitive:                      NopSensitive,
				UseAsync:                       true,
				SchemaElementOptions:           SchemaElementOptions{},
				ServerSideApplyMergeStrategies: ServerSideApplyMergeStrategies{},
			},
		},
		"NameWithSuffixAcronym": {
			reason: "It should return suffix acronym in capital case",
			args: args{
				name: "aws_db_server_id",
			},
			want: &Resource{
				Name:                           "aws_db_server_id",
				ShortGroup:                     "db",
				Kind:                           "ServerID",
				Version:                        "v1alpha1",
				ExternalName:                   NameAsIdentifier,
				References:                     map[string]Reference{},
				Sensitive:                      NopSensitive,
				UseAsync:                       true,
				SchemaElementOptions:           SchemaElementOptions{},
				ServerSideApplyMergeStrategies: ServerSideApplyMergeStrategies{},
			},
		},
		"NameWithMultipleAcronyms": {
			reason: "It should return both prefix & suffix acronyms in capital case",
			args: args{
				name: "aws_db_sql_server_id",
			},
			want: &Resource{
				Name:                           "aws_db_sql_server_id",
				ShortGroup:                     "db",
				Kind:                           "SQLServerID",
				Version:                        "v1alpha1",
				ExternalName:                   NameAsIdentifier,
				References:                     map[string]Reference{},
				Sensitive:                      NopSensitive,
				UseAsync:                       true,
				SchemaElementOptions:           SchemaElementOptions{},
				ServerSideApplyMergeStrategies: ServerSideApplyMergeStrategies{},
			},
		},
	}

	// TODO(muvaf): Find a way to compare function pointers.
	ignoreUnexported := []cmp.Option{
		cmpopts.IgnoreFields(Sensitive{}, "fieldPaths", "AdditionalConnectionDetailsFn"),
		cmpopts.IgnoreFields(LateInitializer{}, "ignoredCanonicalFieldPaths"),
		cmpopts.IgnoreFields(ExternalName{}, "SetIdentifierArgumentFn", "GetExternalNameFn", "GetIDFn"),
		cmpopts.IgnoreFields(Resource{}, "useNoForkClient"),
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := DefaultResource(tc.args.name, tc.args.sch, tc.args.frameworkResource, tc.args.reg, tc.args.opts...)
			if diff := cmp.Diff(tc.want, r, ignoreUnexported...); diff != "" {
				t.Errorf("\n%s\nDefaultResource(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestMoveToStatus(t *testing.T) {
	type args struct {
		sch    *schema.Resource
		fields []string
	}
	type want struct {
		sch *schema.Resource
	}

	cases := map[string]struct {
		reason string
		args
		want
	}{
		"DoesNotExist": {
			args: args{
				fields: []string{"topD"},
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {Type: schema.TypeString},
						"topB": {Type: schema.TypeInt},
						"topC": {Type: schema.TypeString, Optional: true},
					},
				},
			},
			want: want{
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {Type: schema.TypeString},
						"topB": {Type: schema.TypeInt},
						"topC": {Type: schema.TypeString, Optional: true},
					},
				},
			},
		},
		"TopLevelBasicFields": {
			args: args{
				fields: []string{"topA", "topB"},
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {Type: schema.TypeString},
						"topB": {Type: schema.TypeInt},
						"topC": {Type: schema.TypeString, Optional: true},
					},
				},
			},
			want: want{
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {
							Type:     schema.TypeString,
							Optional: false,
							Computed: true,
						},
						"topB": {
							Type:     schema.TypeInt,
							Optional: false,
							Computed: true,
						},
						"topC": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: false,
						},
					},
				},
			},
		},
		"ComplexFields": {
			args: args{
				fields: []string{"topA"},
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {
							Type: schema.TypeMap,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"leafA": {
										Type: schema.TypeMap,
										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{
												"leafB": {
													Type:     schema.TypeString,
													Computed: false,
													Optional: true,
												},
												"leafC": {
													Type:     schema.TypeString,
													Computed: false,
													Optional: true,
												},
											},
										},
									},
								},
							},
						},
						"topB": {Type: schema.TypeString},
					},
				},
			},
			want: want{
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {
							Type:     schema.TypeMap,
							Computed: true,
							Optional: false,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"leafA": {
										Type:     schema.TypeMap,
										Computed: true,
										Optional: false,
										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{
												"leafB": {
													Type:     schema.TypeString,
													Computed: true,
													Optional: false,
												},
												"leafC": {
													Type:     schema.TypeString,
													Computed: true,
													Optional: false,
												},
											},
										},
									},
								},
							},
						},
						"topB": {Type: schema.TypeString},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			MoveToStatus(tc.args.sch, tc.args.fields...)
			if diff := cmp.Diff(tc.want.sch, tc.args.sch); diff != "" {
				t.Errorf("\n%s\nMoveToStatus(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestMarkAsRequired(t *testing.T) {
	type args struct {
		sch    *schema.Resource
		fields []string
	}
	type want struct {
		sch *schema.Resource
	}

	cases := map[string]struct {
		reason string
		args
		want
	}{
		"DoesNotExist": {
			args: args{
				fields: []string{"topD"},
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {Type: schema.TypeString},
						"topB": {Type: schema.TypeInt, Computed: true},
						"topC": {Type: schema.TypeString, Optional: true},
					},
				},
			},
			want: want{
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {Type: schema.TypeString},
						"topB": {Type: schema.TypeInt, Computed: true},
						"topC": {Type: schema.TypeString, Optional: true},
					},
				},
			},
		},
		"TopLevelBasicFields": {
			args: args{
				fields: []string{"topB", "topC"},
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {Type: schema.TypeString},
						"topB": {Type: schema.TypeInt, Computed: true},
						"topC": {Type: schema.TypeString, Optional: true},
					},
				},
			},
			want: want{
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {Type: schema.TypeString},
						"topB": {
							Type:     schema.TypeInt,
							Optional: false,
							Computed: false,
						},
						"topC": {
							Type:     schema.TypeString,
							Optional: false,
							Computed: false,
						},
					},
				},
			},
		},
		"ComplexFields": {
			args: args{
				fields: []string{"topA.leafA", "topA.leafA.leafC"},
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {
							Type: schema.TypeMap,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"leafA": {
										Type: schema.TypeMap,
										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{
												"leafB": {Type: schema.TypeString},
												"leafC": {Type: schema.TypeString},
											},
										},
									},
								},
							},
						},
						"topB": {Type: schema.TypeString},
					},
				},
			},
			want: want{
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {
							Type: schema.TypeMap,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"leafA": {
										Type:     schema.TypeMap,
										Computed: false,
										Optional: false,
										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{
												"leafB": {Type: schema.TypeString},
												"leafC": {
													Type:     schema.TypeString,
													Computed: false,
													Optional: false,
												},
											},
										},
									},
								},
							},
						},
						"topB": {Type: schema.TypeString},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			MarkAsRequired(tc.args.sch, tc.args.fields...)
			if diff := cmp.Diff(tc.want.sch, tc.args.sch); diff != "" {
				t.Errorf("\n%s\nMarkAsRequired(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestGetSchema(t *testing.T) {
	type args struct {
		sch       *schema.Resource
		fieldpath string
	}
	type want struct {
		sch *schema.Schema
	}
	schLeaf := &schema.Schema{
		Type: schema.TypeString,
	}
	schA := &schema.Schema{
		Type: schema.TypeMap,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"fieldA": schLeaf,
			},
		},
	}
	res := &schema.Resource{
		Schema: map[string]*schema.Schema{
			"topA": schA,
		},
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"TopLevelField": {
			args: args{
				fieldpath: "topA",
				sch:       res,
			},
			want: want{
				sch: schA,
			},
		},
		"LeafField": {
			args: args{
				fieldpath: "topA.fieldA",
				sch:       res,
			},
			want: want{
				sch: schLeaf,
			},
		},
		"TopLevelFieldNotFound": {
			args: args{
				fieldpath: "topB",
				sch:       res,
			},
			want: want{
				sch: nil,
			},
		},
		"LeafFieldNotFound": {
			args: args{
				fieldpath: "topA.olala.omama",
				sch:       res,
			},
			want: want{
				sch: nil,
			},
		},
		"TopFieldIsNotMap": {
			args: args{
				fieldpath: "topA.topB",
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {Type: schema.TypeString},
					},
				},
			},
			want: want{
				sch: nil,
			},
		},
		"MiddleFieldIsNotResource": {
			args: args{
				fieldpath: "topA.topB.topC",
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"topB": {
										Elem: &schema.Schema{},
									},
								},
							},
						},
					},
				},
			},
			want: want{
				sch: nil,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sch := GetSchema(tc.args.sch, tc.args.fieldpath)
			if diff := cmp.Diff(tc.want.sch, sch); diff != "" {
				t.Errorf("\n%s\nGetSchema(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestManipulateAllFieldsInSchema(t *testing.T) {
	type args struct {
		sch *schema.Resource
		op  func(sch *schema.Schema)
	}
	type want struct {
		sch *schema.Resource
	}

	cases := map[string]struct {
		reason string
		args
		want
	}{
		"SetEmptyDescription": {
			args: args{
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {
							Description: "topADescription",
							Type:        schema.TypeMap,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"leafA": {
										Description: "leafADescription",
										Type:        schema.TypeMap,
										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{
												"leafB": {
													Description: "",
													Type:        schema.TypeString,
												},
												"leafC": {
													Description: "leafCDescription",
													Type:        schema.TypeString,
												},
											},
										},
									},
								},
							},
						},
						"topB": {Type: schema.TypeString},
					},
				},
				op: func(sch *schema.Schema) {
					sch.Description = ""
				},
			},
			want: want{
				sch: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"topA": {
							Description: "",
							Type:        schema.TypeMap,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"leafA": {
										Description: "",
										Type:        schema.TypeMap,
										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{
												"leafB": {
													Description: "",
													Type:        schema.TypeString,
												},
												"leafC": {
													Description: "",
													Type:        schema.TypeString,
												},
											},
										},
									},
								},
							},
						},
						"topB": {Type: schema.TypeString, Description: ""},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ManipulateEveryField(tc.args.sch, tc.args.op)
			if diff := cmp.Diff(tc.want.sch, tc.args.sch); diff != "" {
				t.Errorf("\n%s\nMoveToStatus(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
