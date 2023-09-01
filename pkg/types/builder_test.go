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
		forProvider     string
		atProvider      string
		validationRules string
		err             error
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
				forProvider: `type example.Parameters struct{Enable *bool "json:\"enable,omitempty\" tf:\"enable,omitempty\""; ID *int64 "json:\"id\" tf:\"id,omitempty\""; Name *string "json:\"name\" tf:\"name,omitempty\""}`,
				atProvider:  `type example.Observation struct{Config *string "json:\"config,omitempty\" tf:\"config,omitempty\""; Enable *bool "json:\"enable,omitempty\" tf:\"enable,omitempty\""; ID *int64 "json:\"id,omitempty\" tf:\"id,omitempty\""; Name *string "json:\"name,omitempty\" tf:\"name,omitempty\""; Value *float64 "json:\"value,omitempty\" tf:\"value,omitempty\""}`,
				validationRules: `
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.id) || has(self.initProvider.id)",message="id is a required parameter"
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.name) || has(self.initProvider.name)",message="name is a required parameter"`,
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
				forProvider: `type example.Parameters struct{List []*string "json:\"list\" tf:\"list,omitempty\""; ResourceIn map[string]example.ResourceInParameters "json:\"resourceIn\" tf:\"resource_in,omitempty\""}`,
				atProvider:  `type example.Observation struct{List []*string "json:\"list,omitempty\" tf:\"list,omitempty\""; ResourceIn map[string]example.ResourceInParameters "json:\"resourceIn,omitempty\" tf:\"resource_in,omitempty\""; ResourceOut map[string]example.ResourceOutObservation "json:\"resourceOut,omitempty\" tf:\"resource_out,omitempty\""}`,
				validationRules: `
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.list) || has(self.initProvider.list)",message="list is a required parameter"
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.resourceIn) || has(self.initProvider.resourceIn)",message="resourceIn is a required parameter"`,
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
				validationRules: `
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.key2SecretRef)",message="key2SecretRef is a required parameter"
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.key3SecretRef)",message="key3SecretRef is a required parameter"`,
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
				forProvider: `type example.Parameters struct{Name *string "json:\"name\" tf:\"name,omitempty\""; ReferenceID *string "json:\"referenceId,omitempty\" tf:\"reference_id,omitempty\""; ExternalResourceID *github.com/crossplane/crossplane-runtime/apis/common/v1.Reference "json:\"externalResourceId,omitempty\" tf:\"-\""; ReferenceIDSelector *github.com/crossplane/crossplane-runtime/apis/common/v1.Selector "json:\"referenceIdSelector,omitempty\" tf:\"-\""}`,
				atProvider:  `type example.Observation struct{Name *string "json:\"name,omitempty\" tf:\"name,omitempty\""; ReferenceID *string "json:\"referenceId,omitempty\" tf:\"reference_id,omitempty\""}`,
				validationRules: `
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.name) || has(self.initProvider.name)",message="name is a required parameter"`,
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
		"Validation_Rules_With_Keywords": {
			args: args{
				cfg: &config.Resource{
					TerraformResource: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"name": {
								Type:     schema.TypeString,
								Required: true,
							},
							// "namespace" is a cel reserved value and should be wrapped when used in
							// validation rules (i.e., __namespace__)
							"namespace": {
								Type:     schema.TypeString,
								Required: true,
							},
						},
					},
				},
			},
			want: want{
				forProvider: `type example.Parameters struct{Name *string "json:\"name\" tf:\"name,omitempty\""; Namespace *string "json:\"namespace\" tf:\"namespace,omitempty\""}`,
				atProvider:  `type example.Observation struct{Name *string "json:\"name,omitempty\" tf:\"name,omitempty\""; Namespace *string "json:\"namespace,omitempty\" tf:\"namespace,omitempty\""}`,
				validationRules: `
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.name) || has(self.initProvider.name)",message="name is a required parameter"
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.__namespace__) || has(self.initProvider.__namespace__)",message="__namespace__ is a required parameter"`,
			},
		},
		"Nested_Required_Fields": {
			args: args{
				cfg: &config.Resource{
					TerraformResource: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"nested": {
								Type:     schema.TypeList,
								Required: true,
								Elem: &schema.Resource{
									Schema: map[string]*schema.Schema{
										"nested_required": {
											Type:     schema.TypeString,
											Required: true,
										},
										"nested_optional": {
											Type:     schema.TypeString,
											Optional: true,
										},
									},
								},
							},
						},
					},
				},
			},
			want: want{
				forProvider: `type example.Parameters struct{Nested []example.NestedParameters "json:\"nested\" tf:\"nested,omitempty\""}`,
				atProvider:  `type example.Observation struct{Nested []example.NestedObservation "json:\"nested,omitempty\" tf:\"nested,omitempty\""}`,
				validationRules: `
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || !has(self.forProvider.nested) || has(self.forProvider.nested[0].nestedRequired) || has(self.initProvider.nested[0].nestedRequired)",message="nested[0].nestedRequired is a required parameter"
// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.nested) || has(self.initProvider.nested)",message="nested is a required parameter"`,
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
				if diff := cmp.Diff(tc.want.forProvider, g.ForProviderType.Obj().String()); diff != "" {
					t.Fatalf("Build(...): -want forProvider, +got forProvider: %s", diff)
				}
			}
			if g.AtProviderType != nil {
				if diff := cmp.Diff(tc.want.atProvider, g.AtProviderType.Obj().String()); diff != "" {
					t.Fatalf("Build(...): -want atProvider, +got atProvider: %s", diff)
				}
			}
			if diff := cmp.Diff(tc.want.validationRules, g.ValidationRules); diff != "" {
				t.Fatalf("Build(...): -want validationRules, +got validationRules: %s", diff)
			}
		})
	}
}

func TestConstructCELRule(t *testing.T) {
	type args struct {
		celPath []string
		isInit  bool
	}
	type want struct {
		celRule *celRule
	}
	cases := map[string]struct {
		args
		want
	}{
		"EmptyCELPath": {
			args: args{
				celPath: []string{},
				isInit:  true,
			},
			want: want{
				celRule: &celRule{},
			},
		},
		"SimpleCELPath": {
			args: args{
				celPath: []string{"required"},
				isInit:  true,
			},
			want: want{
				celRule: newCelRule(
					"has(self.forProvider.required)",
					"has(self.initProvider.required)",
					"required"),
			},
		},
		"SimpleNoInitCELPath": {
			args: args{
				celPath: []string{"required"},
				isInit:  false,
			},
			want: want{
				celRule: newCelRule(
					"has(self.forProvider.required)",
					"",
					"required"),
			},
		},
		"NestedCELPathMap": {
			args: args{
				celPath: []string{"required", "nested"},
				isInit:  true,
			},
			want: want{
				celRule: newCelRule(
					"has(self.forProvider.required.nested)",
					"has(self.initProvider.required.nested)",
					"required.nested"),
			},
		},
		"NestedCELPathList": {
			args: args{
				celPath: []string{"required", "*", "nested"},
				isInit:  true,
			},
			want: want{
				celRule: newCelRule(
					"!has(self.forProvider.required) || has(self.forProvider.required[0].nested)",
					"has(self.initProvider.required[0].nested)",
					"required[0].nested"),
			},
		},
		"DeepNestedCELPathList": {
			args: args{
				celPath: []string{"required", "*", "nested", "*", "deepNested", "*", "evenDeeperNested"},
				isInit:  true,
			},
			want: want{
				celRule: newCelRule(
					"!has(self.forProvider.required) || !has(self.forProvider.required[0].nested) || !has(self.forProvider.required[0].nested[0].deepNested) || has(self.forProvider.required[0].nested[0].deepNested[0].evenDeeperNested)",
					"has(self.initProvider.required[0].nested[0].deepNested[0].evenDeeperNested)",
					"required[0].nested[0].deepNested[0].evenDeeperNested"),
			},
		},
		"NestedCELPathListEndInWildcard": {
			args: args{
				celPath: []string{"required", "*", "nested", "*"},
				isInit:  true,
			},
			want: want{
				celRule: newCelRule(
					"!has(self.forProvider.required) || has(self.forProvider.required[0].nested)",
					"has(self.initProvider.required[0].nested)",
					"required[0].nested"),
			},
		},
		"NestedCELPathListReservedKeyword": {
			args: args{
				celPath: []string{"namespace", "*", "nested"},
				isInit:  true,
			},
			want: want{
				celRule: newCelRule(
					"!has(self.forProvider.__namespace__) || has(self.forProvider.__namespace__[0].nested)",
					"has(self.initProvider.__namespace__[0].nested)",
					"__namespace__[0].nested"),
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			res := constructCELRules(tc.args.celPath, tc.isInit)

			if diff := cmp.Diff(tc.want.celRule.rule, res.rule); diff != "" {
				t.Fatalf("Build(...): -want rule, +got rule: %s", diff)
			}
			if diff := cmp.Diff(tc.want.celRule.path, res.path); diff != "" {
				t.Fatalf("Build(...): -want path, +got path: %s", diff)
			}
			if diff := cmp.Diff(tc.want.celRule.initProviderRule, res.initProviderRule); diff != "" {
				t.Fatalf("Build(...): -want initProviderRule, +got initProviderRule: %s", diff)
			}
		})
	}
}
