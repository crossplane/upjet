// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

// Mode denotes the mode of the runtime Terraform conversion, e.g.,
// conversion from Crossplane parameters to Terraform arguments, or
// conversion from Terraform state to Crossplane state.
type Mode int

const (
	ToTerraform Mode = iota
	FromTerraform
)

type TerraformConversion interface {
	Convert(params map[string]any, cfg *Resource, mode Mode) (map[string]any, error)
}

type TerraformConversions []TerraformConversion

func (tc TerraformConversions) Convert(params map[string]any, cfg *Resource, mode Mode) (map[string]any, error) {
	var err error
	for _, c := range tc {
		params, err = c.Convert(params, cfg, mode)
		if err != nil {
			return nil, err
		}
	}
	return params, nil
}
