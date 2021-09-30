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
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane-contrib/terrajet/pkg/resource"
	"github.com/crossplane-contrib/terrajet/pkg/resource/json"

	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
)

// ProviderRequirement holds values for the Terraform HCL setup requirements
type ProviderRequirement struct {
	Source  string
	Version string
}

// ProviderConfiguration holds the setup configuration body
type ProviderConfiguration map[string]interface{}

// TerraformSetup holds values for the Terraform version and setup
// requirements and configuration body
type TerraformSetup struct {
	Version       string
	Requirement   ProviderRequirement
	Configuration ProviderConfiguration
}

func (p TerraformSetup) validate() error {
	if p.Version == "" {
		return errors.New(fmtErrValidationVersion)
	}
	if p.Requirement.Source == "" || p.Requirement.Version == "" {
		return errors.Errorf(fmtErrValidationProvider, p.Requirement.Source, p.Requirement.Version)
	}
	return nil
}

func NewWorkspaceStore() *WorkspaceStore {
	return &WorkspaceStore{store: sync.Map{}}
}

type WorkspaceStore struct {
	// store holds information about ongoing operations of given resource.
	// Since there can be multiple calls that add/remove values from the map at
	// the same time, it has to be safe for concurrency since those operations
	// cause rehashing in some cases.
	store sync.Map
}

// TODO(muvaf): Take EnqueueFn as parameter tow WorkspaceStore?

func (ws *WorkspaceStore) Workspace(ctx context.Context, tr resource.Terraformed, ts TerraformSetup, l logging.Logger, _ EnqueueFn) (*Workspace, error) {
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
	rawHCL, err := json.JSParser.Marshal(fp.MainTF())
	if err != nil {
		return nil, errors.Wrap(err, "cannot marshal main hcl object")
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf.json"), rawHCL, os.ModePerm); err != nil {
		return nil, errors.Wrap(err, "cannot write tfstate file")
	}
	// TODO(muvaf): Set new logger every time?
	o, _ := ws.store.LoadOrStore(tr.GetUID(), NewWorkspace(dir, WithLogger(l)))
	w := o.(*Workspace)
	// The operation has ended but there could be a lock file if Terraform crashed.
	// So, we clean that up here. We could check the existence of the file first
	// but just deleting in all cases get us to the same state with less logic and
	// same number of fs calls.
	if w.LastOperation.EndTime != nil {
		if err := os.RemoveAll(filepath.Join(dir, ".terraform.tfstate.lock.info")); err != nil {
			return nil, err
		}
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := exec.CommandContext(ctx, "terraform", "init", "-input=false")
	cmd.Dir = w.dir
	err = cmd.Run()
	l.Debug("init completed", "stdout", stdout.String())
	l.Debug("init completed", "stderr", stderr.String())
	return w, errors.Wrapf(err, "cannot init workspace\nstderr: %s\nstdout: %s", stderr.String(), stdout.String())
}

func (ws *WorkspaceStore) Remove(obj xpresource.Object) error {
	w, ok := ws.store.Load(obj.GetUID())
	if !ok {
		return nil
	}
	if err := os.RemoveAll(w.(*Workspace).dir); err != nil {
		return errors.Wrap(err, "cannot remove workspace folder")
	}
	ws.store.Delete(obj.GetUID())
	return nil
}
