package migration

import (
	"fmt"
	"os"

	"github.com/pkg/errors"
	"k8s.io/utils/exec"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
)

type ForkExecutor struct {
	executor exec.Interface
	logger   logging.Logger
}

// ForkExecutorOption allows you to configure ForkExecutor objects.
type ForkExecutorOption func(executor *ForkExecutor)

// WithLogger sets the logger of ForkExecutor.
func WithLogger(l logging.Logger) ForkExecutorOption {
	return func(e *ForkExecutor) {
		e.logger = l
	}
}

// WithExecutor sets the executor of ForkExecutor.
func WithExecutor(e exec.Interface) ForkExecutorOption {
	return func(fe *ForkExecutor) {
		fe.executor = e
	}
}

// NewForkExecutor returns a new ForkExecutor with executor
func NewForkExecutor(opts ...ForkExecutorOption) *ForkExecutor {
	fe := &ForkExecutor{
		logger: logging.NewNopLogger(),
	}
	for _, f := range opts {
		f(fe)
	}
	return fe
}

func (f ForkExecutor) Init(config any) error {
	return nil
}

func (f ForkExecutor) Step(s Step, ctx any) (any, error) {
	if s.Type != StepTypeExec {
		return nil, errors.Wrap(NewUnsupportedStepTypeError(s), "expected step type is Exec")
	}

	cmd := f.executor.Command(s.Exec.Command, s.Exec.Args...)
	cmd.SetEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("could not execute step %s", s.Name))
	}

	return nil, nil
}

func (f ForkExecutor) Destroy() error {
	return nil
}
