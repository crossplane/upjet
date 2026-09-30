// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

func TestCRDFieldPath(t *testing.T) {
	cases := map[string]struct {
		reason string
		tfPath string
		want   string
	}{
		"PlainField": {
			reason: "A Terraform attribute name is lower camel cased the way the CRD spells it.",
			tfPath: "master_password",
			want:   "masterPassword",
		},
		"Acronym": {
			reason: "An acronym is spelled the way the manifest has it.",
			tfPath: "kms_key_id",
			want:   "kmsKeyId",
		},
		"Nested": {
			reason: "Every segment is cased, not just the first.",
			tfPath: "logging_config.target_bucket",
			want:   "loggingConfig.targetBucket",
		},
		"Indexed": {
			reason: "An index belongs to the element, not to the field name, so it is left alone.",
			tfPath: "action[0].authenticate_oidc[1].client_secret",
			want:   "action[0].authenticateOidc[1].clientSecret",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, crdFieldPath(tc.tfPath)); diff != "" {
				t.Errorf("\n%s\ncrdFieldPath(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestUnresolvedChange(t *testing.T) {
	// A parameter whose Secret the caller did not supply. Neither side can be
	// shown, for different reasons, and the field is the user's because they
	// wrote the reference. Reporting it as the provider's would let a client
	// that hides provider-originated changes drop it.
	want := &diffv1alpha1.FieldChange{
		Field:   "spec.forProvider.masterPassword",
		Actual:  absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
		Planned: absentValue(diffv1alpha1.Absence_ABSENCE_UNRESOLVED),
		Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
	}
	if diff := cmp.Diff(want, unresolvedChange("master_password"), protocmp.Transform()); diff != "" {
		t.Errorf("unresolvedChange(): -want, +got:\n%s", diff)
	}
}
