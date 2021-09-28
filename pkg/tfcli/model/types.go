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

package model

import (
	"context"

	"github.com/crossplane-contrib/terrajet/pkg/json"
)

// RefreshResult holds information about the state of a Cloud resource.
type RefreshResult struct {
	// UpToDate is true if the remote Cloud resource configuration
	// matches the Terraform configuration.
	UpToDate bool
	// Exists is true if the remote Cloud resource with the configured
	// id exists.
	Exists bool
	// State holds the Terraform state of the resource.
	// Because Client.Refresh is a synchronous operation,
	// it's non-nil if Client.Refresh does not return an error.
	State *json.StateV4
}

// ApplyResult represents the status of an asynchronous Client.Apply call
type ApplyResult struct {
	// Completed true if the async Client.Apply operation has completed
	Completed bool
	// State the up-to-date Terraform state if Completed is true
	State *json.StateV4
}

// DestroyResult represents the status of an asynchronous Client.Destroy call
type DestroyResult struct {
	// Completed true if the async Client.Destroy operation has completed
	Completed bool
}

// Client represents a Terraform client capable of running
// Refresh, Apply, Destroy pipelines.
type Client interface {
	Refresh(ctx context.Context) (RefreshResult, error)
	Apply(ctx context.Context) (ApplyResult, error)
	Destroy(ctx context.Context) (DestroyResult, error)
	Close(ctx context.Context) error
	DiscardOperation(ctx context.Context) error
}

// OperationType is an operation type for Terraform CLI
type OperationType int

func (ot OperationType) String() string {
	return []string{"init", "refresh", "apply", "destroy"}[ot]
}

const (
	// OperationInit represents an Init operation
	OperationInit OperationType = iota
	// OperationRefresh represents a Refresh operation
	OperationRefresh
	// OperationApply represents an Apply operation
	OperationApply
	// OperationDestroy represents a Destroy operation
	OperationDestroy
)
