// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetDescription(t *testing.T) {
	cases := map[string]struct {
		reason string
		arg    string
		want   string
	}{
		"PlainOptional": {
			reason: "A simple '(Optional)' marker should be stripped entirely.",
			arg:    "timezone - (Optional) Time zone of the DB instance.",
			want:   "Time zone of the DB instance.",
		},
		"PlainRequired": {
			reason: "A simple '(Required)' marker should be stripped entirely.",
			arg:    "name - (Required) Name of the resource.",
			want:   "Name of the resource.",
		},
		"ComplexRequirement": {
			reason: "A parenthesized requirement that is more specific than " +
				"'Optional'/'Required' must be preserved, since it conveys " +
				"information not otherwise available to the user.",
			arg: "username - (Required unless a snapshot_identifier or replicate_source_db is provided) " +
				"Username for the master DB user. Cannot be specified for a replica.",
			want: "(Required unless a snapshot_identifier or replicate_source_db is provided) " +
				"Username for the master DB user. Cannot be specified for a replica.",
		},
		"CaseInsensitiveOptional": {
			reason: "The '(optional)' marker check must be case-insensitive.",
			arg:    "field - (optional) Some description.",
			want:   "Some description.",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := getDescription(tc.arg)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\ngetDescription(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
