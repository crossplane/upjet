// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package templates

import _ "embed" //nolint:golint

// CRDTypesTemplate is populated with CRD and type information.
//
//go:embed crd_types.go.tmpl
var CRDTypesTemplate string

// GroupVersionInfoTemplate is populated with group and version information.
//
//go:embed groupversion_info.go.tmpl
var GroupVersionInfoTemplate string

// TerraformedTemplate is populated with conversion methods implementing
// Terraformed interface on CRD structs.
//
//go:embed terraformed.go.tmpl
var TerraformedTemplate string

// ControllerTemplate is populated with controller setup functions.
//
//go:embed controller.go.tmpl
var ControllerTemplate string

// RegisterTemplate is populated with scheme registration calls.
//
//go:embed register.go.tmpl
var RegisterTemplate string

// SetupTemplate is populated with controller setup calls.
//
//go:embed setup.go.tmpl
var SetupTemplate string

// ConversionHubTemplate is populated with the CRD API versions
// conversion.Hub implementation template string.
//
//go:embed conversion_hub.go.tmpl
var ConversionHubTemplate string

// ConversionSpokeTemplate is populated with the CRD API versions
// conversion.Convertible implementation template string.
//
//go:embed conversion_spoke.go.tmpl
var ConversionSpokeTemplate string
