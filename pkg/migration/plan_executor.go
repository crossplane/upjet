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

type PlanExecutor struct {
	executors []Executor
	plan      Plan
}

// NewPlanExecutor returns a new plan executor for executing the steps
// of a migration plan.
func NewPlanExecutor(plan Plan, executors ...Executor) *PlanExecutor {
	return &PlanExecutor{
		executors: executors,
		plan:      plan,
	}
}

func (pe *PlanExecutor) Execute() error {
	for i, s := range pe.plan.Spec.Steps {
		// TODO: support multiple executors when multiple of them are available
		if _, err := pe.executors[0].Step(s, nil); err != nil {
			return errors.Wrapf(err, "failed to execute step %q at index %d", s.Name, i)
		}
	}
	return nil
}
