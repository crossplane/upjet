/*
Copyright 2021 Upbound Inc.
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

	"github.com/upbound/upjet/pkg/resource/json"
	tferrors "github.com/upbound/upjet/pkg/terraform/errors"
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
	filter                = `{"@level":"info","@message":"Terraform 1.2.1","@module":"terraform.ui","@timestamp":"2022-08-08T14:42:59.377073+03:00","terraform":"1.2.1","type":"version","ui":"1.0"}
{"@level":"error","@message":"Error: error configuring Terraform AWS Provider: error validating provider credentials: error calling sts:GetCallerIdentity: operation error STS: GetCallerIdentity, https response error StatusCode: 403, RequestID: *****, api error InvalidClientTokenId: The security token included in the request is invalid.","@module":"terraform.ui","@timestamp":"2022-08-08T14:43:00.808602+03:00","diagnostic":{"severity":"error","summary":"error configuring Terraform AWS Provider: error validating provider credentials: error calling sts:GetCallerIdentity: operation error STS: GetCallerIdentity, https response error StatusCode: 403, RequestID: *****, api error InvalidClientTokenId: The security token included in the request is invalid.","detail":"","address":"provider[\"registry.terraform.io/hashicorp/aws\"]","range":{"filename":"main.tf.json","start":{"line":1,"column":173,"byte":172},"end":{"line":1,"column":174,"byte":173}},"snippet":{"context":"provider.aws","code":"{\"provider\":{\"aws\":{\"access_key\":\"*****\",\"region\":\"us-east-1\",\"secret_key\":\"/*****\",\"skip_region_validation\":true,\"token\":\"\"}},\"resource\":{\"aws_iam_user\":{\"sample-user\":{\"lifecycle\":{\"prevent_destroy\":true},\"name\":\"sample-user\",\"tags\":{\"crossplane-kind\":\"user.iam.aws.upbound.io\",\"crossplane-name\":\"sample-user\",\"crossplane-providerconfig\":\"default\"}}}},\"terraform\":{\"required_providers\":{\"aws\":{\"source\":\"hashicorp/aws\",\"version\":\"4.15.1\"}}}}","start_line":1,"highlight_start_offset":172,"highlight_end_offset":173,"values":[]}},"type":"diagnostic"}`

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

	filterFn = func(s string) string {
		return ""
	}
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
					WithAferoFs(fs), WithFilterFn(filterFn)),
			},
			want: want{
				err: errors.Errorf("%s operation that started at %s is still running", testType, now.String()),
			},
		},
		"Success": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true}), WithAferoFs(fs),
					WithFilterFn(filterFn)),
			},
			want: want{
				r: ApplyResult{
					State: state,
				},
			},
		},
		"Failure": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(errBoom.Error(), errBoom)), WithAferoFs(fs),
					WithFilterFn(filterFn)),
			},
			want: want{
				err: tferrors.NewApplyFailed([]byte(errBoom.Error())),
			},
		},
		"Filter": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(filter, errors.New(filter))), WithAferoFs(fs),
					WithFilterFn(filterFn)),
			},
			want: want{
				err: tferrors.NewApplyFailed([]byte(filter)),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.w.fs.WriteFile(directory+"terraform.tfstate", []byte(tfstate), 0777); err != nil {
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
				w: NewWorkspace(directory, WithLastOperation(&Operation{Type: testType, startTime: &now, endTime: nil}),
					WithFilterFn(filterFn)),
			},
			want: want{
				err: errors.Errorf("%s operation that started at %s is still running", testType, now.String()),
			},
		},
		"Success": {
			args: args{
				w: NewWorkspace(
					directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true}), WithFilterFn(filterFn)),
			},
			want: want{},
		},
		"Failure": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(errBoom.Error(), errBoom)), WithFilterFn(filterFn)),
			},
			want: want{
				err: tferrors.NewDestroyFailed([]byte(errBoom.Error())),
			},
		},
		"Filter": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(filter, errors.New(filter))), WithAferoFs(fs),
					WithFilterFn(filterFn)),
			},
			want: want{
				err: tferrors.NewDestroyFailed([]byte(filter)),
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
					WithAferoFs(fs), WithFilterFn(filterFn)),
			},
			want: want{
				r: RefreshResult{
					ASyncInProgress: true,
				},
			},
		},
		"Success": {
			args: args{
				w: NewWorkspace(
					directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true}), WithAferoFs(fs),
					WithFilterFn(filterFn)),
			},
			want: want{
				r: RefreshResult{
					State: state,
				},
			},
		},
		"Failure": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(errBoom.Error(), errBoom)), WithAferoFs(fs),
					WithFilterFn(filterFn)),
			},
			want: want{
				err: tferrors.NewRefreshFailed([]byte(errBoom.Error())),
			},
		},
		"Filter": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(filter, errors.New(filter))), WithAferoFs(fs),
					WithFilterFn(filterFn)),
			},
			want: want{
				err: tferrors.NewRefreshFailed([]byte(filter)),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.w.fs.WriteFile(directory+"terraform.tfstate", []byte(tfstate), 0777); err != nil {
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
				w: NewWorkspace(directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true}), WithFilterFn(filterFn)),
			},
			want: want{
				err: errors.Errorf("cannot find the change summary line in plan log: "),
			},
		},
		"ChangeSummaryAdd": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(changeSummaryAdd, nil)), WithFilterFn(filterFn)),
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
				w: NewWorkspace(directory, WithExecutor(newFakeExec(changeSummaryUpdate, nil)), WithFilterFn(filterFn)),
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
				w: NewWorkspace(directory, WithExecutor(newFakeExec(changeSummaryNoAction, nil)), WithFilterFn(filterFn)),
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
				w: NewWorkspace(directory, WithExecutor(newFakeExec(errBoom.Error(), errBoom)), WithFilterFn(filterFn)),
			},
			want: want{
				err: tferrors.NewPlanFailed([]byte(errBoom.Error())),
			},
		},
		"Filter": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(newFakeExec(filter, errors.New(filter))), WithAferoFs(fs), WithFilterFn(filterFn)),
			},
			want: want{
				err: tferrors.NewPlanFailed([]byte(filter)),
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
				w: NewWorkspace(directory, WithLastOperation(&Operation{Type: testType, startTime: &now, endTime: nil}),
					WithFilterFn(filterFn)),
			},
			want: want{
				err: errors.Errorf("%s operation that started at %s is still running", testType, now.String()),
			},
		},
		"Callback": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true}), WithFilterFn(filterFn)),
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
				w: NewWorkspace(directory, WithLastOperation(&Operation{Type: testType, startTime: &now, endTime: nil}),
					WithFilterFn(filterFn)),
			},
			want: want{
				err: errors.Errorf("%s operation that started at %s is still running", testType, now.String()),
			},
		},
		"Callback": {
			args: args{
				w: NewWorkspace(directory, WithExecutor(&testingexec.FakeExec{DisableScripts: true}), WithFilterFn(filterFn)),
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
