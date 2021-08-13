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
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.uber.org/multierr"

	"github.com/crossplane-contrib/terrajet/pkg/process"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/templates"
)

const (
	tplMain       = "main.tf.json"
	fileInitLock  = ".terraform.lock.hcl"
	fileStateLock = ".terraform.tfstate.lock.info"
	fileState     = "terraform.tfstate"
	fileStore     = ".store"
	pathTerraform = "terraform"
	prefixWSDir   = "ws-"
	// error messages
	errInitWorkspace    = "failed to initialize temporary Terraform workspace"
	errDestroyProcess   = "failed to kill Terraform CLI process"
	errDestroyWorkspace = "failed to destroy Terraform workspace"
	errStateUnmarshall  = "failed to unmarshal Terraform state information"
	errNoProcessState   = "failed to store process state: no process state"
	errStore            = "failed to store process state"
	errCheckExitCode    = "failed to check process exit code"
	errNoID             = "failed to observe the Terraform state: cannot deduce resource ID"
	errNoState          = "missing Terraform state file"
	errImport           = "failed to import resource"
	errNoPlan           = "plan line not found in Terraform CLI output"
	errPlan             = "failed to parse the Terraform plan"
	fmtErrPath          = "failed to check path on filesystem: %s: Expected a dir: %v, found a dir: %v"
	fmtErrLoadState     = "failed to load state from file: %s"
	fmtErrStoreState    = "failed to store state into file: %s"
	fmtErrOpRun         = "failed to run Terraform CLI at path %q with args: %v: in dir: %s"

	regexpPlanLine = `Plan:.*([\d+]).*to add,.*([\d+]) to change, ([\d+]) to destroy`
)

type Client struct {
	state       *withState
	provider    *withProvider
	resource    *withResource
	context     *withContext
	execTimeout *withTimeout
	logger      *withLogger
	wsPath      string
	pInfo       *process.Info
	mu          *sync.Mutex
}

func (c Client) GetState() []byte {
	return c.state.tfState
}

func (c Client) GetHandle() string {
	return c.resource.handle
}

// Create attempts to provision the resource.
// Returns false if the operation has not yet been completed.
func (c *Client) Create() (bool, error) {
	initLockExists, stateLockExists, err := c.initOperation()
	if err != nil {
		return false, err
	}
	if !initLockExists || stateLockExists {
		return false, nil
	}

	// then check the state and try to load it if available
	stateExists, err := c.loadStateFromWorkspace(false)
	if err != nil {
		return false, err
	}
	if stateExists {
		// and it has been stored
		return true, nil
	}

	// workspace initialized but no state => run Terraform command
	return false, c.runOperation(pathTerraform, nil, "apply", "-auto-approve", "-input=false")
}

// Delete attempts to delete the resource.
// Returns false if the operation has not yet been completed.
func (c *Client) Delete() (bool, error) {
	initLockExists, stateLockExists, err := c.initOperation()
	if err != nil {
		return false, err
	}
	if !initLockExists || stateLockExists {
		return false, nil
	}
	// check if the resource has been removed from workspace state
	removed, err := c.isRemovedFromState()
	if err != nil {
		return false, err
	}
	if removed {
		return true, nil
	}
	// try to save the given state
	if err := c.storeStateInWorkspace(); err != nil {
		return false, err
	}
	// now try to delete the resource
	return false, c.runOperation(pathTerraform, nil, "destroy", "-auto-approve", "-input=false")
}

type ObserveResult struct {
	UpToDate  bool
	Completed bool
	Exists    bool
}

// IsUpToDate checks whether the specified resource is up-to-date.
// Returns false if the operation has not yet been completed.
func (c *Client) observe() (ObserveResult, error) {
	initLockExists, stateLockExists, err := c.initOperation()
	result := ObserveResult{}
	if err != nil {
		return result, err
	}
	if !initLockExists || stateLockExists {
		return result, nil
	}
	code, stderr, err := c.getExitCode()
	// if store file is not found yet, probably because
	// process did not complete or even start, have the
	// flow continue
	if err != nil && !os.IsNotExist(errors.Cause(err)) {
		return result, err
	}
	if err == nil {
		// fixme(aru): we had better return an error if code not in {0,2}
		// currently we are retrying the operation but we had better
		// leave retries to the K8s controller
		result.Completed = *code == 0 || *code == 2
		result.UpToDate = *code == 0
		result.Exists, err = tfPlanCheckAdd(stderr)
		if err != nil {
			return result, err
		}

		if result.Completed {
			_, err := c.loadStateFromWorkspace(true)
			if err != nil {
				return result, err
			}
			return result, nil
		}
	}
	// try to save the given state
	if err := c.storeStateInWorkspace(); err != nil {
		return result, err
	}
	// now try to refresh the resource
	// TODO(aru): do not use the shell pipeline below
	return result, c.runOperation("sh", func(c *Client, stdout, _ string) error {
		return c.storeLogLines(stdout)
	}, "-c", fmt.Sprintf("%s apply -refresh-only -auto-approve -input=false && "+
		"%s plan -detailed-exitcode -input=false", pathTerraform, pathTerraform))
}

// retruns true if no resource is to be added according to the plan
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

func (c *Client) Observe(id string) (ObserveResult, error) {
	if id == "" && len(c.state.tfState) == 0 {
		return ObserveResult{}, errors.New(errNoID)
	}
	if len(c.state.tfState) == 0 {
		return c.importResource(id)
	}
	return c.observe()
}

func (c *Client) importResource(id string) (ObserveResult, error) {
	result := ObserveResult{}
	initLockExists, stateLockExists, err := c.initOperation()
	if err != nil {
		return result, err
	}
	if !initLockExists || stateLockExists {
		return result, nil
	}
	code, _, err := c.getExitCode()
	// if store file is not found yet, probably because
	// process did not complete or even start, have the
	// flow continue
	if err != nil && !os.IsNotExist(errors.Cause(err)) {
		return result, err
	}
	result.Completed = err == nil
	if result.Completed && *code != 0 {
		// fixme(aru): if the resource does not exist
		// we should return no error here
		// if import failed for another reason
		// we should be returning an error
		// TODO(aru): provide detailed error message
		// from Terraform CLI
		return result, errors.New(errImport)
	}
	if result.Completed && *code == 0 {
		result.Exists = true
		// TODO(aru): we should discuss this: the assumption is that
		// MR should be late-initialized matching the observed state
		result.UpToDate = true
		ok, err := c.loadStateFromWorkspace(true)
		if err != nil {
			return result, err
		}
		// !ok is something we don't expect
		if !ok {
			return result, errors.New(errNoState)
		}
		// then refreshed state is in client cache
		return result, nil
	}
	// remove any existing state file because
	// we might be reusing a dirty workspace
	if err := os.RemoveAll(filepath.Join(c.wsPath, fileState)); err != nil {
		return result, errors.Wrap(err, errImport)
	}
	// now try to refresh the resource
	return result, c.runOperation(pathTerraform, func(c *Client, _, _ string) error {
		return c.storeExitCode()
	}, "import", "-input=false", c.resource.GetAddress(), id)
}

func (c *Client) storeExitCode() error {
	ps := c.pInfo.GetProcessState()
	if ps == nil {
		return errors.New(errNoProcessState)
	}
	return errors.Wrap(
		ioutil.WriteFile(filepath.Join(c.wsPath, fileStore), []byte(strconv.Itoa(ps.ExitCode())), 0644), errStore)
}

func (c *Client) storeLogLines(log string) error {
	ps := c.pInfo.GetProcessState()
	if ps == nil {
		return errors.New(errNoProcessState)
	}
	return errors.Wrap(
		ioutil.WriteFile(filepath.Join(c.wsPath, fileStore),
			[]byte(fmt.Sprintf("%d\n%s", ps.ExitCode(), log)), 0644), errStore)
}

// returns the exit code stored and the string contents
func (c *Client) getExitCode() (*int, string, error) {
	storeFile := filepath.Join(c.wsPath, fileStore)
	defer func() {
		err := os.RemoveAll(storeFile)
		if err != nil {
			c.logger.log.Info("Failed to remove store file", "path", storeFile)
		}
	}()
	// TODO(aru): usual what if contents are too large to fit in memory
	// we do not expect large Terraform output
	buff, err := ioutil.ReadFile(storeFile)
	if err != nil {
		return nil, "", errors.Wrapf(err, errCheckExitCode)
	}
	storedCode, err := strconv.Atoi(strings.Split(string(buff), "\n")[0])
	if err != nil {
		return nil, "", errors.Wrap(err, errCheckExitCode)
	}
	return &storedCode, string(buff), nil
}

// TODO(aru): this type is probably a duplicate
type tfState struct {
	Resources []interface{} `json:"resources"`
}

// returns whether the resource has been removed from state
// assumes that there can at most be one resource in state
// we could also check for <resource type>.<resource name>
// in state
func (c *Client) isRemovedFromState() (bool, error) {
	s := c.state.tfState
	ok, err := c.loadStateFromWorkspace(false)
	if err != nil {
		return false, err
	}
	// if no existing state file, do not assume resource is removed
	// caller site is expected to restore the state from client
	if !ok {
		c.state.tfState = s
		return false, nil
	}
	// if state could be loaded from workspace, check resource count
	state := &tfState{}
	if err := json.Unmarshal(c.state.tfState, &state); err != nil {
		return false, errors.Wrap(err, errStateUnmarshall)
	}
	return len(state.Resources) == 0, nil
}

type storeResult func(c *Client, stdout, stderr string) error

func (c *Client) runOperation(command string, storeResult storeResult, args ...string) error {
	var err error
	c.pInfo, err = process.New(command, args, c.wsPath,
		true, true, false, c.logger.log)
	if err != nil {
		return errors.Wrapf(err, fmtErrOpRun, pathTerraform, args, c.wsPath)
	}
	c.pInfo.LogStdout()
	c.pInfo.LogStderr()
	go func() {
		logger := c.logger.log.WithValues("args", args, "executable", pathTerraform, "cwd", c.wsPath)
		logger.Info("Waiting for peocess to terminate gracefully.", "arg-from-process", c.pInfo.GetCmd().String())
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

// returns whether there is an active Terraform CLI operation in the workspace
func (c *Client) initOperation() (bool, bool, error) {
	// initialize the workspace, and
	// check if init lock & state lock exist, i.e., there is an ongoing Terraform CLI operation
	initLockExists, stateLockExists := false, false
	err := c.destroyOnError(func() error {
		var err error
		initLockExists, stateLockExists, err = c.initWorkspace()
		return err
	})
	// fixme(aru): what if Terraform CLI has crashed before having a chance to
	// remove the lock?
	if err == nil && !initLockExists {
		// then we need to call an init
		// fixme(aru): may need some sort of locking. Possibly we will run
		// multiple inits concurrently. init lock is put at the end of init.
		// may need a custom lock here.
		return initLockExists, stateLockExists, c.runOperation(pathTerraform, nil, "init", "-input=false")
	}
	return initLockExists, stateLockExists, err
}

// returns true if state file exists
// TODO(aru): differentiate not-found error
func (c *Client) loadStateFromWorkspace(errNoState bool) (bool, error) {
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

func (c *Client) storeStateInWorkspace() error {
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

func (c *Client) destroyOnError(f func() error) error {
	err := f()
	if err != nil {
		err = multierr.Combine(err, c.Destroy())
	}
	return err
}

// Destroy after a call to Destroy, please do not reuse the same handle
func (c *Client) Destroy() error {
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

// returns true, true if init, state lock exist, respectively
func (c *Client) initWorkspace() (bool, bool, error) {
	handle, err := c.getHandle()
	if err != nil {
		return false, false, errors.Wrap(err, errInitWorkspace)
	}
	c.wsPath = filepath.Join(os.TempDir(), prefixWSDir+handle)

	// check if the workspace already exists, i.e. there is an open operation
	ok, err := pathExists(c.wsPath, true)
	if err != nil {
		return false, false, err
	}
	initLockExists, stateLockExists := false, false
	if ok {
		stateLockExists, err = pathExists(filepath.Join(c.wsPath, fileStateLock), false)
		if err != nil {
			return false, false, err
		}

		initLockExists, err = pathExists(filepath.Join(c.wsPath, fileInitLock), false)
		if err != nil {
			return false, stateLockExists, err
		}
		// if workspace exists, check the state lock. If state lock exists, do not overwrite config
		if stateLockExists {
			return initLockExists, stateLockExists, nil // init lock or state lock exist, do not overwrite
		}
	}
	// workspace does not exist or no state lock file and no init lock file
	if err := os.MkdirAll(c.wsPath, 0755); err != nil {
		return initLockExists, stateLockExists, errors.Wrap(err, errInitWorkspace)
	}

	conf, err := c.generateTFConfiguration()
	if err != nil {
		return initLockExists, stateLockExists, errors.Wrap(err, errInitWorkspace)
	}
	return initLockExists, stateLockExists,
		errors.Wrap(ioutil.WriteFile(filepath.Join(c.wsPath, tplMain), conf, 0644), errInitWorkspace)
}

func (c *Client) getHandle() (string, error) {
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

func (c Client) generateTFConfiguration() ([]byte, error) {
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
