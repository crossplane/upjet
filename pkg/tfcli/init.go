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
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/multierr"

	cliErrors "github.com/crossplane-contrib/terrajet/pkg/tfcli/errors"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/types"
)

const (
	fileInitLock  = ".terraform.lock.hcl"
	fileStateLock = ".xp.lock"
	prefixWSDir   = "ws-"
	// error messages
	errInitWorkspace    = "failed to initialize temporary Terraform workspace"
	fmtErrXPStateRemove = "failed to remove Crossplane state file: %s"
	fmtErrStoreRemove   = "failed to remove pipeline store file: %s"
	fmtErrNoWS          = "failed to initialize Terraform configuration: No workspace folder: %s"
	fmtErrXPState       = "failed to load Crossplane state file: %s"
)

// init initializes a workspace in a synchronous manner using Terraform CLI
// Workspace initialization is potentially a long-running task
func (c *client) init(ctx context.Context) error {
	// initialize the workspace, and
	// check if init lock & state lock exist, i.e., there is an ongoing Terraform CLI operation
	initLockExists := false
	err := c.closeOnError(ctx, func() error {
		var err error
		initLockExists, err = c.initConfiguration(types.OperationInit, true)
		if (err == nil || errors.Is(err, cliErrors.OperationInProgressError{})) && initLockExists {
			if err == nil || cliErrors.IsOperationInProgress(err, types.OperationInit) {
				return c.removeStateStore()
			}
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	// TODO(aru): what if Terraform CLI has crashed before having a chance to
	// remove the lock?
	if !initLockExists {
		// then we need to call an init
		// TODO(aru): Shared gRPC server configuration will not involve an init lock.
		return multierr.Combine(c.syncPipeline(ctx, false, pathTerraform, "init", "-input=false"),
			c.removeStateStore())
	}
	return nil
}

// initConfiguration checks and initializes a Terraform workspace with a proper
// configuration. If client's workspace does not yet exist, it can prepare
// workspace dir if mkWorkspace is set.
// Returns true if Terraform Init lock exists.
func (c *client) initConfiguration(opType types.OperationType, mkWorkspace bool) (bool, error) {
	handle, err := c.getHandle()
	if err != nil {
		return false, errors.Wrap(err, errInitWorkspace)
	}
	c.wsPath = filepath.Join(os.TempDir(), prefixWSDir+handle)

	// check if the workspace already exists, i.e. there is an open operation
	ok, err := pathExists(c.wsPath, true)
	if err != nil {
		return false, err
	}
	if !ok && !mkWorkspace {
		return false, errors.Errorf(fmtErrNoWS, c.wsPath)
	}

	initLockExists := false
	if ok {
		initLockExists, err = pathExists(filepath.Join(c.wsPath, fileInitLock), false)
		if err != nil {
			return false, err
		}

		// check the state lock. If state lock exists, do not overwrite config
		err = c.checkOperation()
		if !errors.Is(err, os.ErrNotExist) {
			return initLockExists, err
		}
	}
	// workspace does not exist & make workspace is requested or
	// no state lock file
	if err := os.MkdirAll(c.wsPath, 0755); err != nil {
		return initLockExists, errors.Wrap(err, errInitWorkspace)
	}

	conf, err := c.generateTFConfiguration()
	if err != nil {
		return initLockExists, errors.Wrap(err, errInitWorkspace)
	}
	if err := errors.Wrap(ioutil.WriteFile(filepath.Join(c.wsPath, tplMain), conf, 0644), errInitWorkspace); err != nil {
		return initLockExists, err
	}

	xpState := xpState{
		Operation: opType,
	}
	buff, err := json.Marshal(xpState)
	if err != nil {
		return initLockExists, errors.Wrap(err, errInitWorkspace)
	}
	return initLockExists,
		errors.Wrap(ioutil.WriteFile(filepath.Join(c.wsPath, fileStateLock), buff, 0644), errInitWorkspace)
}

func (c *client) checkOperation() error {
	xpStatePath := filepath.Join(c.wsPath, fileStateLock)
	// Terraform state lock file does not seem to contain operation type
	buff, err := ioutil.ReadFile(xpStatePath)
	if err != nil {
		return errors.Wrapf(err, fmtErrXPState, xpStatePath)
	}

	xpState := &xpState{}
	if err := json.Unmarshal(buff, xpState); err != nil {
		return errors.Wrapf(err, fmtErrXPState, xpStatePath)
	}
	return cliErrors.NewOperationInProgressError(xpState.Operation)
}

// removeStateStore removes Crossplane state lock & store
// returning any errors encountered
func (c *client) removeStateStore() error {
	stateFile := filepath.Join(c.wsPath, fileStateLock)
	storeFile := filepath.Join(c.wsPath, fileStore)
	return multierr.Combine(errors.Wrapf(os.RemoveAll(stateFile), fmtErrXPStateRemove, stateFile),
		errors.Wrapf(os.RemoveAll(storeFile), fmtErrStoreRemove, storeFile))
}
