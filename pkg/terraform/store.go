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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/terrajet/pkg/resource"
	"github.com/crossplane-contrib/terrajet/pkg/resource/json"
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
}

func (p Setup) validate() error {
	if p.Version == "" {
		return errors.New(fmtErrValidationVersion)
	}
	if p.Requirement.Source == "" || p.Requirement.Version == "" {
		return errors.Errorf(fmtErrValidationProvider, p.Requirement.Source, p.Requirement.Version)
	}
	return nil
}

func NewWorkspaceStore() *WorkspaceStore {
	return &WorkspaceStore{store: map[types.UID]*Workspace{}, mu: sync.Mutex{}}
}

type WorkspaceStore struct {
	// store holds information about ongoing operations of given resource.
	// Since there can be multiple calls that add/remove values from the map at
	// the same time, it has to be safe for concurrency since those operations
	// cause rehashing in some cases.
	store map[types.UID]*Workspace
	mu    sync.Mutex
}

// TODO(muvaf): Take EnqueueFn as parameter tow WorkspaceStore?

func (ws *WorkspaceStore) Workspace(ctx context.Context, tr resource.Terraformed, ts Setup, l logging.Logger, _ EnqueueFn) (*Workspace, error) {
	dir := filepath.Join(os.TempDir(), string(tr.GetUID()))
	fp, err := NewFileProducer(tr, ts)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create a new file producer")
	}
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return nil, errors.Wrap(err, "cannot create directory for workspace")
	}
	_, err = os.Stat(filepath.Join(dir, "terraform.tfstate"))
	if xpresource.Ignore(os.IsNotExist, err) != nil {
		return nil, errors.Wrap(err, "cannot state terraform.tfstate file")
	}
	if os.IsNotExist(err) {
		s, err := fp.TFState()
		if err != nil {
			return nil, errors.Wrap(err, "cannot produce tfstate")
		}
		rawState, err := json.JSParser.Marshal(s)
		if err != nil {
			return nil, errors.Wrap(err, "cannot marshal state object")
		}
		if err := os.WriteFile(filepath.Join(dir, "terraform.tfstate"), rawState, os.ModePerm); err != nil {
			return nil, errors.Wrap(err, "cannot write tfstate file")
		}
	}
	rawMainTF, err := json.JSParser.Marshal(fp.MainTF())
	if err != nil {
		return nil, errors.Wrap(err, "cannot marshal main hcl object")
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf.json"), rawMainTF, os.ModePerm); err != nil {
		return nil, errors.Wrap(err, "cannot write tfstate file")
	}
	// TODO(muvaf): Set new logger every time?
	ws.mu.Lock()
	w, ok := ws.store[tr.GetUID()]
	if !ok {
		ws.store[tr.GetUID()] = NewWorkspace(dir, WithLogger(l))
		w = ws.store[tr.GetUID()]
	}
	ws.mu.Unlock()
	_, err = os.Stat(filepath.Join(dir, ".terraform.lock.hcl"))
	if xpresource.Ignore(os.IsNotExist, err) != nil {
		return nil, errors.Wrap(err, "cannot stat init lock file")
	}
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

func (ws *WorkspaceStore) Remove(obj xpresource.Object) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	w, ok := ws.store[obj.GetUID()]
	if !ok {
		return nil
	}
	if err := os.RemoveAll(w.dir); err != nil {
		return errors.Wrap(err, "cannot remove workspace folder")
	}
	delete(ws.store, obj.GetUID())
	return nil
}
