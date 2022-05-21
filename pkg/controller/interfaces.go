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
	Plan(context.Context) (terraform.PlanResult, error)
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
