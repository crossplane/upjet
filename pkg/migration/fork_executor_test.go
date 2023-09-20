// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	k8sExec "k8s.io/utils/exec"
	testingexec "k8s.io/utils/exec/testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
)

var (
	backupManagedStep = Step{
		Name: "backup-managed-resources",
		Type: StepTypeExec,
		Exec: &ExecStep{
			Command: "sh",
			Args:    []string{"-c", "kubectl get managed -o yaml"},
		},
	}

	wrongCommand = Step{
		Name: "wrong-command",
		Type: StepTypeExec,
		Exec: &ExecStep{
			Command: "sh",
			Args:    []string{"-c", "nosuchcommand"},
		},
	}

	wrongStepType = Step{
		Name: "wrong-step-type",
		Type: StepTypeDelete,
	}
)

var errorWrongCommand = errors.New("exit status 127")

func newFakeExec(err error) *testingexec.FakeExec {
	return &testingexec.FakeExec{
		CommandScript: []testingexec.FakeCommandAction{
			func(_ string, _ ...string) k8sExec.Cmd {
				return &testingexec.FakeCmd{
					CombinedOutputScript: []testingexec.FakeAction{
						func() ([]byte, []byte, error) {
							return nil, nil, err
						},
					},
				}
			},
		},
	}
}

func TestForkExecutorStep(t *testing.T) {
	type args struct {
		step     Step
		fakeExec *testingexec.FakeExec
	}
	type want struct {
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Success": {
			args: args{
				step:     backupManagedStep,
				fakeExec: &testingexec.FakeExec{DisableScripts: true},
			},
			want: want{
				nil,
			},
		},
		"Failure": {
			args: args{
				step:     wrongCommand,
				fakeExec: newFakeExec(errorWrongCommand),
			},
			want: want{
				errors.Wrap(errorWrongCommand, `failed to execute the step "wrong-command": failed to execute command`),
			},
		},
		"WrongStepType": {
			args: args{
				step: wrongStepType,
			},
			want: want{
				errors.Wrap(NewUnsupportedStepTypeError(wrongStepType), `step type should be Exec or step's manualExecution should be non-empty`),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fe := NewForkExecutor(WithExecutor(tc.fakeExec))
			err := fe.Step(tc.step, nil)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nStep(...): -want error, +got error:\n%s", name, diff)
			}
		})
	}
}
