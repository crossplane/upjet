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
	KeyContextCombinedOutput = "combinedOutput"
)

type PlanExecutor struct {
	executors []Executor
	plan      Plan
	callback  ExecutorCallback
}

type Action int

const (
	ActionContinue Action = iota
	ActionSkip
	ActionCancel
	ActionRepeat
)

type CallbackResult struct {
	Action Action
}

type PlanExecutorOption func(executor *PlanExecutor)

func WithExecutorCallback(cb ExecutorCallback) PlanExecutorOption {
	return func(pe *PlanExecutor) {
		pe.callback = cb
	}
}

type ExecutorCallback interface {
	StepToExecute(s Step, index int) CallbackResult
	StepSucceeded(s Step, index int, buff []byte) CallbackResult
	StepFailed(s Step, index int, buff []byte, err error) CallbackResult
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

func (pe *PlanExecutor) Execute() error {
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
		buff, _ := ctx[KeyContextCombinedOutput].([]byte)
		if err != nil {
			if pe.callback != nil {
				r = pe.callback.StepFailed(pe.plan.Spec.Steps[i], i, buff, err)
			}
		} else if pe.callback != nil {
			r = pe.callback.StepSucceeded(pe.plan.Spec.Steps[i], i, buff)
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
