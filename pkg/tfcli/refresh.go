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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/multierr"

	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
)

const (
	// error messages
	errRefresh        = "failed to refresh the Terraform state"
	errNoID           = errRefresh + ": cannot deduce resource ID"
	errImport         = "failed to import resource"
	fmtErrRefreshExit = errRefresh + ": Terraform pipeline exited with code: %d"
	fmtErrImport      = errImport + ": %s"
)

// Refresh updates local state of the Cloud resource in a synchronous manner
func (c *client) Refresh(ctx context.Context, id string) (model.RefreshResult, error) {
	if id == "" && len(c.state.tfState) == 0 {
		return model.RefreshResult{}, errors.New(errNoID)
	}

	if _, err := c.initConfiguration(model.OperationRefresh, false); err != nil {
		return model.RefreshResult{}, err
	}

	var result model.RefreshResult
	var err error
	if len(c.state.tfState) == 0 {
		result, err = c.importResource(ctx, id)
	} else {
		result, err = c.observe(ctx)
	}
	return result, multierr.Combine(err, c.removeStateStore())
}

func (c *client) importResource(ctx context.Context, id string) (model.RefreshResult, error) {
	result := model.RefreshResult{}
	// as a precaution, remove any existing state file
	if err := os.RemoveAll(filepath.Join(c.wsPath, fileState)); err != nil {
		return result, errors.Wrap(err, errImport)
	}
	// now try to run the synchronous import pipeline
	if err := c.syncPipeline(ctx, true, pathTerraform, "import", "-input=false", c.resource.GetAddress(), id); err != nil {
		return model.RefreshResult{}, err
	}

	code := c.pInfo.GetProcessState().ExitCode()
	if code != 0 {
		stderr, err := c.pInfo.StderrAsString()
		if err != nil {
			return result, err
		}
		// if resource does not exist, return RefreshResult.Exists=false and nil error
		if strings.Contains(stderr, tfMsgNonExistentResource) {
			return result, nil
		}
		// if import failed for another reason
		// we should return an error
		return result, errors.Errorf(fmtErrImport, stderr)
	}

	result.Exists = true
	// the assumption is that MR should be late-initialized
	// matching the observed state
	result.UpToDate = true
	_, err := c.loadStateFromWorkspace(true)
	if err != nil {
		return result, err
	}
	result.State = c.state.tfState

	// then refreshed state is in client cache
	return result, nil
}

// observe checks whether the specified resource is up-to-date
// using a synchronous pipeline.
// RefreshResult.State is non-nil and holds the fresh Terraform state iff
// RefreshResult.Completed is true.
func (c *client) observe(ctx context.Context) (model.RefreshResult, error) {
	result := model.RefreshResult{}
	// try to save the given state
	if err := c.storeStateInWorkspace(); err != nil {
		return result, err
	}
	// now try to run the refresh pipeline synchronously
	if err := c.syncPipeline(ctx, true, "sh", "-c",
		fmt.Sprintf("%s apply -refresh-only -auto-approve -input=false && %s plan -detailed-exitcode -input=false",
			pathTerraform, pathTerraform)); err != nil {
		return result, err
	}
	code := c.pInfo.GetProcessState().ExitCode()
	stdout, err := c.pInfo.StdoutAsString()
	if err != nil {
		return result, err
	}
	if code != 0 && code != 2 {
		return result, errors.Errorf(fmtErrRefreshExit, code)
	}
	result.UpToDate = code == 0
	if !result.UpToDate {
		result.Exists, err = tfPlanCheckAdd(stdout)
		if err != nil {
			return result, err
		}
	} else {
		result.Exists = true
	}

	_, err = c.loadStateFromWorkspace(true)
	if err != nil {
		return result, err
	}
	result.State = c.state.tfState
	return result, nil
}
