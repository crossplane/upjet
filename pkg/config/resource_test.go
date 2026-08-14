// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"go/types"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kind     = "ACoolService"
	name     = "example-service"
	provider = "ACoolProvider"
)

func TestTaggerInitialize(t *testing.T) {
	errBoom := errors.New("boom")

	type args struct {
		mg   xpresource.Managed
		kube client.Client
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"Successful": {
			args: args{
				mg:   &fake.Managed{},
				kube: &test.MockClient{MockUpdate: test.NewMockUpdateFn(nil)},
			},
			want: want{
				err: nil,
			},
		},
		"Failure": {
			args: args{
				mg:   &fake.Managed{},
				kube: &test.MockClient{MockUpdate: test.NewMockUpdateFn(errBoom)},
			},
			want: want{
				err: errBoom,
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			tagger := NewTagger(tc.kube, "tags")
			gotErr := tagger.Initialize(context.TODO(), tc.mg)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("generateTypeName(...): -want error, +got error: %s", diff)
			}
		})
	}
}

func TestSetExternalTagsWithPaved(t *testing.T) {
	type args struct {
		externalTags map[string]string
		paved        *fieldpath.Paved
		fieldName    string
	}
	type want struct {
		pavedString string
		err         error
	}
	cases := map[string]struct {
		args
		want
	}{
		"Successful": {
			args: args{
				externalTags: map[string]string{
					xpresource.ExternalResourceTagKeyKind:     kind,
					xpresource.ExternalResourceTagKeyName:     name,
					xpresource.ExternalResourceTagKeyProvider: provider,
				},
				paved:     fieldpath.Pave(map[string]any{}),
				fieldName: "tags",
			},
			want: want{
				pavedString: fmt.Sprintf(`{"spec":{"forProvider":{"tags":{"%s":"%s","%s":"%s","%s":"%s"}}}}`,
					xpresource.ExternalResourceTagKeyKind, kind,
					xpresource.ExternalResourceTagKeyName, name,
					xpresource.ExternalResourceTagKeyProvider, provider),
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			gotByte, gotErr := setExternalTagsWithPaved(tc.externalTags, tc.paved, tc.fieldName)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("generateTypeName(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.pavedString, string(gotByte), test.EquateErrors()); diff != "" {
				t.Fatalf("generateTypeName(...): -want gotByte, +got gotByte: %s", diff)
			}
		})
	}
}

func TestAddSingletonListConversion(t *testing.T) {
	type args struct {
		r       func() *Resource
		tfPath  string
		crdPath string
	}
	type want struct {
		r func() *Resource
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"AddNonWildcardTFPath": {
			reason: "A non-wildcard TF path of a singleton list should successfully be configured to be converted into an embedded object.",
			args: args{
				tfPath:  "singleton_list",
				crdPath: "singletonList",
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.AddSingletonListConversion("singleton_list", "singletonList")
					return r
				},
			},
			want: want{
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.SchemaElementOptions = SchemaElementOptions{}
					r.SchemaElementOptions["singleton_list"] = &SchemaElementOption{
						EmbeddedObject: true,
					}
					r.listConversionPaths["singleton_list"] = "singletonList"
					return r
				},
			},
		},
		"AddWildcardTFPath": {
			reason: "A wildcard TF path of a singleton list should successfully be configured to be converted into an embedded object.",
			args: args{
				tfPath:  "parent[*].singleton_list",
				crdPath: "parent[*].singletonList",
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.AddSingletonListConversion("parent[*].singleton_list", "parent[*].singletonList")
					return r
				},
			},
			want: want{
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.SchemaElementOptions = SchemaElementOptions{}
					r.SchemaElementOptions["parent.singleton_list"] = &SchemaElementOption{
						EmbeddedObject: true,
					}
					r.listConversionPaths["parent[*].singleton_list"] = "parent[*].singletonList"
					return r
				},
			},
		},
		"AddIndexedTFPath": {
			reason: "An indexed TF path of a singleton list should successfully be configured to be converted into an embedded object.",
			args: args{
				tfPath:  "parent[0].singleton_list",
				crdPath: "parent[0].singletonList",
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.AddSingletonListConversion("parent[0].singleton_list", "parent[0].singletonList")
					return r
				},
			},
			want: want{
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.SchemaElementOptions = SchemaElementOptions{}
					r.SchemaElementOptions["parent.singleton_list"] = &SchemaElementOption{
						EmbeddedObject: true,
					}
					r.listConversionPaths["parent[0].singleton_list"] = "parent[0].singletonList"
					return r
				},
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			r := tc.args.r()
			r.AddSingletonListConversion(tc.args.tfPath, tc.args.crdPath)
			wantR := tc.want.r()
			if diff := cmp.Diff(wantR.listConversionPaths, r.listConversionPaths); diff != "" {
				t.Errorf("%s\nAddSingletonListConversion(tfPath): -wantConversionPaths, +gotConversionPaths: \n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(wantR.SchemaElementOptions, r.SchemaElementOptions); diff != "" {
				t.Errorf("%s\nAddSingletonListConversion(tfPath): -wantSchemaElementOptions, +gotSchemaElementOptions: \n%s", tc.reason, diff)
			}
		})
	}
}

func TestRemoveSingletonListConversion(t *testing.T) {
	type args struct {
		r      func() *Resource
		tfPath string
	}
	type want struct {
		removed bool
		r       func() *Resource
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"RemoveWildcardListConversion": {
			reason: "An existing wildcard list conversion can successfully be removed.",
			args: args{
				tfPath: "parent[*].singleton_list",
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.AddSingletonListConversion("parent[*].singleton_list", "parent[*].singletonList")
					return r
				},
			},
			want: want{
				removed: true,
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					return r
				},
			},
		},
		"RemoveIndexedListConversion": {
			reason: "An existing indexed list conversion can successfully be removed.",
			args: args{
				tfPath: "parent[0].singleton_list",
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.AddSingletonListConversion("parent[0].singleton_list", "parent[0].singletonList")
					return r
				},
			},
			want: want{
				removed: true,
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					return r
				},
			},
		},
		"NonExistingListConversion": {
			reason: "A list conversion path that does not exist cannot be removed.",
			args: args{
				tfPath: "non-existent",
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.AddSingletonListConversion("parent[*].singleton_list", "parent[*].singletonList")
					return r
				},
			},
			want: want{
				removed: false,
				r: func() *Resource {
					r := DefaultResource("test_resource", nil, nil, nil)
					r.AddSingletonListConversion("parent[*].singleton_list", "parent[*].singletonList")
					return r
				},
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			r := tc.args.r()
			got := r.RemoveSingletonListConversion(tc.args.tfPath)
			if diff := cmp.Diff(tc.want.removed, got); diff != "" {
				t.Errorf("%s\nRemoveSingletonListConversion(tfPath): -wantRemoved, +gotRemoved: \n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.r().listConversionPaths, r.listConversionPaths); diff != "" {
				t.Errorf("%s\nRemoveSingletonListConversion(tfPath): -wantConversionPaths, +gotConversionPaths: \n%s", tc.reason, diff)
			}
		})
	}
}

// scalarAtXY returns a Terraform resource schema where x is
// a collection type (list) and x.y is a scalar (int).
func scalarAtXY() *schema.Resource {
	return &schema.Resource{
		Schema: map[string]*schema.Schema{
			"x": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"y": {
							Type:     schema.TypeInt,
							Optional: true,
						},
					},
				},
			},
		},
	}
}

// scalarAtXYZ returns a Terraform resource schema where x & x.y are
// a collection types (list) and x.y.z is a scalar (int).
func scalarAtXYZ() *schema.Resource {
	return &schema.Resource{
		Schema: map[string]*schema.Schema{
			"x": {
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"y": {
							Type:     schema.TypeList,
							Optional: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"z": {
										Type:     schema.TypeInt,
										Optional: true,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestFieldTypeOverride(t *testing.T) {
	strType := types.Typ[types.String]
	type args struct {
		r func() *Resource
		// overridePath is the path passed to Resource.OverrideScalarFieldType.
		overridePath string
		// overrideType is the type passed to Resource.OverrideScalarFieldType.
		overrideType types.Type
		// path is the path queried via Resource.FieldTypeOverride.
		path string
	}
	type want struct {
		// parameterType is the expected ParameterTypeOverride.String(),
		// or the empty string when there's no override configured.
		parameterType string
		// err is the expected error from Resource.OverrideScalarFieldType.
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"OverrideConfigured": {
			reason: "A scalar type override set for a path is returned for that path.",
			args: args{
				path:         "x.y",
				overridePath: "x.y",
				overrideType: strType,
				r: func() *Resource {
					return DefaultResource("test_resource", scalarAtXY(), nil, nil)
				},
			},
			want: want{parameterType: "string"},
		},
		"DifferentPath": {
			reason: "A path without a configured override has no parameter type override.",
			args: args{
				path:         "a.b",
				overridePath: "x.y",
				overrideType: strType,
				r: func() *Resource {
					return DefaultResource("test_resource", scalarAtXY(), nil, nil)
				},
			},
			want: want{parameterType: ""},
		},
		"NoOverrides": {
			reason: "A resource with no configured overrides has no parameter type override for any path.",
			args: args{
				path: "x.y",
				r: func() *Resource {
					return DefaultResource("test_resource", scalarAtXY(), nil, nil)
				},
			},
			want: want{parameterType: ""},
		},
		"OverrideCollectionPath": {
			reason: "A scalar type override configured with a path without the wildcard segments is returned for that path, even if an intermediate path segment is a collection type.",
			args: args{
				path:         "x.y.z",
				overridePath: "x.y.z",
				overrideType: strType,
				r: func() *Resource {
					return DefaultResource("test_resource", scalarAtXYZ(), nil, nil)
				},
			},
			want: want{parameterType: "string"},
		},
		"NilOverrideIgnored": {
			reason: "A nil type override is indistinguishable from no override for the path.",
			args: args{
				path:         "x.y",
				overridePath: "x.y",
				overrideType: nil,
				r: func() *Resource {
					return DefaultResource("test_resource", scalarAtXY(), nil, nil)
				},
			},
			want: want{parameterType: ""},
		},
		"PathNotInSchema": {
			reason: "A path that does not exist in the Terraform resource schema cannot be configured with a type override.",
			args: args{
				path:         "a.b",
				overridePath: "a.b",
				overrideType: strType,
				r: func() *Resource {
					return DefaultResource("test_resource", scalarAtXY(), nil, nil)
				},
			},
			want: want{
				parameterType: "",
				err:           errors.Errorf("path a.b is not valid for the Terraform resource schema of %q", "test_resource"),
			},
		},
		"NoTerraformResourceSchema": {
			reason: "A type override cannot be configured for a resource without a Terraform resource schema to validate the path against.",
			args: args{
				path:         "x.y",
				overridePath: "x.y",
				overrideType: strType,
				r: func() *Resource {
					return DefaultResource("test_resource", nil, nil, nil)
				},
			},
			want: want{
				parameterType: "",
				err:           errors.Errorf("resource %q does not have a valid Terraform resource schema", "test_resource"),
			},
		},
		"PathNotScalar": {
			reason: "A path whose Terraform type is not scalar cannot be configured with a type override.",
			args: args{
				path:         "x.y",
				overridePath: "x.y",
				overrideType: strType,
				r: func() *Resource {
					return DefaultResource("test_resource", scalarAtXYZ(), nil, nil)
				},
			},
			want: want{
				parameterType: "",
				err:           errors.Errorf("field at path %q with Terraform type %s is not scalar, only scalar field types can be overridden", "x.y", schema.TypeList.String()),
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			r := tc.args.r()
			var gotErr error
			if tc.args.overridePath != "" {
				gotErr = r.OverrideScalarFieldType(tc.args.overridePath, tc.args.overrideType)
			}
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Errorf("%s\nOverrideScalarFieldType(%q): -want error, +got error:\n%s", tc.reason, tc.args.overridePath, diff)
			}
			got := r.FieldTypeOverride(tc.args.path).ParameterTypeOverride
			gotStr := ""
			if got != nil {
				gotStr = got.String()
			}
			if diff := cmp.Diff(tc.want.parameterType, gotStr); diff != "" {
				t.Errorf("%s\nFieldTypeOverride(%q): -want, +got:\n%s", tc.reason, tc.args.path, diff)
			}
		})
	}
}
