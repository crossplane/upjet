package conversion

import (
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"

	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
)

// Observation represents result of an observe operation
type Observation struct {
	Completed         bool
	ConnectionDetails managed.ConnectionDetails
	UpToDate          bool
	Exists            bool
	LateInitialized   bool
}

// Creation represents result of a create operation
type Creation struct {
	Completed         bool
	ConnectionDetails managed.ConnectionDetails
}

// Update represents result of an update operation
type Update struct {
	Completed         bool
	ConnectionDetails managed.ConnectionDetails
}

// An Adapter is used to interact with terraform managed resources
type Adapter interface {
	Observe(tr resource.Terraformed) (Observation, error)
	Create(tr resource.Terraformed) (Creation, error)
	Update(tr resource.Terraformed) (Update, error)
	Delete(tr resource.Terraformed) (bool, error)
}
