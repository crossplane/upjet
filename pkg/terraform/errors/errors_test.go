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
				err: WrapApplyFailed(errorBoom, nil),
			},
			want: true,
		},
		"ApplyErrorWithLog": {
			args: args{
				err: WrapApplyFailed(errorBoom, errorLog),
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
				err: WrapDestroyFailed(errorBoom, nil),
			},
			want: true,
		},
		"DestroyErrorWithLog": {
			args: args{
				err: WrapDestroyFailed(errorBoom, errorLog),
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

func TestWrapApplyFailed(t *testing.T) {
	type args struct {
		cause error
		logs  []byte
	}
	tests := map[string]struct {
		args           args
		wantErrMessage string
	}{
		"NilCause": {
			args: args{},
		},
		"ApplyError": {
			args: args{
				cause: errorBoom,
				logs:  errorLog,
			},
			wantErrMessage: "apply failed: Missing required argument: The argument \"location\" is required, but no definition was found.\nMissing required argument: The argument \"name\" is required, but no definition was found.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := WrapApplyFailed(tt.args.cause, tt.args.logs)
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

func TestWrapDestroyFailed(t *testing.T) {
	type args struct {
		cause error
		logs  []byte
	}
	tests := map[string]struct {
		args           args
		wantErrMessage string
	}{
		"NilCause": {
			args: args{},
		},
		"DestroyError": {
			args: args{
				cause: errorBoom,
				logs:  errorLog,
			},
			wantErrMessage: "destroy failed: Missing required argument: The argument \"location\" is required, but no definition was found.\nMissing required argument: The argument \"name\" is required, but no definition was found.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := WrapDestroyFailed(tt.args.cause, tt.args.logs)
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

func TestWrapTFError(t *testing.T) {
	type args struct {
		cause error
		logs  []byte
	}
	tests := map[string]struct {
		args           args
		wantErrMessage string
	}{
		"NilCause": {
			args: args{},
		},
		"ApplyError": {
			args: args{
				cause: errorBoom,
				logs:  errorLog,
			},
			wantErrMessage: "test operation: Missing required argument: The argument \"location\" is required, but no definition was found.\nMissing required argument: The argument \"name\" is required, but no definition was found.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := WrapTFError("test operation", tt.args.cause, tt.args.logs)
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
