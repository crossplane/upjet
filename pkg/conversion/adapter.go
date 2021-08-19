package conversion

import (
	"context"

	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"

	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
)

// Observation represents result of an observe operation
type Observation struct {
	ConnectionDetails managed.ConnectionDetails
	UpToDate          bool
	Exists            bool
	LateInitialized   bool
}

// Update represents result of an update operation
type Update struct {
	Completed         bool
	ConnectionDetails managed.ConnectionDetails
}

// An Adapter is used to interact with terraform managed resources
type Adapter interface {
	Observe(ctx context.Context, tr resource.Terraformed) (Observation, error)
	Update(ctx context.Context, tr resource.Terraformed) (Update, error)
	Delete(ctx context.Context, tr resource.Terraformed) (bool, error)
}
