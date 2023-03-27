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

package terraform

import (
	"sync"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/pkg/errors"
)

// ProviderHandle represents native plugin (Terraform provider) process
// handles used by the various schedulers to map Terraform workspaces
// to these processes.
type ProviderHandle string

const (
	// InvalidProviderHandle is an invalid ProviderHandle.
	InvalidProviderHandle ProviderHandle = ""

	ttlMargin = 0.1
)

// ProviderScheduler represents a shared native plugin process scheduler.
type ProviderScheduler interface {
	// Start forks or reuses a native plugin process associated with
	// the supplied ProviderHandle.
	Start(ProviderHandle) (InUse, string, error)
	// Stop terminates the native plugin process, if it exists, for
	// the specified ProviderHandle.
	Stop(ProviderHandle) error
}

// InUse keeps track of the usage of a shared resource,
// like a native plugin process.
type InUse interface {
	// Increment marks one more user of a shared resource
	// such as a native plugin process.
	Increment()
	// Decrement marks when a user of a shared resource,
	// such as a native plugin process, has released the resource.
	Decrement()
}

// noopInUse satisfies the InUse interface and is a noop implementation.
type noopInUse struct{}

func (noopInUse) Increment() {}

func (noopInUse) Decrement() {}

// NoOpProviderScheduler satisfied the ProviderScheduler interface
// and is a noop implementation, i.e., it does not schedule any
// native plugin processes.
type NoOpProviderScheduler struct{}

// NewNoOpProviderScheduler initializes a new NoOpProviderScheduler.
func NewNoOpProviderScheduler() NoOpProviderScheduler {
	return NoOpProviderScheduler{}
}

func (NoOpProviderScheduler) Start(ProviderHandle) (InUse, string, error) {
	return noopInUse{}, "", nil
}

func (NoOpProviderScheduler) Stop(ProviderHandle) error {
	return nil
}

type schedulerEntry struct {
	ProviderRunner
	inUse           int
	invocationCount int
}

type providerInUse struct {
	scheduler *SharedProviderScheduler
	handle    ProviderHandle
}

func (p *providerInUse) Increment() {
	p.scheduler.mu.Lock()
	defer p.scheduler.mu.Unlock()
	r := p.scheduler.runners[p.handle]
	r.inUse++
	r.invocationCount++
}

func (p *providerInUse) Decrement() {
	p.scheduler.mu.Lock()
	defer p.scheduler.mu.Unlock()
	if p.scheduler.runners[p.handle].inUse == 0 {
		return
	}
	p.scheduler.runners[p.handle].inUse--
}

// SharedProviderScheduler is a ProviderScheduler that
// shares a native plugin (Terraform provider) process between
// MR reconciliation loops whose MRs yield the same ProviderHandle, i.e.,
// whose Terraform resource blocks are configuration-wise identical.
// SharedProviderScheduler is configured with a max TTL and it will gracefully
// attempt to replace ProviderRunners whose TTL exceed this maximum,
// if they are not in-use.
type SharedProviderScheduler struct {
	runnerOpts []SharedProviderOption
	runners    map[ProviderHandle]*schedulerEntry
	ttl        int
	mu         *sync.Mutex
	logger     logging.Logger
}

// NewSharedProviderScheduler initializes a new SharedProviderScheduler
// with the specified logger and options.
func NewSharedProviderScheduler(l logging.Logger, ttl int, opts ...SharedProviderOption) *SharedProviderScheduler {
	return &SharedProviderScheduler{
		runnerOpts: opts,
		mu:         &sync.Mutex{},
		runners:    make(map[ProviderHandle]*schedulerEntry),
		logger:     l,
		ttl:        ttl,
	}
}

func (s *SharedProviderScheduler) Start(h ProviderHandle) (InUse, string, error) {
	logger := s.logger.WithValues("handle", h, "ttl", s.ttl, "ttlMargin", ttlMargin)
	s.mu.Lock()
	defer s.mu.Unlock()

	r := s.runners[h]
	switch {
	case r != nil && (r.invocationCount < s.ttl || r.inUse > 0):
		if r.invocationCount > int(float64(s.ttl)*(1+ttlMargin)) {
			logger.Debug("Reuse budget has been exceeded. Caller will need to retry.")
			return nil, "", errors.Errorf("native provider reuse budget has been exceeded: invocationCount: %d, ttl: %d", r.invocationCount, s.ttl)
		}

		logger.Debug("Reusing the provider runner", "invocationCount", r.invocationCount)
		rc, err := r.Start()
		return &providerInUse{
			scheduler: s,
			handle:    h,
		}, rc, errors.Wrapf(err, "cannot use already started provider with handle: %s", h)
	case r != nil:
		logger.Debug("The provider runner has expired. Attempting to stop...", "invocationCount", r.invocationCount)
		if err := r.Stop(); err != nil {
			return nil, "", errors.Wrapf(err, "cannot schedule a new shared provider for handle: %s", h)
		}
	}

	runner := NewSharedProvider(s.runnerOpts...)
	r = &schedulerEntry{
		ProviderRunner: runner,
	}
	runner.logger = logger
	s.runners[h] = r
	logger.Debug("Starting new shared provider...")
	rc, err := s.runners[h].Start()
	return &providerInUse{
		scheduler: s,
		handle:    h,
	}, rc, errors.Wrapf(err, "cannot start the shared provider runner for handle: %s", h)
}

func (s *SharedProviderScheduler) Stop(ProviderHandle) error {
	// noop
	return nil
}

// WorkspaceProviderScheduler is a ProviderScheduler that
// shares a native plugin (Terraform provider) process between
// the Terraform CLI invocations in the context of a single
// reconciliation loop (belonging to a single workspace).
// When the managed.ExternalDisconnecter disconnects,
// the scheduler terminates the native plugin process.
type WorkspaceProviderScheduler struct {
	runner ProviderRunner
	logger logging.Logger
	inUse  *workspaceInUse
}

type workspaceInUse struct {
	wg *sync.WaitGroup
}

func (w *workspaceInUse) Increment() {
	w.wg.Add(1)
}

func (w *workspaceInUse) Decrement() {
	w.wg.Done()
}

// NewWorkspaceProviderScheduler initializes a new WorkspaceProviderScheduler.
func NewWorkspaceProviderScheduler(l logging.Logger, opts ...SharedProviderOption) *WorkspaceProviderScheduler {
	return &WorkspaceProviderScheduler{
		logger: l,
		runner: NewSharedProvider(append([]SharedProviderOption{WithNativeProviderLogger(l)}, opts...)...),
		inUse: &workspaceInUse{
			wg: &sync.WaitGroup{},
		},
	}
}

func (s *WorkspaceProviderScheduler) Start(h ProviderHandle) (InUse, string, error) {
	s.logger.Debug("Starting workspace scoped provider runner.", "handle", h)
	reattachConfig, err := s.runner.Start()
	return s.inUse, reattachConfig, errors.Wrap(err, "cannot start a workspace provider runner")
}

func (s *WorkspaceProviderScheduler) Stop(h ProviderHandle) error {
	s.logger.Debug("Attempting to stop workspace scoped shared provider runner.", "handle", h)
	go func() {
		s.inUse.wg.Wait()
		s.logger.Debug("Provider runner not in-use, stopping it.", "handle", h)
		if err := s.runner.Stop(); err != nil {
			s.logger.Info("Failed to stop provider runner", "error", errors.Wrap(err, "cannot stop a workspace provider runner"))
		}
	}()
	return nil
}
