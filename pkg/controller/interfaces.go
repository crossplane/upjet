/*
Copyright 2021 Upbound Inc.
*/

package controller

import (
	"context"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/terraform"
)

// TODO(muvaf): It's a bit weird that the functions return the struct of a
// specific implementation of this interface. Maybe a different package for the
// returned result types?

// Workspace is the set of methods that are needed for the controller to work.
type Workspace interface {
	ApplyAsync(terraform.CallbackFn) error
	Apply(context.Context) (terraform.ApplyResult, error)
	DestroyAsync(terraform.CallbackFn) error
	Destroy(context.Context) error
	Refresh(context.Context) (terraform.RefreshResult, error)
	Import(context.Context, resource.Terraformed) (terraform.ImportResult, error)
	Plan(context.Context) (terraform.PlanResult, error)
}

// ProviderSharer shares a native provider process with the receiver.
type ProviderSharer interface {
	UseProvider(inuse terraform.InUse, attachmentConfig string)
}

// Store is where we can get access to the Terraform workspace of given resource.
type Store interface {
	Workspace(ctx context.Context, c resource.SecretClient, tr resource.Terraformed, ts terraform.Setup, cfg *config.Resource) (*terraform.Workspace, error)
}

// CallbackProvider provides functions that can be called with the result of
// async operations.
type CallbackProvider interface {
	Apply(name string) terraform.CallbackFn
	Destroy(name string) terraform.CallbackFn
}
