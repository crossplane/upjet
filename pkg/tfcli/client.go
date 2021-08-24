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
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.uber.org/multierr"

	"github.com/crossplane-contrib/terrajet/pkg/process"
	cliErrors "github.com/crossplane-contrib/terrajet/pkg/tfcli/errors"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/templates"
)

const (
	tplMain         = "main.tf.json"
	fileTFStateLock = ".terraform.tfstate.lock.info"
	fileState       = "terraform.tfstate"
	fileStore       = ".store"
	pathTerraform   = "terraform"
	// error messages
	errDestroyProcess   = "failed to kill Terraform CLI process"
	errDestroyWorkspace = "failed to destroy Terraform workspace"
	errNoProcessState   = "failed to store process state: no process state"
	errStore            = "failed to store process state"
	errCheckExitCode    = "failed to check process exit code"
	errNoPlan           = "plan line not found in Terraform CLI output"
	errPlan             = "failed to parse the Terraform plan"
	fmtErrPath          = "failed to check path on filesystem: %s: Expected a dir: %v, found a dir: %v"
	fmtErrLoadState     = "failed to load state from file: %s"
	fmtErrStoreState    = "failed to store state into file: %s"
	fmtErrAsyncRun      = "failed to run async Terraform pipeline %q with args: %v: in dir: %s"
	fmtErrSyncRun       = "failed to run sync Terraform pipeline %q with args: %v: in dir: %s"

	regexpPlanLine = `Plan:.*([\d+]).*to add,.*([\d+]) to change, ([\d+]) to destroy`

	tfMsgNonExistentResource = "Cannot import non-existent remote object"
)

type client struct {
	state       *withState
	provider    *withProvider
	resource    *withResource
	execTimeout *withTimeout
	logger      *withLogger
	wsPath      string
	pInfo       *process.Info
}

// returns true if no resource is to be added according to the plan
func tfPlanCheckAdd(log string) (bool, error) {
	r := regexp.MustCompile(regexpPlanLine)
	m := r.FindStringSubmatch(log)
	if len(m) != 4 || len(m[1]) == 0 {
		return false, errors.New(errNoPlan)
	}
	addCount, err := strconv.Atoi(m[1])
	if err != nil {
		return false, errors.Wrap(err, errPlan)
	}
	return addCount == 0, nil
}

func (c *client) storePipelineResult(log string) error {
	ps := c.pInfo.GetProcessState()
	if ps == nil {
		return errors.New(errNoProcessState)
	}
	return errors.Wrap(
		ioutil.WriteFile(filepath.Join(c.wsPath, fileStore),
			[]byte(fmt.Sprintf("%d\n%s", ps.ExitCode(), log)), 0644), errStore)
}

func (c *client) checkTFStateLock() error {
	tfStateLock := filepath.Join(c.wsPath, fileTFStateLock)
	tfStateLockExists, err := pathExists(tfStateLock, false)
	if err != nil {
		return err
	}
	if tfStateLockExists {
		return cliErrors.NewPipelineInProgressError(cliErrors.PipelineStateLocked)
	}
	return nil
}

// parsePipelineResult returns the exit code and the string contents if
// there is no Terraform state lock and the store file has been generated
// for the specified operation.
// file format assumed <exit code>\n<string output from pipeline>
// Returns exit code, command output and any errors encountered
// Returned exit code is non-nil iff there are no errors
func (c *client) parsePipelineResult(opType model.OperationType) (*int, string, error) {
	_, err := c.initConfiguration(opType, false)
	if err != nil && !cliErrors.IsOperationInProgress(err, opType) {
		return nil, "", err
	}

	opInProgress := err != nil
	if !opInProgress {
		// then caller needs to start an async pipeline.
		// try to save the given state
		if err := c.storeStateInWorkspace(); err != nil {
			return nil, "", err
		}
		return nil, "", cliErrors.NewPipelineInProgressError(cliErrors.PipelineNotStarted)
	}

	if err := c.checkTFStateLock(); err != nil {
		return nil, "", err
	}
	// then no Terraform state lock file
	storeFile := filepath.Join(c.wsPath, fileStore)
	buff, err := ioutil.ReadFile(storeFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", cliErrors.NewPipelineInProgressError(cliErrors.PipelineStateNoStore)
	}
	if err != nil {
		return nil, "", errors.Wrapf(err, errCheckExitCode)
	}

	contents := string(buff)
	parts := strings.Split(contents, "\n")
	storedCode, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, "", errors.Wrap(err, errCheckExitCode)
	}
	return &storedCode, contents[len(parts[0])+1:], c.removeStateStore()
}

type storeResult func(c *client, stdout, stderr string) error

func (c *client) asyncPipeline(command string, storeResult storeResult, args ...string) error {
	var err error
	c.pInfo, err = process.New(command, args, c.wsPath,
		true, true, false, c.logger.log)
	if err != nil {
		return errors.Wrapf(err, fmtErrAsyncRun, command, args, c.wsPath)
	}
	c.pInfo.LogStdout()
	c.pInfo.LogStderr()
	go func() {
		logger := c.logger.log.WithValues("args", args, "executable", pathTerraform, "cwd", c.wsPath)
		logger.Info("Waiting for process to terminate gracefully.", "arg-from-process", c.pInfo.GetCmd().String())
		err := c.pInfo.WaitError()
		stderr, _ := c.pInfo.StderrAsString()
		stdout, _ := c.pInfo.StdoutAsString()
		logger = logger.WithValues("stderr", stderr, "stdout", stdout)

		if err != nil {
			logger.Info("Failed to run Terraform CLI", "error", err)
		} else {
			logger.Info("Successfully executed Terraform CLI", "stdout", stdout)
		}
		if storeResult != nil {
			storePath := filepath.Join(c.wsPath, fileStore)
			if err := storeResult(c, stdout, stderr); err != nil {
				logger.Info("Failed to store result from Terraform CLI", "store", storePath)
				return
			}
			logger.Info("Successfully stored result from Terraform CLI", "store", storePath)
		}
	}()
	return nil
}

func (c *client) syncPipeline(ctx context.Context, ignoreExitErr bool, command string, args ...string) error {
	var err error
	if c.pInfo, err = process.New(command, args, c.wsPath, false, true, false, c.logger.log); err != nil {
		return errors.Wrapf(err, fmtErrSyncRun, command, args, c.wsPath)
	}
	exitErr := &exec.ExitError{}
	if err = c.pInfo.Run(ctx); err != nil && (!ignoreExitErr || !errors.As(err, &exitErr)) {
		return errors.Wrapf(err, fmtErrSyncRun, command, args, c.wsPath)
	}
	return nil
}

// returns true if state file exists
// TODO(aru): differentiate not-found error
func (c *client) loadStateFromWorkspace(errNoState bool) (bool, error) {
	pathState := filepath.Join(c.wsPath, fileState)
	var err error
	c.state.tfState, err = ioutil.ReadFile(pathState)
	if err == nil {
		return true, nil
	}
	if !os.IsNotExist(err) || errNoState {
		return false, errors.Wrapf(err, fmtErrLoadState, pathState)
	}
	// then state not found
	return false, nil
}

func (c *client) storeStateInWorkspace() error {
	if c.state.tfState == nil {
		return nil
	}

	pathState := filepath.Join(c.wsPath, fileState)
	return errors.Wrapf(ioutil.WriteFile(pathState, c.state.tfState, 0600), fmtErrStoreState, pathState)
}

func pathExists(path string, isDir bool) (bool, error) {
	fInfo, err := os.Stat(path)
	if err == nil && fInfo.IsDir() == isDir {
		// path exists and is of expected type
		return true, nil
	}
	if err == nil && fInfo.IsDir() != isDir {
		return false, errors.Errorf(fmtErrPath, path, isDir, fInfo.IsDir())
	}
	if err != nil && !os.IsNotExist(err) {
		return false, errors.Wrapf(err, fmtErrPath, path, isDir, fInfo.IsDir())
	}
	return false, nil
}

func (c *client) closeOnError(ctx context.Context, f func() error) error {
	err := f()
	if err != nil {
		err = multierr.Combine(err, c.Close(ctx))
	}
	return err
}

// Close releases resources allocated for this client.
// After a call to Close, do not reuse the same handle.
func (c *client) Close(_ context.Context) error {
	var result error
	if c.pInfo != nil {
		result = multierr.Combine(errors.Wrap(c.pInfo.Kill(), errDestroyProcess))
	}
	if c.wsPath != "" {
		result = multierr.Combine(result, errors.Wrap(os.RemoveAll(c.wsPath), errDestroyWorkspace))
	}
	c.resource.handle = ""
	return result
}

type xpState struct {
	Operation model.OperationType `json:"operation"`
}

func (c *client) getHandle() (string, error) {
	handle := c.resource.handle
	// if no handle has been given, generate a new one
	if handle == "" {
		u, err := uuid.NewUUID()
		if err != nil {
			return "", err
		}
		handle = u.String()
		c.resource.handle = handle
	}
	// md5sum the handle so that it's safe to use in paths
	handle = fmt.Sprintf("%x", md5.Sum([]byte(handle)))
	return handle, nil
}

type tfConfigTemplateParams struct {
	ProviderSource        string
	ProviderVersion       string
	ProviderConfiguration []byte
	ResourceType          string
	ResourceName          string
	ResourceBody          []byte
}

func (c client) generateTFConfiguration() ([]byte, error) {
	tmpl, err := template.New(tplMain).Parse(templates.TFConfigurationMain)
	if err != nil {
		return nil, err
	}

	var buff bytes.Buffer
	if err := tmpl.Execute(&buff, &tfConfigTemplateParams{
		ProviderSource:        c.provider.source,
		ProviderVersion:       c.provider.version,
		ProviderConfiguration: c.provider.configuration,
		ResourceType:          c.resource.labelType,
		ResourceName:          c.resource.labelName,
		ResourceBody:          c.resource.body,
	}); err != nil {
		return nil, err
	}

	return buff.Bytes(), nil
}
