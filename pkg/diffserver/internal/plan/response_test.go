// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

func TestUnresolvedChange(t *testing.T) {
	// A parameter whose Secret the caller did not supply. The desired side
	// can never be shown, because it was never evaluated; the current side,
	// when there is one to withhold at all, is treated as sensitive by
	// association with the Secret it would otherwise come from. The field is
	// the user's because they wrote the reference, so reporting it as the
	// provider's would let a client that hides provider-originated changes
	// drop it.
	cases := map[string]struct {
		reason string
		tfPath string
		exists bool
		want   *diffv1alpha1.FieldChange
	}{
		"ExistingResource": {
			reason: "An update withholds the current value as sensitive.",
			tfPath: "master_password",
			exists: true,
			want: &diffv1alpha1.FieldChange{
				Field:   "spec.forProvider.masterPassword",
				Actual:  absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
				Planned: absentValue(diffv1alpha1.Absence_ABSENCE_UNRESOLVED),
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
			},
		},
		"ResourceBeingCreated": {
			reason: "A create has no current value to withhold at all, sensitive or otherwise - unlike an update, there is nothing here yet.",
			tfPath: "master_password",
			exists: false,
			want: &diffv1alpha1.FieldChange{
				Field:   "spec.forProvider.masterPassword",
				Planned: absentValue(diffv1alpha1.Absence_ABSENCE_UNRESOLVED),
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
			},
		},
		"EmbeddedObjectDropsTheIndex": {
			reason: "A singleton list the CRD models as an embedded object must be spelled the same way a resolved change at the same attribute is - encryption_config is configured as one in testResource - or a client correlating changes by field would see the same attribute as two different fields depending on whether its Secret resolved.",
			tfPath: "encryption_config[0].kms_key_id",
			exists: true,
			want: &diffv1alpha1.FieldChange{
				Field:   "spec.forProvider.encryptionConfig.kmsKeyId",
				Actual:  absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
				Planned: absentValue(diffv1alpha1.Absence_ABSENCE_UNRESOLVED),
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
			},
		},
		"GenuineListKeepsTheIndex": {
			reason: "An index into a list the CRD does not model as an embedded object still belongs to the element.",
			tfPath: "subnet_ids[2]",
			exists: true,
			want: &diffv1alpha1.FieldChange{
				Field:   "spec.forProvider.subnetIds[2]",
				Actual:  absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
				Planned: absentValue(diffv1alpha1.Absence_ABSENCE_UNRESOLVED),
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := unresolvedChange(tc.tfPath, testResource(), tc.exists)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("\n%s\nunresolvedChange(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestIsResolvedSecret(t *testing.T) {
	m := secretResolvedMarker
	cases := map[string]struct {
		reason string
		probed map[string]any
		path   string
		want   bool
	}{
		"KeySelector": {
			reason: "A reference to one key of a Secret records a single marked value.",
			probed: map[string]any{"password": m},
			path:   "password",
			want:   true,
		},
		"MissingSecret": {
			reason: "Resolution tolerates a Secret that is not there and records an empty value, which is not evidence that anything resolved.",
			probed: map[string]any{"password": ""},
			path:   "password",
			want:   false,
		},
		"WholeSecret": {
			reason: "A reference to a whole Secret becomes a map of its entries.",
			probed: map[string]any{"tags": map[string]any{"a": m, "b": m}},
			path:   "tags",
			want:   true,
		},
		"ListOfSelectors": {
			reason: "A list of key selectors becomes a list of values, one per selector, and every one of them was found.",
			probed: map[string]any{"keys": []any{m, m}},
			path:   "keys",
			want:   true,
		},
		"ListWithAMissingKey": {
			reason: "A selector whose key is missing records an empty value, and a parameter built from a partly read list is not one the plan can stand behind.",
			probed: map[string]any{"keys": []any{m, ""}},
			path:   "keys",
			want:   false,
		},
		"EmptyList": {
			reason: "Nothing was read.",
			probed: map[string]any{"keys": []any{}},
			path:   "keys",
			want:   false,
		},
		"Absent": {
			reason: "The path carries nothing at all.",
			probed: map[string]any{},
			path:   "password",
			want:   false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, isResolvedSecret(fieldpath.Pave(tc.probed), tc.path)); diff != "" {
				t.Errorf("\n%s\nisResolvedSecret(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestIsUnresolvedParameter(t *testing.T) {
	// The paths a resource's sensitive parameters are reported at use field
	// path syntax, while a diff spells the same attribute as a flatmap key.
	unresolved := []string{"password", "action[0].client_secret", "tags"}

	cases := map[string]struct {
		reason string
		key    string
		want   bool
	}{
		"TopLevel": {
			reason: "The simple case, where both spellings already agree.",
			key:    "password",
			want:   true,
		},
		"NestedIndex": {
			reason: "A nested reference is reported as action[0].client_secret but the diff keys it as action.0.client_secret. Without normalising, it is reported twice: once as the artefact this suppresses and once as unresolved.",
			key:    "action.0.client_secret",
			want:   true,
		},
		"WholeSecretEntry": {
			reason: "A reference to a whole Secret names the parameter its entries land under, and the diff reports each entry, so everything beneath it is unresolved too.",
			key:    "tags.mykey",
			want:   true,
		},
		"Unrelated": {
			reason: "An attribute with no secret reference is untouched.",
			key:    "description",
			want:   false,
		},
		"PrefixOfAnotherName": {
			reason: "A name that merely starts with an unresolved one is a different attribute.",
			key:    "password_policy",
			want:   false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, isUnresolvedParameter(tc.key, unresolved)); diff != "" {
				t.Errorf("\n%s\nisUnresolvedParameter(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestPlanConfig(t *testing.T) {
	at := func(v string) xpresource.Managed {
		tr := &fake.Terraformed{}
		tr.SetGroupVersionKind(schema.GroupVersionKind{Group: "example.upbound.io", Version: v, Kind: "Thing"})
		return tr
	}
	// A fresh Resource with the singleton paths a schema traversal would
	// have found - the same regardless of which version is requested, since
	// they come from the (version-invariant) Terraform schema alone.
	withPaths := func() *config.Resource {
		r := config.DefaultResource("test_resource", nil, nil, nil)
		r.Version = "v1beta2"
		r.AddSingletonListConversion("website", "website")
		return r
	}

	t.Run("EmbeddedHubVersionWithTheConversionAlreadyRegistered", func(t *testing.T) {
		// The common case: cfg.Version's own request, nothing to reconcile.
		cfg := withPaths()
		cfg.TerraformConversions = []config.TerraformConversion{
			config.NewTFSingletonConversion(),
			config.NewTFDynamicValueConversion(),
		}
		if got := planConfig(cfg, at("v1beta2")); got != cfg {
			t.Error("planConfig(): want the configuration untouched when the conversion is already correct for the requested version")
		}
	})

	t.Run("ALegacyShapedVersionDropsOnlyTheSingletonConversion", func(t *testing.T) {
		// Case B: a served version that predates the embedding.
		cfg := withPaths()
		cfg.SingletonListVersions = []string{"v1beta1"}
		cfg.TerraformConversions = []config.TerraformConversion{
			config.NewTFSingletonConversion(),
			config.NewTFDynamicValueConversion(),
		}
		got := planConfig(cfg, at("v1beta1"))
		if got == cfg {
			t.Fatal("planConfig(): want a copy, so that the shared configuration is not modified")
		}
		if diff := cmp.Diff(1, len(got.TerraformConversions)); diff != "" {
			t.Errorf("planConfig(): -want conversions, +got:\n%s", diff)
		}
		if got.TerraformConversions[0] != config.NewTFDynamicValueConversion() {
			t.Error("planConfig(): want the unrelated conversion kept")
		}
		if diff := cmp.Diff(2, len(cfg.TerraformConversions)); diff != "" {
			t.Errorf("planConfig(): the shared configuration was modified: -want, +got:\n%s", diff)
		}
	})

	t.Run("AnotherEmbeddedVersionKeepsTheConversion", func(t *testing.T) {
		// Case C: a non-hub served version that shares cfg.Version's shape,
		// not listed in SingletonListVersions. Nothing to reconcile, same as
		// the hub itself - this is the provider-gcp Bucket regression case.
		cfg := withPaths()
		cfg.TerraformConversions = []config.TerraformConversion{config.NewTFSingletonConversion()}
		if got := planConfig(cfg, at("v1beta3")); got != cfg {
			t.Error("planConfig(): want the configuration untouched for an unlisted version sharing the hub's shape")
		}
	})

	t.Run("AnEmbeddedVersionConstructsTheMissingConversion", func(t *testing.T) {
		// Case A: the reconciler itself runs on a legacy-shaped version (so
		// the provider never registered the conversion at all), but the
		// requested version is embedded. The conversion is built from the
		// paths the schema traversal already found.
		cfg := withPaths()
		cfg.Version = "v1beta1"
		cfg.SingletonListVersions = []string{"v1beta1"}
		// TerraformConversions intentionally left empty: the reconciler
		// reconciles v1beta1, which does not need the conversion.
		got := planConfig(cfg, at("v1beta2"))
		if got == cfg {
			t.Fatal("planConfig(): want a copy, so that the shared configuration is not modified")
		}
		if diff := cmp.Diff(1, len(got.TerraformConversions)); diff != "" {
			t.Fatalf("planConfig(): -want conversions, +got:\n%s", diff)
		}
		if got.TerraformConversions[0] != config.NewTFSingletonConversion() {
			t.Error("planConfig(): want the singleton conversion constructed for the embedded requested version")
		}
		if diff := cmp.Diff(0, len(cfg.TerraformConversions)); diff != "" {
			t.Errorf("planConfig(): the shared configuration was modified: -want, +got:\n%s", diff)
		}
	})

	t.Run("NoSingletonPathsIsAlwaysAsIs", func(t *testing.T) {
		// A resource with nothing to convert: constructing a conversion with
		// no paths to act on would be pointless noise, not merely harmless.
		cfg := &config.Resource{Version: "v1beta2"}
		if got := planConfig(cfg, at("v1beta1")); got != cfg {
			t.Error("planConfig(): want the configuration untouched when there are no singleton paths at all")
		}
	})
}
