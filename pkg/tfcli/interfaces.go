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

package tfcli

import (
	"context"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
)

// RefreshResult represents result of a Refresh operation
type RefreshResult struct {
	UpToDate bool
	Exists   bool
	State    []byte
}

// ApplyResult represents result of an Apply operation
type ApplyResult struct {
	Completed bool
	State     []byte
}

// DestroyResult represents result of a Destroy operation
type DestroyResult struct {
	Completed bool
}

// Client is a Terraform cli client
type Client interface {
	// Refresh is a "blocking" operation refreshing terraform state and preparing
	// a RefreshResult based on that.
	Refresh(ctx context.Context, id string) (RefreshResult, error)
	// Apply is a "non-blocking" operation triggering a Terraform Apply
	Apply(ctx context.Context) (ApplyResult, error)
	// Destroy is a "non-blocking" operation triggering a Terraform Destroy
	Destroy(ctx context.Context) (DestroyResult, error)
	// Close destroys this client and underlying workspace for this client
	Close(ctx context.Context) error
}

// Builder is a Terraform Client builder
type Builder interface {
	BuildClient() (Client, error)
	WithLogger(logger logging.Logger) Builder
	WithTimeout(d time.Duration) Builder
	WithState(tfState []byte) Builder
	WithResourceType(labelType string) Builder
	WithResourceName(labelName string) Builder
	WithResourceBody(body []byte) Builder
	WithHandle(handle string) Builder
	WithProviderSource(source string) Builder
	WithProviderVersion(version string) Builder
	WithProviderConfiguration(conf []byte) Builder
}
