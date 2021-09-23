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
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/multierr"

	"github.com/crossplane-contrib/terrajet/pkg/json"
	tferrors "github.com/crossplane-contrib/terrajet/pkg/tfcli/errors"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/templates"
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
	fmtErrXPStateWrite  = "failed to write Crossplane state file: %s"
)

// init initializes a workspace in a synchronous manner using Terraform CLI
// Workspace initialization is potentially a long-running task
func (c *Client) init(ctx context.Context) error {
	// initialize the workspace, and
	// check if init lock & state lock exist, i.e., there is an ongoing Terraform CLI operation
	initLockExists, err := c.initConfiguration(model.OperationInit, true)
	if (err == nil || errors.Is(err, tferrors.OperationInProgressError{})) && initLockExists {
		if err == nil || tferrors.IsOperationInProgress(err, model.OperationInit) {
			return c.removeStateStore()
		}
		return nil // async operation is in-progress and workspace is already initialized
	}
	if err != nil {
		return multierr.Combine(err, c.Close(ctx))
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

func (c *Client) initWSPath() {
	// md5sum the handle so that it's safe to use in paths
	handle := fmt.Sprintf("%x", sha256.Sum256([]byte(c.handle)))
	c.wsPath = filepath.Join(os.TempDir(), prefixWSDir+handle)
}

// initConfiguration checks and initializes a Terraform workspace with a proper
// configuration. If Client's workspace does not yet exist, it can prepare
// workspace dir if mkWorkspace is set.
// Returns true if Terraform Init lock exists.
func (c *Client) initConfiguration(opType model.OperationType, mkWorkspace bool) (bool, error) { // nolint:gocyclo
	// the cyclomatic complexity of this method (11) is slightly larger than our goal of 10
	c.initWSPath()
	// check if the workspace already exists, i.e. there is an open operation
	ok, err := c.pathExists(c.wsPath, true)
	if err != nil {
		return false, err
	}
	if !ok && !mkWorkspace {
		return false, errors.Errorf(fmtErrNoWS, c.wsPath)
	}

	initLockExists := false
	if ok {
		initLockExists, err = c.pathExists(filepath.Join(c.wsPath, fileInitLock), false)
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
	if err := c.fs.MkdirAll(c.wsPath, 0750); err != nil {
		return initLockExists, errors.Wrap(err, errInitWorkspace)
	}

	conf, err := c.generateTFConfiguration(opType != model.OperationDestroy)
	if err != nil {
		return initLockExists, errors.Wrap(err, errInitWorkspace)
	}
	// Terraform configuration file can potentially contain credentials, hence
	// no read permissions for group & others.
	if err := errors.Wrap(c.writeFile(filepath.Join(c.wsPath, tplMain), conf, 0600), errInitWorkspace); err != nil {
		return initLockExists, err
	}

	ts := time.Now()
	if c.timeout != nil {
		ts = ts.Add(*c.timeout)
	}
	xpState := &xpState{
		Operation: opType,
		Ts:        ts,
	}
	return initLockExists, c.writeStateLock(xpState)
}

func (c Client) generateTFConfiguration(preventDestroy bool) ([]byte, error) {
	tmpl, err := template.New(tplMain).Parse(templates.TFConfigurationMain)
	if err != nil {
		return nil, err
	}
	c.resource.Parameters["lifecycle"] = map[string]bool{
		"prevent_destroy": preventDestroy,
	}
	config, err := json.JSParser.Marshal(c.setup.Configuration)
	if err != nil {
		return nil, errors.Wrap(err, "cannot marshal provider config")
	}
	body, err := json.JSParser.Marshal(c.resource.Parameters)
	if err != nil {
		return nil, errors.Wrap(err, "cannot marshal resource body")
	}
	var buff bytes.Buffer
	vars := map[string]interface{}{
		"Provider": map[string]interface{}{
			"Configuration": config,
		},
		"Resource": map[string]interface{}{
			"LabelType": c.resource.LabelType,
			"LabelName": c.resource.LabelName,
			"Body":      body,
		},
	}
	// NOTE(muvaf): If you run this as part of return statement, buff.Bytes() will
	// be called before execution and it will be empty.
	if err := tmpl.Execute(&buff, &vars); err != nil {
		return nil, err
	}
	return buff.Bytes(), nil
}

type xpState struct {
	Operation model.OperationType `json:"operation"`
	Pid       int                 `json:"pid"`
	Ts        time.Time           `json:"ts"`
}

func (c *Client) addPidState(pid int) error {
	xpState, err := c.readStateLock()
	if err != nil {
		return err
	}
	xpState.Pid = pid
	return c.writeStateLock(xpState)
}

func (c *Client) checkOperation() error {
	xpState, err := c.readStateLock()
	if err != nil {
		return err
	}
	// check if operation timed out if timeout is configured
	if c.timeout != nil && xpState.Ts.Before(time.Now()) {
		storeExists, err := c.pathExists(filepath.Join(c.wsPath, fileStore), false)
		if err != nil {
			return err
		}
		// then async operation has timed out before generating a store file
		if !storeExists {
			return c.removeStateStore()
		}
	}
	return tferrors.NewOperationInProgressError(xpState.Operation)
}

func (c *Client) writeStateLock(xpState *xpState) error {
	xpStatePath := filepath.Join(c.wsPath, fileStateLock)
	buff, err := json.JSParser.Marshal(xpState)
	if err != nil {
		return errors.Wrapf(err, fmtErrXPStateWrite, xpStatePath)
	}
	return errors.Wrapf(c.writeFile(xpStatePath, buff, 0644), fmtErrXPStateWrite, xpStatePath)
}

func (c *Client) readStateLock() (*xpState, error) {
	xpStatePath := filepath.Join(c.wsPath, fileStateLock)
	// Terraform state lock file does not seem to contain operation type
	buff, err := c.readFile(xpStatePath)
	if err != nil {
		return nil, errors.Wrapf(err, fmtErrXPState, xpStatePath)
	}

	xpState := &xpState{}
	if err := json.JSParser.Unmarshal(buff, xpState); err != nil {
		return nil, errors.Wrapf(err, fmtErrXPState, xpStatePath)
	}
	return xpState, nil
}

// removeStateStore removes Crossplane state lock & store
// returning any errors encountered
func (c *Client) removeStateStore() error {
	stateFile := filepath.Join(c.wsPath, fileStateLock)
	storeFile := filepath.Join(c.wsPath, fileStore)
	return multierr.Combine(errors.Wrapf(c.fs.RemoveAll(stateFile), fmtErrXPStateRemove, stateFile),
		errors.Wrapf(c.fs.RemoveAll(storeFile), fmtErrStoreRemove, storeFile))
}
