// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package markers

// Options represents marker options that Upjet need to parse or set.
type Options struct {
	UpjetOptions
	CrossplaneOptions
	KubebuilderOptions
	ServerSideApplyOptions
}

// String returns a string representation of this Options object.
func (o Options) String() string {
	return o.UpjetOptions.String() +
		o.CrossplaneOptions.String() +
		o.KubebuilderOptions.String() +
		o.ServerSideApplyOptions.String()
}
