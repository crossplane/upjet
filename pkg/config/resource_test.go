// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"go/types"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kind     = "ACoolService"
	name     = "example-service"
	provider = "ACoolProvider"
)

// taggedManaged is a managed resource with a spec.forProvider.tags field, i.e.
// what the Tagger initializer is actually used with. fake.Managed alone has no
// such field, so unmarshalling the tags into it would be a no-op.
type taggedManaged struct {
	metav1.TypeMeta
	fake.Managed
	fake.TypedProviderConfigReferencer

	Spec taggedSpec `json:"spec"`
}

type taggedSpec struct {
	ForProvider taggedParameters `json:"forProvider"`
}

type taggedParameters struct {
	Tags map[string]*string `json:"tags,omitempty"`
}

// GetObjectKind disambiguates between the embedded TypeMeta and fake.Managed,
// and makes the resource report a real kind so that the external tags have
// realistic values.
func (t *taggedManaged) GetObjectKind() k8sschema.ObjectKind { return &t.TypeMeta }

// DeepCopyObject is what the fix relies on, so it has to actually deep copy the
// tags map, just like the generated DeepCopyObject of a real managed resource.
func (t *taggedManaged) DeepCopyObject() runtime.Object {
	out := *t
	if t.Spec.ForProvider.Tags != nil {
		out.Spec.ForProvider.Tags = make(map[string]*string, len(t.Spec.ForProvider.Tags))
		for k, v := range t.Spec.ForProvider.Tags {
			if v == nil {
				out.Spec.ForProvider.Tags[k] = nil
				continue
			}
			out.Spec.ForProvider.Tags[k] = ptr.To(*v)
		}
	}
	return &out
}

func TestTaggerInitialize(t *testing.T) {
	errBoom := errors.New("boom")

	// The tags the Tagger will want to see in spec.forProvider.tags, given the
	// kind, name and provider config reference set by newTagged below.
	externalTags := func() map[string]*string {
		return map[string]*string{
			xpresource.ExternalResourceTagKeyKind:     ptr.To("acoolservice"),
			xpresource.ExternalResourceTagKeyName:     ptr.To(name),
			xpresource.ExternalResourceTagKeyProvider: ptr.To(provider),
		}
	}
	withTags := func(extra map[string]*string) map[string]*string {
		tags := externalTags()
		for k, v := range extra {
			tags[k] = v
		}
		return tags
	}
	newTagged := func(tags map[string]*string, policies ...xpv2.ManagementAction) *taggedManaged {
		mg := &taggedManaged{}
		mg.TypeMeta = metav1.TypeMeta{Kind: kind, APIVersion: "v1beta1"}
		mg.SetName(name)
		mg.SetProviderConfigReference(&xpv2.ProviderConfigReference{Name: provider})
		mg.SetManagementPolicies(policies)
		mg.Spec.ForProvider.Tags = tags
		return mg
	}

	type args struct {
		mg        *taggedManaged
		updateErr error
	}
	type want struct {
		err error
		// updates is the number of calls the Tagger is expected to make to
		// kube.Update.
		updates int
		// tags, if set, is the expected spec.forProvider.tags after Initialize.
		tags map[string]*string
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"FirstReconcileTagsAbsent": {
			reason: "The external tags are missing, so they should be set and persisted.",
			args:   args{mg: newTagged(nil)},
			want: want{
				updates: 1,
				tags:    externalTags(),
			},
		},
		"FirstReconcileUpdateFailed": {
			reason: "An error from the API server should be returned to the caller.",
			args:   args{mg: newTagged(nil), updateErr: errBoom},
			want: want{
				err:     errBoom,
				updates: 1,
				tags:    externalTags(),
			},
		},
		"SteadyStateTagsAlreadySet": {
			reason: "The external tags are already in the spec, so there is nothing to update.",
			args:   args{mg: newTagged(externalTags())},
			want: want{
				updates: 0,
				tags:    externalTags(),
			},
		},
		"SteadyStateWithUserTags": {
			reason: "Tags the user set themselves must not trigger an update, and must survive it.",
			args:   args{mg: newTagged(withTags(map[string]*string{"owner": ptr.To("team-a")}))},
			want: want{
				updates: 0,
				tags:    withTags(map[string]*string{"owner": ptr.To("team-a")}),
			},
		},
		"ExternalTagChanged": {
			reason: "An external tag with a stale value should be corrected and persisted.",
			args: args{mg: newTagged(withTags(map[string]*string{
				xpresource.ExternalResourceTagKeyName: ptr.To("some-old-name"),
			}))},
			want: want{
				updates: 1,
				tags:    externalTags(),
			},
		},
		"ObserveOnly": {
			reason: "Observe-only resources should not have their spec touched at all.",
			args:   args{mg: newTagged(nil, xpv2.ManagementActionObserve)},
			want: want{
				updates: 0,
				tags:    nil,
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			updates := 0
			kube := &test.MockClient{
				MockUpdate: func(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
					updates++
					return tc.args.updateErr
				},
			}

			tagger := NewTagger(kube, "tags")
			gotErr := tagger.Initialize(context.TODO(), tc.args.mg)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Errorf("Initialize(...): %s: -want error, +got error: %s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.updates, updates); diff != "" {
				t.Errorf("Initialize(...): %s: -want update calls, +got update calls: %s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.tags, tc.args.mg.Spec.ForProvider.Tags); diff != "" {
				t.Errorf("Initialize(...): %s: -want tags, +got tags: %s", tc.reason, diff)
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
