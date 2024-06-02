// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	kind     = "ACoolService"
	name     = "example-service"
	provider = "ACoolProvider"
)

func TestTagger_Initialize(t *testing.T) {
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

func TestReferences_AddReference(t *testing.T) {
	t.Run("Adding a reference", func(t *testing.T) {
		r := &Resource{References: References{}}
		err := r.References.AddReference("forwarding_rule.certificate_name", Reference{
			TerraformName: "digitalocean_certificate",
		})
		if err != nil {
			t.Fatalf("AddReference() got error: %v", err)
		}
		if len(r.References) != 1 {
			t.Fatalf("AddReference() got error: %v", err)
		}
	})
	t.Run("Adding twice a reference for a given field", func(t *testing.T) {
		r := &Resource{References: References{}}
		err := r.References.AddReference("forwarding_rule.certificate_name", Reference{
			TerraformName: "digitalocean_certificate",
		})
		if err != nil {
			t.Fatalf("AddReference() got error: %v", err)
		}

		err = r.References.AddReference("forwarding_rule.certificate_name", Reference{
			TerraformName: "digitalocean_certificate",
		})
		if !errors.Is(err, ErrReferenceAlreadyExists) {
			t.Fatalf("AddReference() should have returned an error")
		}
	})
}
