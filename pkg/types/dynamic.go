// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"go/token"
	"go/types"
)

const (
	// PackagePathK8sApiExtensions is the go package path for the
	// K8s apiextensions/v1 package
	PackagePathK8sApiExtensions = "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var typeK8sAPIExtensionsJson types.Type = types.NewNamed(
	types.NewTypeName(token.NoPos, types.NewPackage(PackagePathK8sApiExtensions, "kapiextensions"), "JSON", nil),
	types.NewStruct(nil, nil),
	nil,
)
