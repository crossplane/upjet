/*
Copyright 2022 The Crossplane Authors.

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
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/utils/exec"
	testingexec "k8s.io/utils/exec/testing"
)

func TestStartSharedServer(t *testing.T) {
	testPath := "path"
	testArgs := []string{"arg1", "arg2"}
	testReattachConfig1 := `TF_REATTACH_PROVIDERS='test1'`
	testReattachConfig2 := `TF_REATTACH_PROVIDERS='test2'`
	testErr := errors.New("boom")
	type args struct {
		runner NativeProviderRunner
	}
	type want struct {
		reattachConfig string
		err            error
	}
	tests := map[string]struct {
		args args
		want want
	}{
		"NotConfiguredNoOp": {
			args: args{
				runner: NoOpProviderRunner{},
			},
		},
		"NotConfiguredSharedGRPC": {
			args: args{
				runner: NewSharedGRPCRunner(logging.NewNopLogger()),
			},
			want: want{
				err: errors.New(errNativeProviderPath),
			},
		},
		"SuccessfullyStarted": {
			args: args{
				runner: NewSharedGRPCRunner(logging.NewNopLogger(), WithNativeProviderPath(testPath), WithNativeProviderArgs(testArgs...),
					WithNativeProviderExecutor(newExecutorWithStoutPipe(testReattachConfig1, nil))),
			},
			want: want{
				reattachConfig: "test1",
			},
		},
		"AlreadyRunning": {
			args: args{
				runner: &SharedGRPCRunner{
					nativeProviderPath: testPath,
					reattachConfig:     "test1",
					logger:             logging.NewNopLogger(),
					executor:           newExecutorWithStoutPipe(testReattachConfig2, nil),
					mu:                 &sync.Mutex{},
				},
			},
			want: want{
				reattachConfig: "test1",
			},
		},
		"NativeProviderError": {
			args: args{
				runner: NewSharedGRPCRunner(logging.NewNopLogger(), WithNativeProviderPath(testPath),
					WithNativeProviderExecutor(newExecutorWithStoutPipe(testReattachConfig1, testErr))),
			},
			want: want{
				err: testErr,
			},
		},
		"NativeProviderTimeout": {
			args: args{
				runner: &SharedGRPCRunner{
					nativeProviderPath: testPath,
					logger:             logging.NewNopLogger(),
					executor:           newExecutorWithStoutPipe("invalid", nil),
					mu:                 &sync.Mutex{},
					clock:              &fakeClock{},
				},
			},
			want: want{
				err: errors.Errorf(errFmtTimeout, reattachTimeout),
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			reattachConfig, err := tt.args.runner.StartSharedServer()
			if diff := cmp.Diff(tt.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nStartSharedServer(): -want error, +got error:\n%s", name, diff)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(reattachConfig, tt.want.reattachConfig); diff != "" {
				t.Errorf("\n%s\nStartSharedServer(): -want reattachConfig, +got reattachConfig:\n%s", name, diff)
			}
		})
	}
}

type fakeClock struct {
	clock.FakeClock
}

func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	defer func() {
		f.Step(reattachTimeout)
	}()
	return f.FakeClock.After(d)
}

func newExecutorWithStoutPipe(reattachConfig string, err error) exec.Interface {
	return &testingexec.FakeExec{
		CommandScript: []testingexec.FakeCommandAction{
			func(cmd string, args ...string) exec.Cmd {
				return &testingexec.FakeCmd{
					StdoutPipeResponse: testingexec.FakeStdIOPipeResponse{
						ReadCloser: io.NopCloser(strings.NewReader(reattachConfig)),
						Error:      err,
					},
				}
			},
		},
	}
}

func TestWithNativeProviderPath(t *testing.T) {
	tests := map[string]struct {
		path string
		want string
	}{
		"NotConfigured": {
			path: "",
			want: "",
		},
		"Configured": {
			path: "a/b/c",
			want: "a/b/c",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sr := &SharedGRPCRunner{}
			WithNativeProviderPath(tt.path)(sr)
			if !reflect.DeepEqual(sr.nativeProviderPath, tt.want) {
				t.Errorf("WithNativeProviderPath(tt.path) = %v, want %v", sr.nativeProviderArgs, tt.want)
			}
		})
	}
}

func TestWithNativeProviderArgs(t *testing.T) {
	tests := map[string]struct {
		args []string
		want []string
	}{
		"NotConfigured": {},
		"Configured": {
			args: []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sr := &SharedGRPCRunner{}
			WithNativeProviderArgs(tt.args...)(sr)
			if !reflect.DeepEqual(sr.nativeProviderArgs, tt.want) {
				t.Errorf("WithNativeProviderArgs(tt.args) = %v, want %v", sr.nativeProviderArgs, tt.want)
			}
		})
	}
}

func TestWithNativeProviderExecutor(t *testing.T) {
	tests := map[string]struct {
		executor exec.Interface
		want     exec.Interface
	}{
		"NotConfigured": {},
		"Configured": {
			executor: &testingexec.FakeExec{},
			want:     &testingexec.FakeExec{},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sr := &SharedGRPCRunner{}
			WithNativeProviderExecutor(tt.executor)(sr)
			if !reflect.DeepEqual(sr.executor, tt.want) {
				t.Errorf("WithNativeProviderExecutor(tt.executor) = %v, want %v", sr.executor, tt.want)
			}
		})
	}
}
