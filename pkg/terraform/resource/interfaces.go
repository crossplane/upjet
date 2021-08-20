package resource

import (
	"github.com/crossplane/crossplane-runtime/pkg/resource"
)

// TerraformStateHandler handles terraform state
type TerraformStateHandler interface {
	GetObservation() ([]byte, error)
	SetObservation(data []byte) error

	GetParameters() ([]byte, error)
	SetParameters(data []byte) error
}

// TerraformMetadataProvider provides Terraform metadata for the Terraform managed resource
type TerraformMetadataProvider interface {
	GetTerraformResourceType() string
	GetTerraformResourceIdField() string
}

// Terraformed is a Kubernetes object representing a concrete terraform managed resource
type Terraformed interface {
	resource.Managed

	TerraformMetadataProvider
	TerraformStateHandler
}
