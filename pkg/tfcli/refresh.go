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
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/multierr"

	"github.com/crossplane-contrib/terrajet/pkg/json"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
)

const (
	errFmtObserve   = "observe failed with the following error: %s"
	errFmtNoPlan    = "plan line not found in Terraform CLI output: %s"
	errFmtParsePlan = "failed to parse Terraform plan output: %s"
)

// Refresh updates local state of the Cloud resource in a synchronous manner
func (c *Client) Refresh(ctx context.Context) (model.RefreshResult, error) {
	if _, err := c.initConfiguration(model.OperationRefresh, false); err != nil {
		return model.RefreshResult{}, err
	}
	// Prepare the workspace for a refresh operation. The reason we run refresh
	// in all cases including import is that the state that comes with "import"
	// may not include all information we have. So, we first write everything
	// into state file, which could include additional parameters that user has
	// but not covered by import, and then run refresh. Otherwise, users would
	// first give only external name, let import run, and then fill the gaps
	// because we don't have a clear signal like checking tfstate existence to
	// understand whether import or refresh needs to be run. It's safer to run
	// the one that can work in all those cases.
	if err := c.storeStateInWorkspace(); err != nil {
		return model.RefreshResult{}, err
	}
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
	st, err := c.loadStateFromWorkspace()
	if err != nil {
		return model.RefreshResult{}, err
	}
	return model.RefreshResult{
		Exists:   !needAdd,
		UpToDate: !needChange,
		State:    st,
	}, multierr.Combine(err, c.removeStateStore())
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
