// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"math/big"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestTFValueToGoValueDynamicPseudoType(t *testing.T) {
	cases := map[string]struct {
		input tftypes.Value
		want  any
	}{
		"Null": {
			input: tftypes.NewValue(tftypes.DynamicPseudoType, nil),
			want:  nil,
		},
		"String": {
			input: tftypes.NewValue(tftypes.DynamicPseudoType, "on"),
			want:  "on",
		},
		"Bool": {
			input: tftypes.NewValue(tftypes.DynamicPseudoType, true),
			want:  true,
		},
		"Integer": {
			input: tftypes.NewValue(tftypes.DynamicPseudoType, new(big.Float).SetInt64(42)),
			want:  int64(42),
		},
		"Float": {
			input: tftypes.NewValue(tftypes.DynamicPseudoType, big.NewFloat(1.5)),
			want:  1.5,
		},
		"Object": {
			input: tftypes.NewValue(tftypes.DynamicPseudoType, map[string]tftypes.Value{
				"mode": tftypes.NewValue(tftypes.String, "strict"),
				"ttl":  tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(300)),
			}),
			want: map[string]any{"mode": "strict", "ttl": int64(300)},
		},
		"List": {
			input: tftypes.NewValue(tftypes.DynamicPseudoType, []tftypes.Value{
				tftypes.NewValue(tftypes.String, "a"),
				tftypes.NewValue(tftypes.DynamicPseudoType, "nested"),
			}),
			want: []any{"a", "nested"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tfValueToGoValue(tc.input)
			if err != nil {
				t.Fatalf("tfValueToGoValue(...): %s", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("tfValueToGoValue(...): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestTFValueToGoValueDynamicUnknown(t *testing.T) {
	if _, err := tfValueToGoValue(tftypes.NewValue(tftypes.DynamicPseudoType, tftypes.UnknownValue)); err == nil {
		t.Error("tfValueToGoValue(unknown dynamic): expected error, got nil")
	}
}
