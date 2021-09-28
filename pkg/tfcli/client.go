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

package tfcli

import (
	"os"
	"path/filepath"

	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/crossplane-contrib/terrajet/pkg/json"
)

func NewWorkspaceStore(setup TerraformSetup) *WorkspaceStore {
	return &WorkspaceStore{setup: setup}
}

type WorkspaceStore struct {
	// store holds information about ongoing operations of given resource.
	// It is not necessary to make it safe for concurrency access as long as
	// there is only one single go routine that writes to a given single entry.
	store map[types.UID]*Workspace

	setup TerraformSetup
}

// TODO(muvaf): Take EnqueueFn as parameter tow WorkspaceStore?

func (ws *WorkspaceStore) Workspace(tr resource.Terraformed, enq EnqueueFn) (*Workspace, error) {
	dir := filepath.Join(os.TempDir(), string(tr.GetUID()))
	fp, err := NewFileProducer(tr)
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
	if _, ok := ws.store[tr.GetUID()]; !ok {
		ws.store[tr.GetUID()] = &Workspace{
			Enqueue: enq,
			dir:     dir,
		}
	}
	return ws.store[tr.GetUID()], nil
}

func (ws *WorkspaceStore) Remove(obj xpresource.Object) error {
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
