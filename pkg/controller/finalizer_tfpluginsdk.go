// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/pkg/errors"
)

const (
	errRemoveTracker = "cannot remove tracker from the store"
)

// TrackerCleaner is the interface for the common finalizer of both Terraform
// plugin SDK and framework managed resources.
type TrackerCleaner interface {
	RemoveTracker(obj xpresource.Object) error
}

// NewOperationTrackerFinalizer returns a new OperationTrackerFinalizer.
func NewOperationTrackerFinalizer(tc TrackerCleaner, af xpresource.Finalizer) *OperationTrackerFinalizer {
	return &OperationTrackerFinalizer{
		Finalizer:      af,
		OperationStore: tc,
	}
}

// OperationTrackerFinalizer removes the operation tracker from the workspace store and only
// then calls RemoveFinalizer of the underlying Finalizer.
type OperationTrackerFinalizer struct {
	xpresource.Finalizer
	OperationStore TrackerCleaner
}

// AddFinalizer to the supplied Managed resource.
func (nf *OperationTrackerFinalizer) AddFinalizer(ctx context.Context, obj xpresource.Object) error {
	return nf.Finalizer.AddFinalizer(ctx, obj)
}

// RemoveFinalizer removes the workspace from workspace store before removing
// the finalizer.
func (nf *OperationTrackerFinalizer) RemoveFinalizer(ctx context.Context, obj xpresource.Object) error {
	if err := nf.OperationStore.RemoveTracker(obj); err != nil {
		return errors.Wrap(err, errRemoveTracker)
	}
	return nf.Finalizer.RemoveFinalizer(ctx, obj)
}
