// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

// templateOrDefault returns the provider-supplied template override when it is
// non-empty; otherwise it falls back to the built-in default template. The code
// generators use it to select between a template configured on the
// config.Provider (via config.WithControllerTemplate,
// config.WithSetupAggregatorTemplate or config.WithTerraformedTemplate) and the
// corresponding default template shipped with upjet.
func templateOrDefault(override, defaultTemplate string) string {
	if override != "" {
		return override
	}
	return defaultTemplate
}
