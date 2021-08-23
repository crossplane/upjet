/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package templates

import _ "embed" // nolint:golint

// CRDTypesTemplate is populated with CRD and type information.
//go:embed crd_types.go.tmpl
var CRDTypesTemplate string

// GroupVersionInfoTemplate is populated with group and version information.
//go:embed groupversion_info.go.tmpl
var GroupVersionInfoTemplate string

// TerraformedTemplate is populated with conversion methods implementing
// Terraformed interface on CRD structs.
//go:embed terraformed.go.tmpl
var TerraformedTemplate string

// ControllerTemplate is populated with controller setup functions.
//go:embed controller.go.tmpl
var ControllerTemplate string
