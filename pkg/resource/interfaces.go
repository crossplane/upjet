// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package resource

import (
	"github.com/crossplane/crossplane-runtime/pkg/resource"
)

// Observable structs can get and set observations in the form of Terraform JSON.
type Observable interface {
	GetObservation() (map[string]any, error)
	SetObservation(map[string]any) error
	GetID() string
}

// Parameterizable structs can get and set parameters of the managed resource
// using map form of Terraform JSON.
type Parameterizable interface {
	GetParameters() (map[string]any, error)
	SetParameters(map[string]any) error
	GetInitParameters() (map[string]any, error)
}

// MetadataProvider provides Terraform metadata for the Terraform managed
// resource.
type MetadataProvider interface {
	GetTerraformResourceType() string
	GetTerraformSchemaVersion() int
	GetConnectionDetailsMapping() map[string]string
}

// LateInitializer late-initializes the managed resource from observed Terraform
// state.
type LateInitializer interface {
	// LateInitialize this Terraformed resource using its observed tfState.
	// returns True if the there are any spec changes for the resource.
	LateInitialize(attrs []byte) (bool, error)
}

// Terraformed is a Kubernetes object representing a concrete terraform managed
// resource.
type Terraformed interface {
	resource.Managed

	MetadataProvider
	Observable
	Parameterizable
	LateInitializer
}
