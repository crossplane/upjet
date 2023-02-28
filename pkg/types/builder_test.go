/*
 Copyright 2021 Upbound Inc.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package types

import (
	"fmt"
	"go/token"
	"go/types"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/config"
)

func TestBuilder_generateTypeName(t *testing.T) {
	type args struct {
		existing []string
		suffix   string
		names    []string
	}
	type want struct {
		out string
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"NoExisting": {
			args: args{
				existing: []string{
					"SomeOtherType",
				},
				suffix: "Parameters",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkParameters",
				err: nil,
			},
		},
		"NoExistingMultipleIndexes": {
			args: args{
				existing: []string{
					"SomeOtherType",
				},
				suffix: "Parameters",
				names: []string{
					"RouterNat",
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkParameters",
				err: nil,
			},
		},
		"NoIndexExists": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
				},
				suffix: "Parameters",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkParameters_2",
				err: nil,
			},
		},
		"MultipleIndexesExist": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
					"SubnetworkParameters_2",
					"SubnetworkParameters_3",
					"SubnetworkParameters_4",
				},
				suffix: "Parameters",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkParameters_5",
				err: nil,
			},
		},
		"ErrIfAllIndexesExist": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
					"SubnetworkParameters_2",
					"SubnetworkParameters_3",
					"SubnetworkParameters_4",
					"SubnetworkParameters_5",
					"SubnetworkParameters_6",
					"SubnetworkParameters_7",
					"SubnetworkParameters_8",
					"SubnetworkParameters_9",
				},
				suffix: "Parameters",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				err: errors.Errorf("could not generate a unique name for %s", "SubnetworkParameters"),
			},
		},
		"MultipleNamesPrependsBeforeIndexing": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
				},
				suffix: "Parameters",
				names: []string{
					"RouterNat",
					"Subnetwork",
				},
			},
			want: want{
				out: "RouterNatSubnetworkParameters",
				err: nil,
			},
		},
		"MultipleNamesUsesIndexingIfNeeded": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
					"RouterNatSubnetworkParameters",
				},
				suffix: "Parameters",
				names: []string{
					"RouterNat",
					"Subnetwork",
				},
			},
			want: want{
				out: "RouterNatSubnetworkParameters_2",
				err: nil,
			},
		},
		"AnySuffixWouldWorkSame": {
			args: args{
				existing: []string{
					"SubnetworkObservation",
					"SubnetworkObservation_2",
					"SubnetworkObservation_3",
					"SubnetworkObservation_4",
				},
				suffix: "Observation",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkObservation_5",
				err: nil,
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			p := types.NewPackage("path/to/test", "test")
			for _, s := range tc.existing {
				p.Scope().Insert(types.NewTypeName(token.NoPos, p, s, &types.Struct{}))
			}

			g := &Builder{
				Package: p,
			}
			got, gotErr := generateTypeName(tc.args.suffix, g.Package, tc.args.names...)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("generateTypeName(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("generateTypeName(...) out = %v, want %v", got, tc.want.out)
			}
		})
	}
}

func TestBuild(t *testing.T) {
	type args struct {
		cfg *config.Resource
	}
	type want struct {
		forProvider string
		atProvider  string
		err         error
	}
	cases := map[string]struct {
		args
		want
	}{
		"Base_Types": {
			args: args{
				cfg: &config.Resource{
					TerraformResource: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"name": {
								Type:     schema.TypeString,
								Required: true,
							},
							"id": {
								Type:     schema.TypeInt,
								Required: true,
							},
							"enable": {
								Type:     schema.TypeBool,
								Optional: true,
								Computed: true,
							},
							"value": {
								Type:     schema.TypeFloat,
								Optional: false,
								Computed: true,
							},
							"config": {
								Type:     schema.TypeString,
								Optional: false,
								Computed: true,
							},
						},
					},
				},
			},
			want: want{
				forProvider: `type example.Parameters struct{Enable *bool "json:\"enable,omitempty\" tf:\"enable,omitempty\""; ID *int64 "json:\"id,omitempty\" tf:\"id,omitempty\""; Name *string "json:\"name,omitempty\" tf:\"name,omitempty\""}`,
				atProvider:  `type example.Observation struct{Config *string "json:\"config,omitempty\" tf:\"config,omitempty\""; Enable *bool "json:\"enable,omitempty\" tf:\"enable,omitempty\""; ID *int64 "json:\"id,omitempty\" tf:\"id,omitempty\""; Name *string "json:\"name,omitempty\" tf:\"name,omitempty\""; Value *float64 "json:\"value,omitempty\" tf:\"value,omitempty\""}`,
			},
		},
		"Resource_Types": {
			args: args{
				cfg: &config.Resource{
					TerraformResource: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"list": {
								Type:     schema.TypeList,
								Required: true,
								Elem: &schema.Schema{
									Type:     schema.TypeString,
									Required: true,
								},
							},
							"resource_in": {
								Type:     schema.TypeMap,
								Required: true,
								Elem:     &schema.Resource{},
							},
							"resource_out": {
								Type:     schema.TypeMap,
								Optional: false,
								Computed: true,
								Elem:     &schema.Resource{},
							},
						},
					},
				},
			},
			want: want{
				forProvider: `type example.Parameters struct{List []*string "json:\"list,omitempty\" tf:\"list,omitempty\""; ResourceIn map[string]example.ResourceInParameters "json:\"resourceIn,omitempty\" tf:\"resource_in,omitempty\""}`,
				atProvider:  `type example.Observation struct{List []*string "json:\"list,omitempty\" tf:\"list,omitempty\""; ResourceIn map[string]example.ResourceInParameters "json:\"resourceIn,omitempty\" tf:\"resource_in,omitempty\""; ResourceOut map[string]example.ResourceOutObservation "json:\"resourceOut,omitempty\" tf:\"resource_out,omitempty\""}`,
			},
		},
		"Sensitive_Fields": {
			args: args{
				cfg: &config.Resource{
					TerraformResource: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"key_1": {
								Type:      schema.TypeString,
								Optional:  true,
								Sensitive: true,
							},
							"key_2": {
								Type:      schema.TypeString,
								Sensitive: true,
							},
							"key_3": {
								Type:      schema.TypeList,
								Sensitive: true,
							},
						},
					},
				},
			},
			want: want{
				forProvider: `type example.Parameters struct{Key1SecretRef *github.com/crossplane/crossplane-runtime/apis/common/v1.SecretKeySelector "json:\"key1SecretRef,omitempty\" tf:\"-\""; Key2SecretRef github.com/crossplane/crossplane-runtime/apis/common/v1.SecretKeySelector "json:\"key2SecretRef\" tf:\"-\""; Key3SecretRef []github.com/crossplane/crossplane-runtime/apis/common/v1.SecretKeySelector "json:\"key3SecretRef\" tf:\"-\""}`,
				atProvider:  `type example.Observation struct{}`,
			},
		},
		"Invalid_Sensitive_Fields": {
			args: args{
				cfg: &config.Resource{
					TerraformResource: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"key_1": {
								Type:      schema.TypeFloat,
								Sensitive: true,
							},
						},
					},
				},
			},
			want: want{
				err: errors.Wrapf(fmt.Errorf(`got type %q for field %q, only types "string", "*string", []string, []*string, "map[string]string" and "map[string]*string" supported as sensitive`, "*float64", "Key1"), "cannot build the Types"),
			},
		},
		"References": {
			args: args{
				cfg: &config.Resource{
					TerraformResource: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"name": {
								Type:     schema.TypeString,
								Required: true,
							},
							"reference_id": {
								Type:     schema.TypeString,
								Required: true,
							},
						},
					},
					References: map[string]config.Reference{
						"reference_id": {
							Type:         "string",
							RefFieldName: "ExternalResourceID",
						},
					},
				},
			},
			want: want{
				forProvider: `type example.Parameters struct{Name *string "json:\"name,omitempty\" tf:\"name,omitempty\""; ReferenceID *string "json:\"referenceId,omitempty\" tf:\"reference_id,omitempty\""; ExternalResourceID *github.com/crossplane/crossplane-runtime/apis/common/v1.Reference "json:\"externalResourceId,omitempty\" tf:\"-\""; ReferenceIDSelector *github.com/crossplane/crossplane-runtime/apis/common/v1.Selector "json:\"referenceIdSelector,omitempty\" tf:\"-\""}`,
				atProvider:  `type example.Observation struct{Name *string "json:\"name,omitempty\" tf:\"name,omitempty\""; ReferenceID *string "json:\"referenceId,omitempty\" tf:\"reference_id,omitempty\""}`,
			},
		},
		"Invalid_Schema_Type": {
			args: args{
				cfg: &config.Resource{
					TerraformResource: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"name": {
								Type:     schema.TypeInvalid,
								Required: true,
							},
						},
					},
				},
			},
			want: want{
				err: errors.Wrapf(errors.Wrapf(errors.Errorf("invalid schema type %s", "TypeInvalid"), "cannot infer type from schema of field %s", "name"), "cannot build the Types"),
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			builder := NewBuilder(types.NewPackage("example", ""))
			g, err := builder.Build(tc.cfg)

			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Fatalf("Build(...): -want error, +got error: %s", diff)
			}
			if g.ForProviderType != nil {
				if diff := cmp.Diff(tc.want.forProvider, g.ForProviderType.Obj().String(), test.EquateErrors()); diff != "" {
					t.Fatalf("Build(...): -want forProvider, +got forProvider: %s", diff)
				}
			}
			if g.AtProviderType != nil {
				if diff := cmp.Diff(tc.want.atProvider, g.AtProviderType.Obj().String(), test.EquateErrors()); diff != "" {
					t.Fatalf("Build(...): -want atProvider, +got atProvider: %s", diff)
				}
			}
		})
	}
}
