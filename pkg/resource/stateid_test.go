// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package resource

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNormalizeStateID(t *testing.T) {
	cases := map[string]struct {
		reason  string
		state   map[string]any
		want    map[string]any
		wantErr bool
	}{
		"NumericID": {
			reason: "A plugin-framework numeric id becomes its string form.",
			state:  map[string]any{"id": float64(577654), "name": "snoutzone-dev"},
			want:   map[string]any{"id": "577654", "name": "snoutzone-dev"},
		},
		"StringIDUntouched": {
			reason: "The plugin-SDK case must not change.",
			state:  map[string]any{"id": "vpc-2213das"},
			want:   map[string]any{"id": "vpc-2213das"},
		},
		"NoID": {
			reason: "An ID-less resource is not an error.",
			state:  map[string]any{"name": "x"},
			want:   map[string]any{"name": "x"},
		},
		"Zero": {
			reason: "Zero is a representable integer and is rendered plainly, not as 0e+00.",
			state:  map[string]any{"id": float64(0)},
			want:   map[string]any{"id": "0"},
		},
		"LargeIDRejected": {
			reason:  "Past 2^53 a float64 cannot round-trip, so converting would address the wrong resource.",
			state:   map[string]any{"id": float64(1 << 54)},
			wantErr: true,
		},
		"FractionalRejected": {
			reason:  "A fractional id is not an identifier we can render faithfully.",
			state:   map[string]any{"id": 1.5},
			wantErr: true,
		},
		"OtherTypeLeftAlone": {
			reason: "Anything else is left for the caller to report with its own context.",
			state:  map[string]any{"id": true},
			want:   map[string]any{"id": true},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := NormalizeStateID(tc.state)
			if tc.wantErr {
				if err == nil {
					t.Errorf("NormalizeStateID(...): want error, got none\n%s", tc.reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeStateID(...): unexpected error: %v\n%s", err, tc.reason)
			}
			if diff := cmp.Diff(tc.want, tc.state); diff != "" {
				t.Errorf("NormalizeStateID(...): -want, +got:\n%s\n%s", diff, tc.reason)
			}
		})
	}
}
