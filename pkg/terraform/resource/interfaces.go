package resource

import (
	"github.com/crossplane/crossplane-runtime/pkg/resource"
)

type Observable interface {
	GetObservation() ([]byte, error)
	SetObservation(data []byte) error
}

type Parameterizable interface {
	GetParameters() (map[string]interface{}, error)
	SetParameters(map[string]interface{}) error
}

// MetadataProvider provides Terraform metadata for the Terraform managed resource
type MetadataProvider interface {
	GetTerraformResourceType() string
	GetTerraformResourceIdField() string
}

// Terraformed is a Kubernetes object representing a concrete terraform managed resource
type Terraformed interface {
	resource.Managed

	MetadataProvider
	Observable
	Parameterizable
}
