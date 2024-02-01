// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
)

const (
	errRemoveTracker = "cannot remove tracker from the store"
)

// TrackerCleaner is the interface that the no-fork finalizer needs to work with.
type TrackerCleaner interface {
	RemoveTracker(obj xpresource.Object) error
}

// NewTerraformPluginSDKFinalizer returns a new TerraformPluginSDKFinalizer.
func NewTerraformPluginSDKFinalizer(tc TrackerCleaner, af xpresource.Finalizer) *TerraformPluginSDKFinalizer {
	return &TerraformPluginSDKFinalizer{
		Finalizer:      af,
		OperationStore: tc,
	}
}

// TerraformPluginSDKFinalizer removes the operation tracker from the workspace store and only
// then calls RemoveFinalizer of the underlying Finalizer.
type TerraformPluginSDKFinalizer struct {
	xpresource.Finalizer
	OperationStore TrackerCleaner
}

// AddFinalizer to the supplied Managed resource.
func (nf *TerraformPluginSDKFinalizer) AddFinalizer(ctx context.Context, obj xpresource.Object) error {
	return nf.Finalizer.AddFinalizer(ctx, obj)
}

// RemoveFinalizer removes the workspace from workspace store before removing
// the finalizer.
func (nf *TerraformPluginSDKFinalizer) RemoveFinalizer(ctx context.Context, obj xpresource.Object) error {
	if err := nf.OperationStore.RemoveTracker(obj); err != nil {
		return errors.Wrap(err, errRemoveTracker)
	}
	return nf.Finalizer.RemoveFinalizer(ctx, obj)
}
