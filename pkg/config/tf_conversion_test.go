// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDynamicValueConversionFromTerraformUnwrapsEnvelope(t *testing.T) {
	r := &Resource{dynamicAttributeConversionPaths: []string{"value"}}
	params := map[string]any{
		"value": map[string]any{"type": "string", "value": "bare"},
	}
	got, err := NewTFDynamicValueConversion().Convert(params, r, FromTerraform)
	if err != nil {
		t.Fatalf("Convert(FromTerraform): %s", err)
	}
	if diff := cmp.Diff("bare", got["value"]); diff != "" {
		t.Errorf("DynamicPseudoType envelope was not unwrapped: -want, +got:\n%s", diff)
	}
}
