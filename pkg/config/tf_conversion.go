// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/config/conversion"
)

// Mode denotes the mode of the runtime Terraform conversion, e.g.,
// conversion from Crossplane parameters to Terraform arguments, or
// conversion from Terraform state to Crossplane state.
type Mode int

const (
	ToTerraform Mode = iota
	FromTerraform
)

// String returns a string representation of the conversion mode.
func (m Mode) String() string {
	switch m {
	case ToTerraform:
		return "toTerraform"
	case FromTerraform:
		return "fromTerraform"
	default:
		return "unknown"
	}
}

type TerraformConversion interface {
	Convert(params map[string]any, r *Resource, mode Mode) (map[string]any, error)
}

// ApplyTFConversions applies the configured Terraform conversions on the
// specified params map in the given mode, i.e., from Crossplane layer to the
// Terraform layer or vice versa.
func (r *Resource) ApplyTFConversions(params map[string]any, mode Mode) (map[string]any, error) {
	var err error
	for _, c := range r.TerraformConversions {
		params, err = c.Convert(params, r, mode)
		if err != nil {
			return nil, err
		}
	}
	return params, nil
}

type singletonListConversion struct{}

// NewTFSingletonConversion initializes a new TerraformConversion to convert
// between singleton lists and embedded objects in the exchanged data
// at runtime between the Crossplane & Terraform layers.
func NewTFSingletonConversion() TerraformConversion {
	return singletonListConversion{}
}

func (s singletonListConversion) Convert(params map[string]any, r *Resource, mode Mode) (map[string]any, error) {
	var err error
	var m map[string]any
	switch mode {
	case FromTerraform:
		m, err = conversion.Convert(params, r.TFListConversionPaths(), conversion.ToEmbeddedObject, nil)
	case ToTerraform:
		m, err = conversion.Convert(params, r.TFListConversionPaths(), conversion.ToSingletonList, nil)
	}
	return m, errors.Wrapf(err, "failed to convert between Crossplane and Terraform layers in mode %q", mode)
}
