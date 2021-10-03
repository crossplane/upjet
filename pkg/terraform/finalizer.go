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

package terraform

import (
	"context"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/resource"
)

const (
	errRemoveWorkspace = "cannot remove workspace from the store"
)

type Store interface {
	Workspace(ctx context.Context, tr resource.Terraformed, ts Setup, l logging.Logger) (*Workspace, error)
	Remove(obj xpresource.Object) error
}

// TODO(muvaf): A FinalizerChain in crossplane-runtime?

// NewWorkspaceFinalizer returns a new WorkspaceFinalizer.
func NewWorkspaceFinalizer(ws Store, af xpresource.Finalizer) *WorkspaceFinalizer {
	return &WorkspaceFinalizer{
		Finalizer: af,
		Store:     ws,
	}
}

// WorkspaceFinalizer removes the workspace from the workspace store and only
// then calls RemoveFinalizer of the underlying Finalizer.
type WorkspaceFinalizer struct {
	xpresource.Finalizer
	Store Store
}

// AddFinalizer to the supplied Managed resource.
func (wf *WorkspaceFinalizer) AddFinalizer(ctx context.Context, obj xpresource.Object) error {
	return wf.Finalizer.AddFinalizer(ctx, obj)
}

// RemoveFinalizer removes the workspace from workspace store before removing
// the finalizer.
func (wf *WorkspaceFinalizer) RemoveFinalizer(ctx context.Context, obj xpresource.Object) error {
	if err := wf.Store.Remove(obj); err != nil {
		return errors.Wrap(err, errRemoveWorkspace)
	}
	return wf.Finalizer.RemoveFinalizer(ctx, obj)
}
