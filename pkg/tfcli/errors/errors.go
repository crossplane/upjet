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

package errors

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
)

const (
	fmtErrOperationInProgress = "%s operation is in progress"
	fmtErrPipelineInProgress  = "pipeline in state: %s"
)

// OperationInProgressError is an error indicating that there is an ongoing
// operation which prevents starting a new one. While an operation
// is still in-progress, a new one is not allowed to be started
// until the active one completes and its results are successfully
// retrieved.
type OperationInProgressError struct {
	op model.OperationType
}

// Error returns the associated error string
func (o OperationInProgressError) Error() string {
	return fmt.Sprintf(fmtErrOperationInProgress, o.op.String())
}

// GetOperation returns the OperationType that is in progress
func (o OperationInProgressError) GetOperation() model.OperationType {
	return o.op
}

// NewOperationInProgressError initializes a new OperationInProgressError
// of the specified type
func NewOperationInProgressError(opType model.OperationType) error {
	return OperationInProgressError{
		op: opType,
	}
}

// IsApplying returns true if the specified error represents
// an OperationInProgressError for an apply operation.
func IsApplying(err error) bool {
	return IsOperationInProgress(err, model.OperationApply)
}

// IsDestroying returns true if the specified error represents
// an OperationInProgressError for a destroy operation.
func IsDestroying(err error) bool {
	return IsOperationInProgress(err, model.OperationDestroy)
}

// Is returns whether the specified error is an OperationInProgressError
// regardless of the operation type that's in-progress
func (o OperationInProgressError) Is(err error) bool {
	_, ok := err.(OperationInProgressError)
	return ok
}

// IsOperationInProgress returns true if the specified error represents an
// OperationInProgressError with the specified operation type.
// If opType is nil, then no operation type check is done.
func IsOperationInProgress(err error, opType model.OperationType) bool {
	opErr := &OperationInProgressError{}
	return errors.As(err, opErr) && (opType == opErr.GetOperation())
}

// PipelineState represents a pipeline of Terraform commands
type PipelineState string

const (
	// PipelineNotStarted states a not-yet-started command pipeline
	PipelineNotStarted PipelineState = "Asynchronous Terraform pipeline not started yet"
	// PipelineStateLocked states a running pipeline
	PipelineStateLocked PipelineState = "Terraform CLI is still running"
	// PipelineStateNoStore states an async pipeline which has not produced a result yet
	PipelineStateNoStore PipelineState = "Result is not available yet"
)

// PipelineInProgressError indicates that while an asynchronous
// Terraform pipeline is still in-progress, an attempt has been
// made to retrieve its results.
type PipelineInProgressError struct {
	pipelineState PipelineState
}

// NewPipelineInProgressError initializes a new PipelineInProgressError
// for the specified pipeline state.
func NewPipelineInProgressError(state PipelineState) error {
	return PipelineInProgressError{
		pipelineState: state,
	}
}

// Error returns the associated error string
func (p PipelineInProgressError) Error() string {
	return fmt.Sprintf(fmtErrPipelineInProgress, p.pipelineState)
}

// Is returns whether the specified error is a PipelineInProgressError
// regardless of the pipeline state
func (p PipelineInProgressError) Is(err error) bool {
	_, ok := err.(PipelineInProgressError)
	return ok
}

// IsPipelineInProgress returns true and the observed pipeline state
// if the specified error represents an PipelineInProgressError.
func IsPipelineInProgress(err error) (PipelineState, bool) {
	stateErr := &PipelineInProgressError{}
	if errors.As(err, stateErr) {
		return stateErr.pipelineState, true
	}
	return "invalid", false
}
