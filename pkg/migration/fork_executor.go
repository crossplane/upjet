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

import (
	"os"

	"github.com/pkg/errors"
	"k8s.io/utils/exec"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
)

const (
	errForkExecutorNotSupported = "step type should be Exec or step's manualExecution should be non-empty"
	errStepFailedFmt            = "failed to execute the step %q"
)

// forkExecutor executes Exec steps or steps with the `manualExecution` hints
// by forking processes.
type forkExecutor struct {
	executor exec.Interface
	logger   logging.Logger
	cwd      string
}

// ForkExecutorOption allows you to configure forkExecutor objects.
type ForkExecutorOption func(executor *forkExecutor)

// WithLogger sets the logger of forkExecutor.
func WithLogger(l logging.Logger) ForkExecutorOption {
	return func(e *forkExecutor) {
		e.logger = l
	}
}

// WithExecutor sets the executor of ForkExecutor.
func WithExecutor(e exec.Interface) ForkExecutorOption {
	return func(fe *forkExecutor) {
		fe.executor = e
	}
}

// WithWorkingDir sets the current working directory for the executor.
func WithWorkingDir(dir string) ForkExecutorOption {
	return func(e *forkExecutor) {
		e.cwd = dir
	}
}

// NewForkExecutor returns a new fork executor using a process forker.
func NewForkExecutor(opts ...ForkExecutorOption) Executor {
	fe := &forkExecutor{
		executor: exec.New(),
		logger:   logging.NewNopLogger(),
	}
	for _, f := range opts {
		f(fe)
	}
	return fe
}

func (f forkExecutor) Init(_ any) error {
	return nil
}

func (f forkExecutor) Step(s Step, _ any) (any, error) {
	var cmd exec.Cmd
	switch {
	case s.Type == StepTypeExec:
		return nil, errors.Wrapf(f.exec(f.executor.Command(s.Exec.Command, s.Exec.Args...)), errStepFailedFmt, s.Name)
	// TODO: we had better have separate executors to handle the other types of
	// steps
	case len(s.ManualExecution) != 0:
		for _, c := range s.ManualExecution {
			cmd = f.executor.Command("sh", "-c", c)
			if err := f.exec(cmd); err != nil {
				return nil, errors.Wrapf(f.exec(cmd), errStepFailedFmt, s.Name)
			}
		}
		return nil, nil
	default:
		return nil, errors.Wrap(NewUnsupportedStepTypeError(s), errForkExecutorNotSupported)
	}
}

func (f forkExecutor) exec(cmd exec.Cmd) error {
	cmd.SetEnv(os.Environ())
	if f.cwd != "" {
		cmd.SetDir(f.cwd)
	}
	buff, err := cmd.CombinedOutput()
	logMsg := "Successfully executed command"
	if err != nil {
		logMsg = "Command execution failed"
	}
	f.logger.Info(logMsg, "output", string(buff))
	return errors.Wrapf(err, "failed to execute command")
}

func (f forkExecutor) Destroy() error {
	return nil
}
