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
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"go.uber.org/multierr"

	"github.com/crossplane-contrib/terrajet/pkg/process"
	tferrors "github.com/crossplane-contrib/terrajet/pkg/tfcli/errors"
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
	errWriteFile        = "failed to write file"
	errReadFile         = "failed to read file"
	fmtErrPath          = "failed to check path on filesystem: %s: Expected a dir: %v, found a dir: %v"
	fmtErrLoadState     = "failed to load state from file: %s"
	fmtErrStoreState    = "failed to store state into file: %s"
	fmtErrAsyncRun      = "failed to run async Terraform pipeline %q with args: %v: in dir: %s"
	fmtErrSyncRun       = "failed to run sync Terraform pipeline %q with args: %v: in dir: %s"

	regexpPlanLine = `Plan:.*([\d+]).*to add,.*([\d+]) to change, ([\d+]) to destroy`

	tfMsgNonExistentResource = "Cannot import non-existent remote object"
)

// Client is an implementation of types.Client and represents a
// Terraform client capable of running Refresh, Apply, Destroy pipelines.
type Client struct {
	provider Provider
	resource Resource
	handle   string
	tfState  []byte
	logger   logging.Logger
	wsPath   string
	pInfo    *process.Info
	fs       afero.Fs
	timeout  *time.Duration
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

func (c *Client) storePipelineResult(log string) error {
	ps := c.pInfo.GetProcessState()
	if ps == nil {
		return errors.New(errNoProcessState)
	}
	return errors.Wrap(
		c.writeFile(filepath.Join(c.wsPath, fileStore),
			[]byte(fmt.Sprintf("%d\n%s", ps.ExitCode(), log)), 0644), errStore)
}

func (c *Client) checkTFStateLock() error {
	tfStateLock := filepath.Join(c.wsPath, fileTFStateLock)
	tfStateLockExists, err := c.pathExists(tfStateLock, false)
	if err != nil {
		return err
	}
	if tfStateLockExists {
		return tferrors.NewPipelineInProgressError(tferrors.PipelineStateLocked)
	}
	return nil
}

// parsePipelineResult returns the exit code and the string contents if
// there is no Terraform state lock and the store file has been generated
// for the specified operation.
// file format assumed <exit code>\n<string output from pipeline>
// Returns exit code, command output and any errors encountered
// Returned exit code is non-nil iff there are no errors
func (c *Client) parsePipelineResult(opType model.OperationType) (*int, string, error) {
	_, err := c.initConfiguration(opType, false)
	if err != nil && !tferrors.IsOperationInProgress(err, opType) {
		return nil, "", err
	}

	opInProgress := err != nil
	if !opInProgress {
		// then caller needs to start an async pipeline.
		// try to save the given state
		if err := c.storeStateInWorkspace(); err != nil {
			return nil, "", err
		}
		return nil, "", tferrors.NewPipelineInProgressError(tferrors.PipelineNotStarted)
	}

	if err := c.checkTFStateLock(); err != nil {
		return nil, "", err
	}
	// then no Terraform state lock file
	storeFile := filepath.Join(c.wsPath, fileStore)
	buff, err := c.readFile(storeFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", tferrors.NewPipelineInProgressError(tferrors.PipelineStateNoStore)
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

type storeResult func(c *Client, stdout, stderr string) error

func (c *Client) asyncPipeline(command string, storeResult storeResult, args ...string) error {
	var err error
	c.pInfo, err = process.New(command, args, c.wsPath,
		true, true, false, c.logger)
	if err != nil {
		return errors.Wrapf(err, fmtErrAsyncRun, command, args, c.wsPath)
	}
	pid, err := c.pInfo.GetPid()
	if err != nil {
		return err
	}
	if err := c.addPidState(pid); err != nil {
		return err
	}
	c.pInfo.LogStdout()
	c.pInfo.LogStderr()
	go func() {
		logger := c.logger.WithValues("args", args, "executable", pathTerraform, "cwd", c.wsPath)
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

func (c *Client) syncPipeline(ctx context.Context, ignoreExitErr bool, command string, args ...string) error {
	var err error
	if c.pInfo, err = process.New(command, args, c.wsPath, false, true, false, c.logger); err != nil {
		return errors.Wrapf(err, fmtErrSyncRun, command, args, c.wsPath)
	}
	exitErr := &exec.ExitError{}
	if err = c.pInfo.Run(ctx); err != nil && (!ignoreExitErr || !errors.As(err, &exitErr)) {
		return errors.Wrapf(err, fmtErrSyncRun, command, args, c.wsPath)
	}
	return nil
}

// load Terraform state into Client's cache
func (c *Client) loadStateFromWorkspace() error {
	pathState := filepath.Join(c.wsPath, fileState)
	var err error
	c.tfState, err = c.readFile(pathState)
	if err != nil {
		return errors.Wrapf(err, fmtErrLoadState, pathState)
	}
	return nil
}

func (c *Client) storeStateInWorkspace() error {
	if c.tfState == nil {
		return nil
	}

	pathState := filepath.Join(c.wsPath, fileState)
	return errors.Wrapf(c.writeFile(pathState, c.tfState, 0600), fmtErrStoreState, pathState)
}

func (c *Client) pathExists(path string, isDir bool) (bool, error) {
	fInfo, err := c.fs.Stat(path)
	if err == nil && fInfo.IsDir() == isDir {
		// path exists and is of expected type
		return true, nil
	}
	if err == nil && fInfo.IsDir() != isDir {
		return false, errors.Errorf(fmtErrPath, path, isDir, fInfo.IsDir())
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, errors.Wrapf(err, fmtErrPath, path, isDir, fInfo.IsDir())
	}
	return false, nil
}

// Close releases resources allocated for this Client.
// After a call to Close, do not reuse the same handle.
func (c *Client) Close(_ context.Context) error {
	var result error
	if c.pInfo != nil {
		result = multierr.Combine(errors.Wrap(c.pInfo.Kill(), errDestroyProcess))
	}
	if c.wsPath != "" {
		result = multierr.Combine(result, errors.Wrap(c.fs.RemoveAll(c.wsPath), errDestroyWorkspace))
	}
	c.handle = ""
	return result
}

type xpState struct {
	Operation model.OperationType `json:"operation"`
	Pid       int                 `json:"pid"`
	Ts        time.Time           `json:"ts"`
}

func (c *Client) getHandle() (string, error) {
	handle := c.handle
	// if no handle has been given, generate a new one
	if handle == "" {
		u, err := uuid.NewUUID()
		if err != nil {
			return "", err
		}
		handle = u.String()
		c.handle = handle
	}
	// md5sum the handle so that it's safe to use in paths
	handle = fmt.Sprintf("%x", sha256.Sum256([]byte(handle)))
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

func (c Client) generateTFConfiguration() ([]byte, error) {
	tmpl, err := template.New(tplMain).Parse(templates.TFConfigurationMain)
	if err != nil {
		return nil, err
	}

	var buff bytes.Buffer
	if err := tmpl.Execute(&buff, &tfConfigTemplateParams{
		ProviderSource:        c.provider.Source,
		ProviderVersion:       c.provider.Version,
		ProviderConfiguration: c.provider.Configuration,
		ResourceType:          c.resource.LabelType,
		ResourceName:          c.resource.LabelName,
		ResourceBody:          c.resource.Body,
	}); err != nil {
		return nil, err
	}

	return buff.Bytes(), nil
}

func (c *Client) writeFile(filename string, data []byte, perm fs.FileMode) error {
	f, err := c.fs.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return errors.Wrap(err, errWriteFile)
	}
	_, err = f.Write(data)
	return errors.Wrap(err, errWriteFile)
}

func (c *Client) readFile(filename string) ([]byte, error) {
	f, err := c.fs.Open(filename)
	if err != nil {
		return nil, errors.Wrap(err, errReadFile)
	}
	buff, err := io.ReadAll(f)
	return buff, errors.Wrap(err, errReadFile)
}
