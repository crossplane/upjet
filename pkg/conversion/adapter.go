package conversion

import (
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"

	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
)

// ObserveResult represents result of an observe operation
type ObserveResult struct {
	// Tells whether the observe operation is completed.
	Completed bool
	// Base64 encoded Terraform state to persist
	State string
	// Sensitive information that is available during creation/update.
	ConnectionDetails managed.ConnectionDetails
	// Is resource up to date
	UpToDate bool
	// Does resource exist
	Exists bool
	// Was the resource late initialized
	LateInitialized bool
}

// CreateResult represents result of a create operation
type CreateResult struct {
	// Tells whether the apply operation is completed.
	Completed bool
	// Terraform state to persist
	ExternalName string
	// Base64 encoded Terraform state to persist
	State string
	// Sensitive information that is available during creation/update.
	ConnectionDetails managed.ConnectionDetails
}

// UpdateResult represents result of an update operation
type UpdateResult struct {
	// Tells whether the apply operation is completed.
	Completed bool
	// Terraform state to persist
	State string
	// Sensitive information that is available during creation/update.
	ConnectionDetails managed.ConnectionDetails
}

// DeletionResult represents result of a delete operation
type DeletionResult struct {
	// Tells whether the apply operation is completed.
	Completed bool
}

// An Adapter is used to interact with terraform managed resources
type Adapter interface {
	Observe(tr resource.Terraformed) (ObserveResult, error)
	Create(tr resource.Terraformed) (CreateResult, error)
	Update(tr resource.Terraformed) (UpdateResult, error)
	Delete(tr resource.Terraformed) (DeletionResult, error)
}
