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

	tferrors "github.com/crossplane-contrib/terrajet/pkg/tfcli/errors"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
)

const (
	fmtErrApply = "failed to apply Terraform configuration: %s"
)

// Apply attempts to provision the resource.
// ApplyResult.Completed is false if the operation has not yet been completed.
// ApplyResult.State is non-nil and holds the fresh Terraform state iff
// ApplyResult.Completed is true.
func (c *Client) Apply(_ context.Context) (model.ApplyResult, error) {
	code, tfLog, err := c.parsePipelineResult(model.OperationApply)
	pipelineState, ok := tferrors.IsPipelineInProgress(err)
	if !ok && err != nil {
		return model.ApplyResult{}, err
	}
	// if no pipeline state error and code is 0,
	// then pipeline has completed successfully
	if err == nil {
		switch *code {
		case 0:
			st, err := c.loadStateFromWorkspace()
			if err != nil {
				return model.ApplyResult{}, err
			}
			return model.ApplyResult{Completed: true, State: st}, nil
		default:
			return model.ApplyResult{Completed: true}, errors.Errorf(fmtErrApply, tfLog)
		}
	}
	// then check pipeline state. If pipeline is already started we need to wait.
	if pipelineState != tferrors.PipelineNotStarted {
		return model.ApplyResult{}, nil
	}
	// if pipeline is not started yet, try to start it
	return model.ApplyResult{}, c.asyncPipeline(pathTerraform, func(c *Client, stdout, _ string) error {
		return c.storePipelineResult(stdout)
	}, "apply", "-auto-approve", "-input=false")
}
