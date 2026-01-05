// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"sync"
	"sync/atomic"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tfsdk "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"k8s.io/apimachinery/pkg/types"

	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
)

type stateMark int

const (
	defaultState stateMark = iota
	reconstructedState
)

// AsyncTracker holds information for a managed resource to track the
// async Terraform operations and the
// Terraform state (TF SDKv2 or TF Plugin Framework) of the external resource
//
// The typical usage is to instantiate an AsyncTracker for a managed resource,
// and store in a global OperationTrackerStore, to carry information between
// reconciliation scopes.
//
// When an asynchronous Terraform operation is started for the resource
// in a reconciliation (e.g. with a goroutine), consumers can mark an operation start
// on the LastOperation field, then access the operation status in the
// forthcoming reconciliation cycles, and act upon
// (e.g. hold further actions if there is an ongoing operation, mark the end
// when underlying Terraform operation is completed, save the resulting
// terraform state etc.)
//
// When utilized without the LastOperation usage, it can act as a Terraform
// state cache for synchronous reconciliations
type AsyncTracker struct {
	// LastOperation holds information about the most recent operation.
	// Consumers are responsible for managing the last operation by starting,
	// ending and flushing it when done with processing the results.
	// Designed to allow only one ongoing operation at a given time.
	LastOperation *terraform.Operation
	logger        logging.Logger
	mu            *sync.Mutex
	// TF Plugin SDKv2 instance state for TF Plugin SDKv2-based resources
	tfState *tfsdk.InstanceState
	// TF Plugin Framework instance state for TF Plugin Framework-based resources
	fwState *tfprotov6.DynamicValue
	// stateMark holds meta-information about the state
	stateMark stateMark
	// lifecycle of certain external resources are bound to a parent resource's
	// lifecycle, and they cannot be deleted without actually deleting
	// the owning external resource (e.g.,  a database resource as the parent
	// resource and a database configuration resource whose lifecycle is bound
	// to it. For such resources, Terraform still removes the state for them
	// after a successful delete call either by resetting to some defaults in
	// the parent resource, or by a noop. We logically delete such resources as
	// deleted after a successful delete call so that the next observe can
	// tell the managed reconciler that the resource no longer "exists".
	isDeleted atomic.Bool
}

type AsyncTrackerOption func(manager *AsyncTracker)

// WithAsyncTrackerLogger sets the logger of AsyncTracker.
func WithAsyncTrackerLogger(l logging.Logger) AsyncTrackerOption {
	return func(w *AsyncTracker) {
		w.logger = l
	}
}

// NewAsyncTracker initializes an AsyncTracker with given options
func NewAsyncTracker(opts ...AsyncTrackerOption) *AsyncTracker {
	w := &AsyncTracker{
		LastOperation: &terraform.Operation{},
		logger:        logging.NewNopLogger(),
		mu:            &sync.Mutex{},
	}
	for _, f := range opts {
		f(w)
	}
	return w
}

// GetTfState returns the stored Terraform Plugin SDKv2 InstanceState for
// SDKv2 Terraform resources
// MUST be only used for SDKv2 resources.
func (a *AsyncTracker) GetTfState() *tfsdk.InstanceState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tfState
}

// HasState returns whether the AsyncTracker has a SDKv2 state stored.
// MUST be only used for SDKv2 resources.
func (a *AsyncTracker) HasState() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tfState != nil && a.tfState.ID != ""
}

// SetTfState stores the given SDKv2 Terraform InstanceState into
// the AsyncTracker
// MUST be only used for SDKv2 resources.
func (a *AsyncTracker) SetTfState(state *tfsdk.InstanceState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tfState = state
	a.stateMark = defaultState
}

// SetReconstructedTfState stores the given SDKv2 Terraform InstanceState into
// the AsyncTracker and marks the state as reconstructed.
// MUST be only used for SDKv2 resources.
func (a *AsyncTracker) SetReconstructedTfState(state *tfsdk.InstanceState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tfState = state
	a.stateMark = reconstructedState
}

// ResetReconstructedTfState clears the TF Plugin SDKv2 InstanceState
// if it is a reconstructed state. No-op otherwise.
// MUST be only used for SDKv2 resources.
func (a *AsyncTracker) ResetReconstructedTfState() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stateMark == reconstructedState {
		a.tfState = nil
		a.stateMark = defaultState
	}
}

// GetTfID returns the Terraform ID of the external resource currently
// stored in this AsyncTracker's SDKv2 instance state.
// MUST be only used for SDKv2 resources.
func (a *AsyncTracker) GetTfID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tfState == nil {
		return ""
	}
	return a.tfState.ID
}

// IsDeleted returns whether the associated external resource
// has logically been deleted.
func (a *AsyncTracker) IsDeleted() bool {
	return a.isDeleted.Load()
}

// SetDeleted sets the logical deletion status of
// the associated external resource.
func (a *AsyncTracker) SetDeleted(deleted bool) {
	a.isDeleted.Store(deleted)
}

// GetFrameworkTFState returns the stored Terraform Plugin Framework external
// resource state in this AsyncTracker as *tfprotov6.DynamicValue
// MUST be used only for Terraform Plugin Framework resources
func (a *AsyncTracker) GetFrameworkTFState() *tfprotov6.DynamicValue {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fwState
}

// HasFrameworkTFState returns whether this AsyncTracker has a
// Terraform Plugin Framework state stored.
// MUST be used only for Terraform Plugin Framework resources
func (a *AsyncTracker) HasFrameworkTFState() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fwState != nil
}

// SetFrameworkTFState stores the given *tfprotov6.DynamicValue Terraform Plugin Framework external
// resource state into this AsyncTracker's fwstate
// MUST be used only for Terraform Plugin Framework resources
func (a *AsyncTracker) SetFrameworkTFState(state *tfprotov6.DynamicValue) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fwState = state
	a.stateMark = defaultState
}

// SetReconstructedFrameworkTFState stores the given *tfprotov6.DynamicValue
// Terraform Plugin Framework external resource reconstructed state into this
// AsyncTracker's fwstate and marks the state as reconstructed.
// MUST be used only for Terraform Plugin Framework resources
func (a *AsyncTracker) SetReconstructedFrameworkTFState(state *tfprotov6.DynamicValue) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fwState = state
	a.stateMark = reconstructedState
}

// ResetReconstructedFrameworkTFState clears the TF Plugin Framework resource
// state if it is a reconstructed state. No-op otherwise.
func (a *AsyncTracker) ResetReconstructedFrameworkTFState() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stateMark == reconstructedState {
		a.fwState = nil
		a.stateMark = defaultState
	}
}

// OperationTrackerStore stores the AsyncTracker instances associated with the
// managed resource instance.
type OperationTrackerStore struct {
	store  map[types.UID]*AsyncTracker
	logger logging.Logger
	mu     *sync.Mutex
}

// NewOperationStore returns a new OperationTrackerStore instance
func NewOperationStore(l logging.Logger) *OperationTrackerStore {
	ops := &OperationTrackerStore{
		store:  map[types.UID]*AsyncTracker{},
		logger: l,
		mu:     &sync.Mutex{},
	}

	return ops
}

// Tracker returns the associated *AsyncTracker stored in this
// OperationTrackerStore for the given managed resource.
// If there is no tracker stored previously, a new AsyncTracker is created and
// stored for the specified managed resource. Subsequent calls with the same managed
// resource will return the previously instantiated and stored AsyncTracker
// for that managed resource
func (ops *OperationTrackerStore) Tracker(tr resource.Terraformed) *AsyncTracker {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	tracker, ok := ops.store[tr.GetUID()]
	if !ok {
		l := ops.logger.WithValues("trackerUID", tr.GetUID(), "resourceName", tr.GetName(), "resourceNamespace", tr.GetNamespace(), "gvk", tr.GetObjectKind().GroupVersionKind().String())
		ops.store[tr.GetUID()] = NewAsyncTracker(WithAsyncTrackerLogger(l))
		tracker = ops.store[tr.GetUID()]
	}
	return tracker
}

// RemoveTracker will remove the stored AsyncTracker of the given managed
// resource from this OperationTrackerStore.
func (ops *OperationTrackerStore) RemoveTracker(obj xpresource.Object) error {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	delete(ops.store, obj.GetUID())
	return nil
}
