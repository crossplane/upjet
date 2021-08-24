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
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
)

const (
	fmtErrDestroy = "failed to destroy resource: %s"
)

// Destroy attempts to delete the resource.
// DestroyResult.Completed is false if the operation has not yet been completed.
func (c *client) Destroy(_ context.Context) (model.DestroyResult, error) {
	code, tfLog, err := c.parsePipelineResult(model.OperationDestroy)
	pipelineState, ok := cliErrors.IsPipelineInProgress(err)
	if !ok && err != nil {
		return model.DestroyResult{}, err
	}
	// if no pipeline state error and code is 0,
	// then pipeline has completed successfully
	if err == nil {
		switch *code {
		case 0:
			return model.DestroyResult{
				Completed: true,
			}, nil

		default:
			return model.DestroyResult{
				Completed: true,
			}, errors.Errorf(fmtErrDestroy, tfLog)
		}
	}
	// then check pipeline state. If pipeline is already started we need to wait.
	if pipelineState != cliErrors.PipelineNotStarted {
		return model.DestroyResult{}, nil
	}
	// if pipeline is not started yet, try to start it
	return model.DestroyResult{},
		c.asyncPipeline(pathTerraform, func(c *client, stdout, _ string) error {
			return c.storePipelineResult(stdout)
		}, "destroy", "-auto-approve", "-input=false")
}
