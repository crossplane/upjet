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

	"github.com/pkg/errors"

	cliErrors "github.com/crossplane-contrib/terrajet/pkg/tfcli/errors"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/types"
)

const (
	fmtErrApply = "failed to apply Terraform configuration: %s"
)

// Apply attempts to provision the resource.
// ApplyResult.Completed is false if the operation has not yet been completed.
// ApplyResult.State is non-nil and holds the fresh Terraform state iff
// ApplyResult.Completed is true.
func (c *client) Apply(_ context.Context) (types.ApplyResult, error) {
	code, tfLog, err := c.parsePipelineResult(types.OperationApply)
	pipelineState, ok := cliErrors.IsPipelineInProgress(err)
	if !ok && err != nil {
		return types.ApplyResult{}, err
	}
	// if no pipeline state error and code is 0,
	// then pipeline has completed successfully
	if err == nil {
		switch *code {
		case 0:
			// then check the state and try to load it if available
			_, err := c.loadStateFromWorkspace(true)
			if err != nil {
				return types.ApplyResult{}, err
			}
			// and it has been stored
			return types.ApplyResult{
				Completed: true,
				State:     c.state.tfState,
			}, nil

		default:
			return types.ApplyResult{
				Completed: true,
			}, errors.Errorf(fmtErrApply, tfLog)
		}
	}
	// then check pipeline state. If pipeline is already started we need to wait.
	if pipelineState != cliErrors.PipelineNotStarted {
		return types.ApplyResult{}, nil
	}
	// if pipeline is not started yet, try to start it
	return types.ApplyResult{}, c.asyncPipeline(pathTerraform, func(c *client, stdout, _ string) error {
		return c.storePipelineResult(stdout)
	}, "apply", "-auto-approve", "-input=false")
}
