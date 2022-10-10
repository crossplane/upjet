/*
Copyright 2021 Upbound Inc.
*/

package templates

import _ "embed" // nolint:golint

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
