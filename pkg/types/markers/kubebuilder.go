// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package markers

import "fmt"

// KubebuilderOptions represents the kubebuilder options that upjet would
// need to control
type KubebuilderOptions struct {
	Required *bool
	Minimum  *int
	Maximum  *int
	Default  *string
}

func (o KubebuilderOptions) String() string {
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
