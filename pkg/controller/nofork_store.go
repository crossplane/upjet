// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"sync"
	"sync/atomic"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	tfsdk "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"k8s.io/apimachinery/pkg/types"

	"github.com/crossplane/upjet/pkg/resource"
	"github.com/crossplane/upjet/pkg/terraform"
)

type AsyncTracker struct {
	LastOperation *terraform.Operation
	logger        logging.Logger
	mu            *sync.Mutex
	tfID          string
	tfState       *tfsdk.InstanceState
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

func (a *AsyncTracker) GetTfState() *tfsdk.InstanceState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tfState
}

func (a *AsyncTracker) HasState() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tfState != nil && a.tfState.ID != ""
}

func (a *AsyncTracker) SetTfState(state *tfsdk.InstanceState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tfState = state
}

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

type OperationTrackerStore struct {
	store  map[types.UID]*AsyncTracker
	logger logging.Logger
	mu     *sync.Mutex
}

func NewOperationStore(l logging.Logger) *OperationTrackerStore {
	ops := &OperationTrackerStore{
		store:  map[types.UID]*AsyncTracker{},
		logger: l,
		mu:     &sync.Mutex{},
	}

	return ops
}

func (ops *OperationTrackerStore) Tracker(tr resource.Terraformed) *AsyncTracker {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	tracker, ok := ops.store[tr.GetUID()]
	if !ok {
		l := ops.logger.WithValues("trackerUID", tr.GetUID(), "resourceName", tr.GetName())
		ops.store[tr.GetUID()] = NewAsyncTracker(WithAsyncTrackerLogger(l))
		tracker = ops.store[tr.GetUID()]
	}
	return tracker
}

func (ops *OperationTrackerStore) RemoveTracker(obj xpresource.Object) error {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	delete(ops.store, obj.GetUID())
	return nil
}
