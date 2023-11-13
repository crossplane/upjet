// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

var (
	errorLog = []byte(`{"@level":"info","@message":"Terraform 1.0.3","@module":"terraform.ui","@timestamp":"2021-11-14T23:23:14.009380+03:00","terraform":"1.0.3","type":"version","ui":"0.1.0"}
{"@level":"error","@message":"Error: Missing required argument","@module":"terraform.ui","@timestamp":"2021-11-14T23:23:14.576254+03:00","diagnostic":{"severity":"error","summary":"Missing required argument","detail":"The argument \"location\" is required, but no definition was found.","range":{"filename":"main.tf.json","start":{"line":24,"column":7,"byte":568},"end":{"line":24,"column":8,"byte":569}},"snippet":{"context":"resource.azurerm_resource_group.example","code":"      }","start_line":24,"highlight_start_offset":6,"highlight_end_offset":7,"values":[]}},"type":"diagnostic"}
{"@level":"error","@message":"Error: Missing required argument","@module":"terraform.ui","@timestamp":"2021-11-14T23:23:14.576430+03:00","diagnostic":{"severity":"error","summary":"Missing required argument","detail":"The argument \"name\" is required, but no definition was found.","range":{"filename":"main.tf.json","start":{"line":24,"column":7,"byte":568},"end":{"line":24,"column":8,"byte":569}},"snippet":{"context":"resource.azurerm_resource_group.example","code":"      }","start_line":24,"highlight_start_offset":6,"highlight_end_offset":7,"values":[]}},"type":"diagnostic"}`)
	errorBoom = errors.New("boom")
)

func TestIsApplyFailed(t *testing.T) {
	var nilApplyErr *applyFailed
	type args struct {
		err error
	}
	tests := map[string]struct {
		args args
		want bool
	}{
		"NilError": {
			args: args{},
			want: false,
		},
		"NilApplyError": {
			args: args{
				err: nilApplyErr,
			},
			want: true,
		},
		"NonApplyError": {
			args: args{
				err: errorBoom,
			},
			want: false,
		},
		"ApplyErrorNoLog": {
			args: args{
				err: NewApplyFailed(nil),
			},
			want: true,
		},
		"ApplyErrorWithLog": {
			args: args{
				err: NewApplyFailed(errorLog),
			},
			want: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsApplyFailed(tt.args.err); got != tt.want {
				t.Errorf("IsApplyFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsDestroyFailed(t *testing.T) {
	var nilDestroyErr *destroyFailed
	type args struct {
		err error
	}
	tests := map[string]struct {
		args args
		want bool
	}{
		"NilError": {
			args: args{},
			want: false,
		},
		"NilDestroyError": {
			args: args{
				err: nilDestroyErr,
			},
			want: true,
		},
		"NonDestroyError": {
			args: args{
				err: errorBoom,
			},
			want: false,
		},
		"DestroyErrorNoLog": {
			args: args{
				err: NewDestroyFailed(nil),
			},
			want: true,
		},
		"DestroyErrorWithLog": {
			args: args{
				err: NewDestroyFailed(errorLog),
			},
			want: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsDestroyFailed(tt.args.err); got != tt.want {
				t.Errorf("IsDestroyFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRefreshFailed(t *testing.T) {
	var nilRefreshErr *refreshFailed
	type args struct {
		err error
	}
	tests := map[string]struct {
		args args
		want bool
	}{
		"NilError": {
			args: args{},
			want: false,
		},
		"NilRefreshError": {
			args: args{
				err: nilRefreshErr,
			},
			want: true,
		},
		"NonRefreshError": {
			args: args{
				err: errorBoom,
			},
			want: false,
		},
		"RefreshErrorNoLog": {
			args: args{
				err: NewRefreshFailed(nil),
			},
			want: true,
		},
		"RefreshErrorWithLog": {
			args: args{
				err: NewRefreshFailed(errorLog),
			},
			want: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsRefreshFailed(tt.args.err); got != tt.want {
				t.Errorf("IsRefreshFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsPlanFailed(t *testing.T) {
	var nilPlanErr *planFailed
	type args struct {
		err error
	}
	tests := map[string]struct {
		args args
		want bool
	}{
		"NilError": {
			args: args{},
			want: false,
		},
		"NilPlanError": {
			args: args{
				err: nilPlanErr,
			},
			want: true,
		},
		"NonPlanError": {
			args: args{
				err: errorBoom,
			},
			want: false,
		},
		"PlanErrorNoLog": {
			args: args{
				err: NewPlanFailed(nil),
			},
			want: true,
		},
		"PlanErrorWithLog": {
			args: args{
				err: NewPlanFailed(errorLog),
			},
			want: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsPlanFailed(tt.args.err); got != tt.want {
				t.Errorf("IsPlanFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewApplyFailed(t *testing.T) {
	type args struct {
		logs []byte
	}
	tests := map[string]struct {
		args           args
		wantErrMessage string
	}{
		"ApplyError": {
			args: args{
				logs: errorLog,
			},
			wantErrMessage: "apply failed: Missing required argument: The argument \"location\" is required, but no definition was found.\nMissing required argument: The argument \"name\" is required, but no definition was found.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := NewApplyFailed(tt.args.logs)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tt.wantErrMessage, got); diff != "" {
				t.Errorf("\nWrapApplyFailed(...): -want message, +got message:\n%s", diff)
			}
		})
	}
}

func TestNewDestroyFailed(t *testing.T) {
	type args struct {
		logs []byte
	}
	tests := map[string]struct {
		args           args
		wantErrMessage string
	}{
		"DestroyError": {
			args: args{
				logs: errorLog,
			},
			wantErrMessage: "destroy failed: Missing required argument: The argument \"location\" is required, but no definition was found.\nMissing required argument: The argument \"name\" is required, but no definition was found.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := NewDestroyFailed(tt.args.logs)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tt.wantErrMessage, got); diff != "" {
				t.Errorf("\nWrapApplyFailed(...): -want message, +got message:\n%s", diff)
			}
		})
	}
}

func TestNewRefreshFailed(t *testing.T) {
	type args struct {
		logs []byte
	}
	tests := map[string]struct {
		args           args
		wantErrMessage string
	}{
		"RefreshError": {
			args: args{
				logs: errorLog,
			},
			wantErrMessage: "refresh failed: Missing required argument: The argument \"location\" is required, but no definition was found.\nMissing required argument: The argument \"name\" is required, but no definition was found.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := NewRefreshFailed(tt.args.logs)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tt.wantErrMessage, got); diff != "" {
				t.Errorf("\nWrapRefreshFailed(...): -want message, +got message:\n%s", diff)
			}
		})
	}
}

func TestNewPlanFailed(t *testing.T) {
	type args struct {
		logs []byte
	}
	tests := map[string]struct {
		args           args
		wantErrMessage string
	}{
		"PlanError": {
			args: args{
				logs: errorLog,
			},
			wantErrMessage: "plan failed: Missing required argument: The argument \"location\" is required, but no definition was found.\nMissing required argument: The argument \"name\" is required, but no definition was found.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := NewPlanFailed(tt.args.logs)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tt.wantErrMessage, got); diff != "" {
				t.Errorf("\nWrapPlanFailed(...): -want message, +got message:\n%s", diff)
			}
		})
	}
}

func TestNewRetryScheduleError(t *testing.T) {
	type args struct {
		invocationCount, ttl int
	}
	tests := map[string]struct {
		args
		wantErrMessage string
	}{
		"Successful": {
			args: args{
				invocationCount: 101,
				ttl:             100,
			},
			wantErrMessage: "native provider reuse budget has been exceeded: invocationCount: 101, ttl: 100",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := NewRetryScheduleError(tc.args.invocationCount, tc.args.ttl)
			got := err.Error()
			if diff := cmp.Diff(tc.wantErrMessage, got); diff != "" {
				t.Errorf("\nNewRetryScheduleError(...): -want message, +got message:\n%s", diff)
			}
		})
	}
}

func TestIsRetryScheduleError(t *testing.T) {
	var nilErr *retrySchedule
	type args struct {
		err error
	}
	tests := map[string]struct {
		args
		want bool
	}{
		"NilError": {
			args: args{},
			want: false,
		},
		"NilRetryScheduleError": {
			args: args{
				err: nilErr,
			},
			want: true,
		},
		"NonRetryScheduleError": {
			args: args{
				err: errorBoom,
			},
			want: false,
		},
		"Successful": {
			args: args{err: NewRetryScheduleError(101, 100)},
			want: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsRetryScheduleError(tc.args.err); got != tc.want {
				t.Errorf("IsRetryScheduleError() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewAsyncCreateFailed(t *testing.T) {
	type args struct {
		err error
	}
	tests := map[string]struct {
		args
		wantErrMessage string
	}{
		"Successful": {
			args: args{
				err: errors.New("already exists"),
			},
			wantErrMessage: "async create failed: already exists",
		},
		"Nil": {
			args: args{
				err: nil,
			},
			wantErrMessage: "",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := NewAsyncCreateFailed(tc.args.err)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tc.wantErrMessage, got); diff != "" {
				t.Errorf("\nNewAsyncCreateFailed(...): -want message, +got message:\n%s", diff)
			}
		})
	}
}

func TestIsAsyncCreateFailed(t *testing.T) {
	var nilErr *asyncCreateFailed
	type args struct {
		err error
	}
	tests := map[string]struct {
		args
		want bool
	}{
		"NilError": {
			args: args{},
			want: false,
		},
		"NilAsyncCreateError": {
			args: args{
				err: nilErr,
			},
			want: true,
		},
		"NonAsyncCreateError": {
			args: args{
				err: errorBoom,
			},
			want: false,
		},
		"Successful": {
			args: args{err: NewAsyncCreateFailed(errors.New("test"))},
			want: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsAsyncCreateFailed(tc.args.err); got != tc.want {
				t.Errorf("IsAsyncCreateFailed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAsyncUpdateFailed(t *testing.T) {
	type args struct {
		err error
	}
	tests := map[string]struct {
		args
		wantErrMessage string
	}{
		"Successful": {
			args: args{
				err: errors.New("immutable field"),
			},
			wantErrMessage: "async update failed: immutable field",
		},
		"Nil": {
			args: args{
				err: nil,
			},
			wantErrMessage: "",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := NewAsyncUpdateFailed(tc.args.err)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tc.wantErrMessage, got); diff != "" {
				t.Errorf("\nAsyncUpdateFailed(...): -want message, +got message:\n%s", diff)
			}
		})
	}
}

func TestIsAsyncUpdateFailed(t *testing.T) {
	var nilErr *asyncUpdateFailed
	type args struct {
		err error
	}
	tests := map[string]struct {
		args
		want bool
	}{
		"NilError": {
			args: args{},
			want: false,
		},
		"NilAsyncUpdateError": {
			args: args{
				err: nilErr,
			},
			want: true,
		},
		"NonAsyncUpdateError": {
			args: args{
				err: errorBoom,
			},
			want: false,
		},
		"Successful": {
			args: args{err: NewAsyncUpdateFailed(errors.New("test"))},
			want: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsAsyncUpdateFailed(tc.args.err); got != tc.want {
				t.Errorf("IsAsyncUpdateFailed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAsyncDeleteFailed(t *testing.T) {
	type args struct {
		err error
	}
	tests := map[string]struct {
		args
		wantErrMessage string
	}{
		"Successful": {
			args: args{
				err: errors.New("dependency violation"),
			},
			wantErrMessage: "async delete failed: dependency violation",
		},
		"Nil": {
			args: args{
				err: nil,
			},
			wantErrMessage: "",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := NewAsyncDeleteFailed(tc.args.err)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tc.wantErrMessage, got); diff != "" {
				t.Errorf("\nAsyncDeleteFailed(...): -want message, +got message:\n%s", diff)
			}
		})
	}
}

func TestIsAsyncDeleteFailed(t *testing.T) {
	var nilErr *asyncDeleteFailed
	type args struct {
		err error
	}
	tests := map[string]struct {
		args
		want bool
	}{
		"NilError": {
			args: args{},
			want: false,
		},
		"NilAsyncUpdateError": {
			args: args{
				err: nilErr,
			},
			want: true,
		},
		"NonAsyncUpdateError": {
			args: args{
				err: errorBoom,
			},
			want: false,
		},
		"Successful": {
			args: args{err: NewAsyncDeleteFailed(errors.New("test"))},
			want: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsAsyncDeleteFailed(tc.args.err); got != tc.want {
				t.Errorf("IsAsyncDeleteFailed() = %v, want %v", got, tc.want)
			}
		})
	}
}
