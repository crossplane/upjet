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
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/terrajet/pkg/resource"
)

// SetupFn is a function that returns Terraform setup which contains
// provider requirement, configuration and Terraform version.
type SetupFn func(ctx context.Context, client client.Client, mg xpresource.Managed) (Setup, error)

// ProviderRequirement holds values for the Terraform HCL setup requirements
type ProviderRequirement struct {
	Source  string
	Version string
}

// ProviderConfiguration holds the setup configuration body
type ProviderConfiguration map[string]interface{}

// Setup holds values for the Terraform version and setup
// requirements and configuration body
type Setup struct {
	Version       string
	Requirement   ProviderRequirement
	Configuration ProviderConfiguration
	Env           []string
}

// WorkspaceStoreOption lets you configure the workspace store.
type WorkspaceStoreOption func(*WorkspaceStore)

// WithFs lets you set the fs of WorkspaceStore. Used mostly for testing.
func WithFs(fs afero.Fs) WorkspaceStoreOption {
	return func(ws *WorkspaceStore) {
		ws.fs = afero.Afero{Fs: fs}
	}
}

// NewWorkspaceStore returns a new WorkspaceStore.
func NewWorkspaceStore(l logging.Logger, opts ...WorkspaceStoreOption) *WorkspaceStore {
	ws := &WorkspaceStore{
		store:  map[types.UID]*Workspace{},
		logger: l,
		mu:     sync.Mutex{},
		fs:     afero.Afero{Fs: afero.NewOsFs()},
	}
	for _, f := range opts {
		f(ws)
	}
	return ws
}

// WorkspaceStore allows you to manage multiple Terraform workspaces.
type WorkspaceStore struct {
	// store holds information about ongoing operations of given resource.
	// Since there can be multiple calls that add/remove values from the map at
	// the same time, it has to be safe for concurrency since those operations
	// cause rehashing in some cases.
	store  map[types.UID]*Workspace
	logger logging.Logger

	mu sync.Mutex
	fs afero.Afero
}

// Workspace makes sure the Terraform workspace for the given resource is ready
// to be used and returns the Workspace object configured to work in that
// workspace folder in the filesystem.
func (ws *WorkspaceStore) Workspace(ctx context.Context, c resource.SecretClient, tr resource.Terraformed, ts Setup) (*Workspace, error) {
	dir := filepath.Join(ws.fs.GetTempDir(""), string(tr.GetUID()))
	if err := ws.fs.MkdirAll(dir, os.ModePerm); err != nil {
		return nil, errors.Wrap(err, "cannot create directory for workspace")
	}
	fp, err := NewFileProducer(ctx, c, dir, tr, ts)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create a new file producer")
	}
	_, err = ws.fs.Stat(filepath.Join(fp.Dir, "terraform.tfstate"))
	if xpresource.Ignore(os.IsNotExist, err) != nil {
		return nil, errors.Wrap(err, "cannot stat terraform.tfstate file")
	}
	if os.IsNotExist(err) {
		if err := fp.WriteTFState(); err != nil {
			return nil, errors.Wrap(err, "cannot reproduce tfstate file")
		}
	}
	if err := fp.WriteMainTF(); err != nil {
		return nil, errors.Wrap(err, "cannot write main tf file")
	}
	l := ws.logger.WithValues("workspace", dir)
	ws.mu.Lock()
	w, ok := ws.store[tr.GetUID()]
	if !ok {
		ws.store[tr.GetUID()] = NewWorkspace(dir, WithLogger(l))
		w = ws.store[tr.GetUID()]
	}
	ws.mu.Unlock()
	_, err = ws.fs.Stat(filepath.Join(dir, ".terraform.lock.hcl"))
	if xpresource.Ignore(os.IsNotExist, err) != nil {
		return nil, errors.Wrap(err, "cannot stat init lock file")
	}
	w.env = ts.Env
	// We need to initialize only if the workspace hasn't been initialized yet.
	if !os.IsNotExist(err) {
		return w, nil
	}
	cmd := exec.CommandContext(ctx, "terraform", "init", "-input=false")
	cmd.Dir = w.dir
	out, err := cmd.CombinedOutput()
	l.Debug("init ended", "out", string(out))
	return w, errors.Wrapf(err, "cannot init workspace: %s", string(out))
}

// Remove deletes the workspace directory from the filesystem and erases its
// record from the store.
func (ws *WorkspaceStore) Remove(obj xpresource.Object) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	w, ok := ws.store[obj.GetUID()]
	if !ok {
		return nil
	}
	if err := ws.fs.RemoveAll(w.dir); err != nil {
		return errors.Wrap(err, "cannot remove workspace folder")
	}
	delete(ws.store, obj.GetUID())
	return nil
}
