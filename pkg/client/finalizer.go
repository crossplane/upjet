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

package client

import (
	"context"

	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
)

// TODO(muvaf): A FinalizerChain in crossplane-runtime?

func NewWorkspaceFinalizer(ws *WorkspaceStore, af resource.APIFinalizer) *WorkspaceFinalizer {
	return &WorkspaceFinalizer{
		APIFinalizer: af,
		Store:        ws,
	}
}

type WorkspaceFinalizer struct {
	resource.APIFinalizer
	Store *WorkspaceStore
}

// AddFinalizer to the supplied Managed resource.
func (wf *WorkspaceFinalizer) AddFinalizer(ctx context.Context, obj resource.Object) error {
	return wf.APIFinalizer.AddFinalizer(ctx, obj)
}

// RemoveFinalizer from the supplied Managed resource.
func (wf *WorkspaceFinalizer) RemoveFinalizer(ctx context.Context, obj resource.Object) error {
	if err := wf.Store.Remove(obj); err != nil {
		return errors.Wrap(err, "cannot remove workspace from the store")
	}
	return wf.APIFinalizer.RemoveFinalizer(ctx, obj)
}
