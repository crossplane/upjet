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
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/multierr"

	"github.com/crossplane-contrib/terrajet/pkg/json"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
)

const (
	// error messages
	errRefresh      = "failed to refresh the Terraform state"
	errNoID         = errRefresh + ": cannot deduce resource ID"
	errImport       = "failed to import resource"
	errFmtObserve   = "observe exited with code %d with the following error: %s"
	errFmtImport    = errImport + ": %s"
	errFmtNoPlan    = "plan line not found in Terraform CLI output: %s"
	errFmtParsePlan = "failed to parse Terraform plan output: %s"
)

// Refresh updates local state of the Cloud resource in a synchronous manner
func (c *Client) Refresh(ctx context.Context, id string) (model.RefreshResult, error) {
	if id == "" && len(c.tfState) == 0 {
		return model.RefreshResult{}, errors.New(errNoID)
	}

	if _, err := c.initConfiguration(model.OperationRefresh, false); err != nil {
		return model.RefreshResult{}, err
	}

	var result model.RefreshResult
	var err error
	if len(c.tfState) == 0 {
		result, err = c.importResource(ctx, id)
	} else {
		result, err = c.observe(ctx)
	}
	return result, multierr.Combine(err, c.removeStateStore())
}

func (c *Client) importResource(ctx context.Context, id string) (model.RefreshResult, error) {
	result := model.RefreshResult{}
	// as a precaution, remove any existing state file
	if err := c.fs.RemoveAll(filepath.Join(c.wsPath, fileState)); err != nil {
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
		return result, errors.Errorf(errFmtImport, stderr)
	}

	result.Exists = true
	// the assumption is that MR should be late-initialized
	// matching the observed state
	result.UpToDate = true
	if err := c.loadStateFromWorkspace(); err != nil {
		return result, err
	}
	result.State = c.tfState

	// then refreshed state is in Client cache
	return result, nil
}

// observe checks whether the specified resource is up-to-date
// using a synchronous pipeline.
// RefreshResult.State is non-nil and holds the fresh Terraform state iff
// RefreshResult.Completed is true.
func (c *Client) observe(ctx context.Context) (model.RefreshResult, error) {
	// try to save the given state
	if err := c.storeStateInWorkspace(); err != nil {
		return model.RefreshResult{}, err
	}
	// now try to run the refresh pipeline synchronously
	if err := c.syncPipeline(ctx, true, "sh", "-c",
		fmt.Sprintf("%s apply -refresh-only -auto-approve -input=false && %s plan -detailed-exitcode -refresh=false -input=false -json",
			pathTerraform, pathTerraform)); err != nil {
		return model.RefreshResult{}, err
	}
	code := c.pInfo.GetProcessState().ExitCode()
	stdout, err := c.pInfo.StdoutAsString()
	if err != nil {
		return model.RefreshResult{}, err
	}
	stderr, err := c.pInfo.StderrAsString()
	if err != nil {
		return model.RefreshResult{}, err
	}
	// Code 2 means that we need to make a change and that's a valid case for
	// us, so we don't treat it as an error case.
	if code != 0 && code != 2 {
		return model.RefreshResult{}, errors.Errorf(errFmtObserve, code, stderr)
	}
	needAdd, needChange, err := parsePlan(stdout)
	if err != nil {
		return model.RefreshResult{}, errors.Wrapf(err, errFmtParsePlan, stdout)
	}
	if err := c.loadStateFromWorkspace(); err != nil {
		return model.RefreshResult{}, err
	}
	return model.RefreshResult{
		Exists:   !needAdd,
		UpToDate: !needChange,
		State:    c.tfState,
	}, nil
}

func parsePlan(log string) (needAdd bool, needChange bool, err error) {
	line := ""
	for _, l := range strings.Split(log, "\n") {
		if strings.Contains(l, `"type":"change_summary"`) {
			line = l
			break
		}
	}
	if line == "" {
		return false, false, errors.Errorf(errFmtNoPlan, log)
	}
	type plan struct {
		Changes struct {
			Add    float64 `json:"add,omitempty"`
			Change float64 `json:"change,omitempty"`
		} `json:"changes,omitempty"`
	}
	p := &plan{}
	if err := json.JSParser.Unmarshal([]byte(line), p); err != nil {
		return false, false, errors.Wrap(err, "cannot unmarshal change summary json")
	}
	return p.Changes.Add > 0, p.Changes.Change > 0, nil
}
