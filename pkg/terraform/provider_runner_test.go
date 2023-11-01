// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package terraform

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	clock "k8s.io/utils/clock/testing"
	"k8s.io/utils/exec"
	testingexec "k8s.io/utils/exec/testing"
)

func TestStartSharedServer(t *testing.T) {
	testPath := "path"
	testName := "provider-test"
	testArgs := []string{"arg1", "arg2"}
	testReattachConfig1 := `1|5|unix|test1|grpc|`
	testReattachConfig2 := `1|5|unix|test2|grpc|`
	testErr := errors.New("boom")
	type args struct {
		runner ProviderRunner
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
				runner: NewNoOpProviderRunner(),
			},
		},
		"SuccessfullyStarted": {
			args: args{
				runner: NewSharedProvider(WithNativeProviderLogger(logging.NewNopLogger()), WithNativeProviderPath(testPath),
					WithNativeProviderName(testName), WithNativeProviderArgs(testArgs...), WithNativeProviderExecutor(newExecutorWithStoutPipe(testReattachConfig1, nil))),
			},
			want: want{
				reattachConfig: fmt.Sprintf(`{"provider-test":{"Protocol":"grpc","ProtocolVersion":5,"Pid":%d,"Test": true,"Addr":{"Network": "unix","String": "test1"}}}`, os.Getpid()),
			},
		},
		"AlreadyRunning": {
			args: args{
				runner: &SharedProvider{
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
				runner: NewSharedProvider(WithNativeProviderLogger(logging.NewNopLogger()), WithNativeProviderPath(testPath),
					WithNativeProviderName(testName), WithNativeProviderArgs(testArgs...), WithNativeProviderExecutor(newExecutorWithStoutPipe(testReattachConfig1, testErr))),
			},
			want: want{
				err: testErr,
			},
		},
		"NativeProviderTimeout": {
			args: args{
				runner: &SharedProvider{
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
			reattachConfig, err := tt.args.runner.Start()
			if diff := cmp.Diff(tt.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nStartSharedServer(): -want error, +got error:\n%s", name, diff)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tt.want.reattachConfig, reattachConfig); diff != "" {
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
			sr := &SharedProvider{}
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
			sr := &SharedProvider{}
			WithNativeProviderExecutor(tt.executor)(sr)
			if !reflect.DeepEqual(sr.executor, tt.want) {
				t.Errorf("WithNativeProviderExecutor(tt.executor) = %v, want %v", sr.executor, tt.want)
			}
		})
	}
}
