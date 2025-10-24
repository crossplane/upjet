// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package kubebuilder

import (
	"fmt"

	"k8s.io/utils/ptr"
)

// Options controls the kubebuilder markers that upjet will generate.
type Options struct {
	// Required generates the
	// +kubebuilder:validation:Optional
	// or
	// +kubebuilder:validation:Required
	// marker, when Required is set to false or true, respectively.
	Required *bool
	// Minimum generates the
	// +kubebuilder:validation:Minimum=<val>
	// marker.
	Minimum *int
	// Maximum generates the
	// +kubebuilder:validation:Maximum=<val>
	// marker.
	Maximum *int
	// Default generates the
	// +kubebuilder:default:=<val>
	// marker. Please note that you will need to include the quotes when setting
	// Default as needed, e.g., `"10"`.
	Default *string
}

func (o *Options) setFrom(opt *Options) {
	if opt.Required != nil {
		o.Required = ptr.To(*opt.Required)
	}
	if opt.Minimum != nil {
		o.Minimum = ptr.To(*opt.Minimum)
	}
	if opt.Maximum != nil {
		o.Maximum = ptr.To(*opt.Maximum)
	}
	if opt.Default != nil {
		o.Default = ptr.To(*opt.Default)
	}
}

// OverrideFrom returns a new Options of o with its attributes overridden from
// opt. If opt is nil, it returns a deep copy of o.
// Only non-nil fields from opt are used to override the values of o.
func (o *Options) OverrideFrom(opt *Options) Options {
	result := Options{}
	result.setFrom(o)

	if opt == nil {
		return result
	}

	result.setFrom(opt)
	return result
}

// String returns a string representation of its receiver.
func (o *Options) String() string {
	m := ""

	if o.Required != nil {
		if *o.Required {
			m += "+kubebuilder:validation:Required\n"
		} else {
			m += "+kubebuilder:validation:Optional\n"
		}
	}
	if o.Minimum != nil {
		m += fmt.Sprintf("+kubebuilder:validation:Minimum=%d\n", *o.Minimum)
	}
	if o.Maximum != nil {
		m += fmt.Sprintf("+kubebuilder:validation:Maximum=%d\n", *o.Maximum)
	}
	if o.Default != nil {
		m += fmt.Sprintf("+kubebuilder:default:=%s\n", *o.Default)
	}

	return m
}
