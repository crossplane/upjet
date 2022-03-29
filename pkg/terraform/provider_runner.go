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
	"sync"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/utils/exec"
)

// NativeProviderRunner is the interface for running
// Terraform native provider processes in the shared
// gRPC server mode
type NativeProviderRunner interface {
	StartSharedServer() (string, error)
}

// NoOpProviderRunner is a no-op NativeProviderRunner
type NoOpProviderRunner struct{}

// StartSharedServer takes no action
func (NoOpProviderRunner) StartSharedServer() (string, error) {
	return "", nil
}

// SharedGRPCRunner runs the configured native provider plugin
// using the supplied command-line args
type SharedGRPCRunner struct {
	nativeProviderPath string
	nativeProviderArgs []string
	reattachConfig     string
	logger             logging.Logger
	executor           exec.Interface
	clock              clock.Clock
	mu                 *sync.Mutex
}

// SharedGRPCRunnerOption lets you configure the shared gRPC runner.
type SharedGRPCRunnerOption func(runner *SharedGRPCRunner)

// WithNativeProviderPath enables shared gRPC mode and configures the path
// of the Terraform native provider. When set, Terraform CLI does not fork
// the native plugin for each request but a shared server is used instead.
func WithNativeProviderPath(path string) SharedGRPCRunnerOption {
	return func(sr *SharedGRPCRunner) {
		sr.nativeProviderPath = path
	}
}

// WithNativeProviderArgs are the arguments to be passed to the native provider
func WithNativeProviderArgs(args ...string) SharedGRPCRunnerOption {
	return func(sr *SharedGRPCRunner) {
		sr.nativeProviderArgs = args
	}
}

// WithNativeProviderExecutor sets the process executor to be used
func WithNativeProviderExecutor(e exec.Interface) SharedGRPCRunnerOption {
	return func(sr *SharedGRPCRunner) {
		sr.executor = e
	}
}

// NewSharedGRPCRunner instantiates a SharedGRPCRunner with an
// OS executor using the supplied logger
func NewSharedGRPCRunner(l logging.Logger, opts ...SharedGRPCRunnerOption) *SharedGRPCRunner {
	sr := &SharedGRPCRunner{
		logger:   l,
		executor: exec.New(),
		clock:    clock.RealClock{},
		mu:       &sync.Mutex{},
	}
	for _, o := range opts {
		o(sr)
	}
	return sr
}
