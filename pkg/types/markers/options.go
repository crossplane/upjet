// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package markers

import "github.com/crossplane/upjet/v2/pkg/types/markers/kubebuilder"

// Options represents marker options that Upjet need to parse or set.
type Options struct {
	UpjetOptions
	CrossplaneOptions
	KubebuilderOptions kubebuilder.Options
	ServerSideApplyOptions
}

// String returns a string representation of this Options object.
func (o Options) String() string {
	return o.UpjetOptions.String() +
		o.CrossplaneOptions.String() +
		o.KubebuilderOptions.String() +
		o.ServerSideApplyOptions.String()
}
