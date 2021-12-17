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

package terraform

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	k8sExec "k8s.io/utils/exec"
	testingexec "k8s.io/utils/exec/testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"

	"github.com/crossplane-contrib/terrajet/pkg/resource/json"
	tferrors "github.com/crossplane-contrib/terrajet/pkg/terraform/errors"
)

var (
	testType              = "very-cool-type"
	applyType             = "apply"
	lineage               = "very-cool-lineage"
	terraformVersion      = "1.0.10"
	version               = 1
	serial                = 3
	directory             = "random-dir/"
	changeSummaryAdd      = `{"@level":"info","@message":"Plan: 1 to add, 0 to change, 0 to destroy.","@module":"terraform.ui","@timestamp":"0000-00-00T00:00:00.000000+03:00","changes":{"add":1,"change":0,"remove":0,"operation":"plan"},"type":"change_summary"}`
	changeSummaryUpdate   = `{"@level":"info","@message":"Plan: 0 to add, 1 to change, 0 to destroy.","@module":"terraform.ui","@timestamp":"0000-00-00T00:00:00.000000+03:00","changes":{"add":0,"change":1,"remove":0,"operation":"plan"},"type":"change_summary"}`
	changeSummaryNoAction = `{"@level":"info","@message":"Plan: 0 to add, 0 to change, 0 to destroy.","@module":"terraform.ui","@timestamp":"0000-00-00T00:00:00.000000+03:00","changes":{"add":0,"change":0,"remove":0,"operation":"plan"},"type":"change_summary"}`

	state = &json.StateV4{
		Version:          uint64(version),
		TerraformVersion: terraformVersion,
		Serial:           uint64(serial),
		Lineage:          lineage,
		RootOutputs:      map[string]json.OutputStateV4{},
		Resources:        []json.ResourceStateV4{},
	}

	now = time.Now()

	fs = afero.Afero{
		Fs: afero.NewMemMapFs(),
	}

	tfstate = `{"version": 1,"terraform_version": "1.0.10","serial": 3,"lineage": "very-cool-lineage","outputs": {},"resources": []}`
)

func newFakeExec(stdOut string, err error) *testingexec.FakeExec {
	return &testingexec.FakeExec{
		CommandScript: []testingexec.FakeCommandAction{
			func(_ string, _ ...string) k8sExec.Cmd {
				return &testingexec.FakeCmd{
					CombinedOutputScript: []testingexec.FakeAction{
						func() ([]byte, []byte, error) {
							return []byte(stdOut), nil, err
						},
					},
				}
			},
		},
	}
}

func TestWorkspaceApply(t *testing.T) {
	type args struct {
		w *Workspace
	}
	type want struct {
		r   ApplyResult
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Running": {
			args: args{
				w: NewWorkspace(directory, WithLastOperation(&Operation{Type: testType, startTime: &now, endTime: nil}),
					WithAferoFs(fs)),
			},
			want: want{
				err: errors.Errorf("%s operation that started at %s is still running", testType, now.String()),
			},
		},
		"Success": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true}), WithAferoFs(fs)),
			},
			want: want{
				r: ApplyResult{
					State: state,
				},
			},
		},
		"Failure": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(errBoom.Error(), errBoom)), WithAferoFs(fs)),
			},
			want: want{
				err: tferrors.NewApplyFailed([]byte(errBoom.Error())),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.w.fs.WriteFile(directory+"terraform.tfstate", []byte(tfstate), 777); err != nil {
				panic(err)
			}
			r, err := tc.w.Apply(context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nApply(...): -want error, +got error:\n%s", name, diff)
			}
			if diff := cmp.Diff(tc.want.r, r, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nApply(...): -want error, +got error:\n%s", name, diff)
			}
		})
	}
}

func TestWorkspaceDestroy(t *testing.T) {
	type args struct {
		w *Workspace
	}
	type want struct {
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Running": {
			args: args{
				w: NewWorkspace(directory, WithLastOperation(&Operation{Type: testType, startTime: &now, endTime: nil})),
			},
			want: want{
				err: errors.Errorf("%s operation that started at %s is still running", testType, now.String()),
			},
		},
		"Success": {
			args: args{
				w: NewWorkspace(
					directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true})),
			},
			want: want{},
		},
		"Failure": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(errBoom.Error(), errBoom))),
			},
			want: want{
				err: tferrors.NewDestroyFailed([]byte(errBoom.Error())),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.w.Destroy(context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nDestroy(...): -want error, +got error:\n%s", name, diff)
			}
		})
	}
}

func TestWorkspaceRefresh(t *testing.T) {
	type args struct {
		w *Workspace
	}
	type want struct {
		r   RefreshResult
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Running": {
			args: args{
				w: NewWorkspace(directory, WithLastOperation(&Operation{Type: applyType, startTime: &now, endTime: nil}),
					WithAferoFs(fs)),
			},
			want: want{
				r: RefreshResult{
					IsApplying: true,
				},
			},
		},
		"Success": {
			args: args{
				w: NewWorkspace(
					directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true}), WithAferoFs(fs)),
			},
			want: want{
				r: RefreshResult{
					State: state,
				},
			},
		},
		"Failure": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(errBoom.Error(), errBoom)), WithAferoFs(fs)),
			},
			want: want{
				err: tferrors.NewRefreshFailed([]byte(errBoom.Error())),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.w.fs.WriteFile(directory+"terraform.tfstate", []byte(tfstate), 777); err != nil {
				panic(err)
			}
			r, err := tc.w.Refresh(context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nRefresh(...): -want error, +got error:\n%s", name, diff)
			}
			if diff := cmp.Diff(tc.want.r, r, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nRefresh(...): -want error, +got error:\n%s", name, diff)
			}
		})
	}
}

func TestWorkspacePlan(t *testing.T) {
	type args struct {
		w *Workspace
	}
	type want struct {
		r   PlanResult
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Running": {
			args: args{
				w: NewWorkspace(directory, WithLastOperation(&Operation{Type: testType, startTime: &now, endTime: nil})),
			},
			want: want{
				err: errors.Errorf("%s operation that started at %s is still running", testType, now.String()),
			},
		},
		"NoChangeSummary": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true})),
			},
			want: want{
				err: errors.Errorf("cannot find the change summary line in plan log: "),
			},
		},
		"ChangeSummaryAdd": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(changeSummaryAdd, nil))),
			},
			want: want{
				r: PlanResult{
					Exists:   false,
					UpToDate: true,
				},
			},
		},
		"ChangeSummaryUpdate": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(changeSummaryUpdate, nil))),
			},
			want: want{
				r: PlanResult{
					Exists:   true,
					UpToDate: false,
				},
			},
		},
		"ChangeSummaryNoAction": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(changeSummaryNoAction, nil))),
			},
			want: want{
				r: PlanResult{
					Exists:   true,
					UpToDate: true,
				},
			},
		},
		"Failure": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(errBoom.Error(), errBoom))),
			},
			want: want{
				err: tferrors.NewPlanFailed([]byte(errBoom.Error())),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := tc.w.Plan(context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nPlan(...): -want error, +got error:\n%s", name, diff)
			}
			if diff := cmp.Diff(tc.want.r, r, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nPlan(...): -want error, +got error:\n%s", name, diff)
			}
		})
	}
}

func TestWorkspaceApplyAsync(t *testing.T) {
	calls := make(chan bool)

	type args struct {
		w *Workspace
		c CallbackFn
	}
	type want struct {
		called bool
		err    error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Running": {
			args: args{
				w: NewWorkspace(directory, WithLastOperation(&Operation{Type: testType, startTime: &now, endTime: nil})),
			},
			want: want{
				err: errors.Errorf("%s operation that started at %s is still running", testType, now.String()),
			},
		},
		"Callback": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true})),
				c: func(err error, ctx context.Context) error {
					calls <- true
					return nil
				},
			},
			want: want{
				called: true,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.w.ApplyAsync(tc.c)
			if t.Name() == "TestWorkspaceApplyAsync/Callback" {
				called := <-calls

				if diff := cmp.Diff(tc.want.called, called, test.EquateErrors()); diff != "" {
					t.Errorf("\n%s\nApplyAsync(...): -want error, +got error:\n%s", name, diff)
				}
			}
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nApplyAsync(...): -want error, +got error:\n%s", name, diff)
			}
		})
	}
}

func TestWorkspaceDestroyAsync(t *testing.T) {
	calls := make(chan bool)

	type args struct {
		w *Workspace
		c CallbackFn
	}
	type want struct {
		called bool
		err    error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Running": {
			args: args{
				w: NewWorkspace(directory, WithLastOperation(&Operation{Type: testType, startTime: &now, endTime: nil})),
			},
			want: want{
				err: errors.Errorf("%s operation that started at %s is still running", testType, now.String()),
			},
		},
		"Callback": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true})),
				c: func(err error, ctx context.Context) error {
					calls <- true
					return nil
				},
			},
			want: want{
				called: true,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.w.DestroyAsync(tc.c)
			if t.Name() == "TestWorkspaceDestroyAsync/Callback" {
				called := <-calls

				if diff := cmp.Diff(tc.want.called, called, test.EquateErrors()); diff != "" {
					t.Errorf("\n%s\nDestroyAsync(...): -want error, +got error:\n%s", name, diff)
				}
			}
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nDestroyAsync(...): -want error, +got error:\n%s", name, diff)
			}
		})
	}
}
