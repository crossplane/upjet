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
	CreateOrUpdate(ctx context.Context, tr resource.Terraformed) (Update, error)
	Delete(ctx context.Context, tr resource.Terraformed) (bool, error)
}
