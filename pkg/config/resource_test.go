// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
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
