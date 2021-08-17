package conversion

import (
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"

	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
)

// ObserveResult represents result of an observe operation
type ObserveResult struct {
	Completed         bool
	ConnectionDetails managed.ConnectionDetails
	UpToDate          bool
	Exists            bool
	LateInitialized   bool
}

// CreateResult represents result of a create operation
type CreateResult struct {
	Completed         bool
	ConnectionDetails managed.ConnectionDetails
}

// UpdateResult represents result of an update operation
type UpdateResult struct {
	Completed         bool
	ConnectionDetails managed.ConnectionDetails
}

// DeletionResult represents result of a delete operation
type DeletionResult struct {
	Completed bool
}

// An Adapter is used to interact with terraform managed resources
type Adapter interface {
	Observe(tr resource.Terraformed) (ObserveResult, error)
	Create(tr resource.Terraformed) (CreateResult, error)
	Update(tr resource.Terraformed) (UpdateResult, error)
	Delete(tr resource.Terraformed) (bool, error)
}
