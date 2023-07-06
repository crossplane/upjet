// Copyright 2023 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package migration

import "github.com/pkg/errors"

const (
	// KeyContextDiagnostics is the executor step context key for
	// storing any extra diagnostics information from
	// the executor.
	KeyContextDiagnostics = "diagnostics"
)

// PlanExecutor drives the execution of a plan's steps and
// uses the configured `executors` to execute those steps.
type PlanExecutor struct {
	executors []Executor
	plan      Plan
	callback  ExecutorCallback
}

// Action represents an action to be taken by the PlanExecutor.
// An Action is dictated by a ExecutorCallback implementation
// to the PlanExecutor for each step.
type Action int

const (
	// ActionContinue tells the PlanExecutor to continue with the execution
	// of a Step.
	ActionContinue Action = iota
	// ActionSkip tells the PlanExecutor to skip the execution
	// of the current Step.
	ActionSkip
	// ActionCancel tells the PlanExecutor to stop executing
	// the Steps of a Plan.
	ActionCancel
	// ActionRepeat tells the PlanExecutor to repeat the execution
	// of the current Step.
	ActionRepeat
)

// CallbackResult is the type of a value returned from one of the callback
// methods of ExecutorCallback implementations.
type CallbackResult struct {
	Action Action
}

// PlanExecutorOption is a mutator function for setting an option of a
// PlanExecutor.
type PlanExecutorOption func(executor *PlanExecutor)

// WithExecutorCallback configures an ExecutorCallback for a PlanExecutor
// to be notified as the Plan's Step's are executed.
func WithExecutorCallback(cb ExecutorCallback) PlanExecutorOption {
	return func(pe *PlanExecutor) {
		pe.callback = cb
	}
}

// ExecutorCallback is the interface for the callback implementations
// to be notified while executing each Step of a migration Plan.
type ExecutorCallback interface {
	// StepToExecute is called just before a migration Plan's Step is executed.
	// Can be used to cancel the execution of the Plan, or to continue/skip
	// the Step's execution.
	StepToExecute(s Step, index int) CallbackResult
	// StepSucceeded is called after a migration Plan's Step is
	// successfully executed.
	// Can be used to cancel the execution of the Plan, or to
	// continue/skip/repeat the Step's execution.
	StepSucceeded(s Step, index int, diagnostics any) CallbackResult
	// StepFailed is called after a migration Plan's Step has
	// failed to execute.
	// Can be used to cancel the execution of the Plan, or to
	// continue/skip/repeat the Step's execution.
	StepFailed(s Step, index int, diagnostics any, err error) CallbackResult
}

// NewPlanExecutor returns a new plan executor for executing the steps
// of a migration plan.
func NewPlanExecutor(plan Plan, executors []Executor, opts ...PlanExecutorOption) *PlanExecutor {
	pe := &PlanExecutor{
		executors: executors,
		plan:      plan,
	}
	for _, o := range opts {
		o(pe)
	}
	return pe
}

func (pe *PlanExecutor) Execute() error { //nolint:gocyclo // easier to follow this way
	ctx := make(map[string]any)
	for i := 0; i < len(pe.plan.Spec.Steps); i++ {
		var r CallbackResult
		if pe.callback != nil {
			r = pe.callback.StepToExecute(pe.plan.Spec.Steps[i], i)
			switch r.Action {
			case ActionCancel:
				return nil
			case ActionSkip:
				continue
			case ActionContinue, ActionRepeat:
			}
		}

		err := pe.executors[0].Step(pe.plan.Spec.Steps[i], ctx)
		diag := ctx[KeyContextDiagnostics]
		if err != nil {
			if pe.callback != nil {
				r = pe.callback.StepFailed(pe.plan.Spec.Steps[i], i, diag, err)
			}
		} else if pe.callback != nil {
			r = pe.callback.StepSucceeded(pe.plan.Spec.Steps[i], i, diag)
		}

		switch r.Action {
		case ActionCancel:
			return errors.Wrapf(err, "failed to execute step %q at index %d", pe.plan.Spec.Steps[i].Name, i)
		case ActionContinue, ActionSkip:
			continue
		case ActionRepeat:
			i--
		}
	}
	return nil
}
