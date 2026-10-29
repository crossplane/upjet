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

// OperationTrackerFinalizer calls RemoveFinalizer of the underlying Finalizer
// and only then removes the operation tracker from the workspace store.
type OperationTrackerFinalizer struct {
	xpresource.Finalizer
	OperationStore TrackerCleaner
}

// AddFinalizer to the supplied Managed resource.
func (nf *OperationTrackerFinalizer) AddFinalizer(ctx context.Context, obj xpresource.Object) error {
	return nf.Finalizer.AddFinalizer(ctx, obj)
}

// RemoveFinalizer removes the finalizer and, only if that succeeds, removes
// the operation tracker from the store. If the finalizer update fails (e.g.,
// with a conflict), the tracker is retained so that the next reconciliation
// still observes the completed deletion.
func (nf *OperationTrackerFinalizer) RemoveFinalizer(ctx context.Context, obj xpresource.Object) error {
	if err := nf.Finalizer.RemoveFinalizer(ctx, obj); err != nil {
		return err
	}
	return errors.Wrap(nf.OperationStore.RemoveTracker(obj), errRemoveTracker)
}
