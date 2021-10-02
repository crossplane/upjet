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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/resource/json"
)

type WorkspaceOption func(c *Workspace)

func WithEnqueueFn(fn EnqueueFn) WorkspaceOption {
	return func(w *Workspace) {
		w.Enqueue = fn
	}
}

func WithLogger(l logging.Logger) WorkspaceOption {
	return func(w *Workspace) {
		w.logger = l
	}
}

func NewWorkspace(dir string, opts ...WorkspaceOption) *Workspace {
	w := &Workspace{
		LastOperation: &Operation{},
		dir:           dir,
		Enqueue:       NopEnqueueFn,
	}
	for _, f := range opts {
		f(w)
	}
	return w
}

type EnqueueFn func()

func NopEnqueueFn() {}

type Workspace struct {
	LastOperation *Operation
	Enqueue       EnqueueFn
	AsyncTimeout  time.Duration

	dir    string
	logger logging.Logger
}

func (w *Workspace) ApplyAsync() error {
	if w.LastOperation.IsInProgress() {
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	w.LastOperation.MarkStart("apply")
	ctx, cancel := context.WithDeadline(context.TODO(), w.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		cmd := exec.CommandContext(ctx, "terraform", "apply", "-auto-approve", "-input=false", "-lock=false", "-json")
		cmd.Dir = w.dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			w.LastOperation.Err = errors.Wrapf(err, "cannot apply: %s", string(out))
		}
		w.LastOperation.MarkEnd()
		w.logger.Debug("apply async ended", "out", string(out))

		// After the operation is completed, we need to get the results saved on
		// the custom resource as soon as possible. We can wait for the next
		// reconciliation, enqueue manually or update the CR independent of the
		// reconciliation.
		w.Enqueue()
		cancel()
	}()
	return nil
}

type ApplyResult struct {
	State *json.StateV4
}

func (w *Workspace) Apply(ctx context.Context) (ApplyResult, error) {
	if w.LastOperation.IsInProgress() {
		return ApplyResult{}, errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	cmd := exec.CommandContext(ctx, "terraform", "apply", "-auto-approve", "-input=false", "-lock=false", "-detailed-exitcode", "-json")
	cmd.Dir = w.dir
	out, err := cmd.CombinedOutput()
	w.logger.Debug("apply ended", "out", string(out))
	if err != nil {
		return ApplyResult{}, errors.Wrapf(err, "cannot apply: %s", string(out))
	}
	raw, err := os.ReadFile(filepath.Join(w.dir, "terraform.tfstate"))
	if err != nil {
		return ApplyResult{}, errors.Wrap(err, "cannot read terraform state file")
	}
	s := &json.StateV4{}
	if err := json.JSParser.Unmarshal(raw, s); err != nil {
		return ApplyResult{}, errors.Wrap(err, "cannot unmarshal tfstate file")
	}
	return ApplyResult{State: s}, nil
}

func (w *Workspace) DestroyAsync() error {
	switch {
	// Destroy call is idempotent and can be called repeatedly.
	case w.LastOperation.Type == "destroy":
		return nil
	// We cannot run destroy until current non-destroy operation is completed.
	// TODO(muvaf): Gracefully terminate the ongoing apply operation?
	case w.LastOperation.IsInProgress():
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	w.LastOperation.MarkStart("destroy")
	ctx, cancel := context.WithDeadline(context.TODO(), w.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		cmd := exec.CommandContext(ctx, "terraform", "destroy", "-auto-approve", "-input=false", "-lock=false", "-json")
		cmd.Dir = w.dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			w.LastOperation.Err = errors.Wrapf(err, "cannot destroy: %s", string(out))
		}
		w.LastOperation.MarkEnd()
		w.logger.Debug("destroy async ended", "out", string(out))

		// After the operation is completed, we need to get the results saved on
		// the custom resource as soon as possible. We can wait for the next
		// reconcilitaion, enqueue manually or update the CR independent of the
		// reconciliation.
		w.Enqueue()
		cancel()
	}()
	return nil
}

func (w *Workspace) Destroy(ctx context.Context) error {
	if w.LastOperation.IsInProgress() {
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	cmd := exec.CommandContext(ctx, "terraform", "destroy", "-auto-approve", "-input=false", "-lock=false", "-json")
	cmd.Dir = w.dir
	out, err := cmd.CombinedOutput()
	w.logger.Debug("destroy ended", "out", string(out))
	return errors.Wrapf(err, "cannot destroy: %s", string(out))
}

type RefreshResult struct {
	IsApplying         bool
	IsDestroying       bool
	State              *json.StateV4
	LastOperationError error
}

func (w *Workspace) Refresh(ctx context.Context) (RefreshResult, error) {
	switch {
	case w.LastOperation.IsInProgress():
		return RefreshResult{
			IsApplying:   w.LastOperation.Type == "apply",
			IsDestroying: w.LastOperation.Type == "destroy",
		}, nil
	case w.LastOperation.IsEnded():
		defer w.LastOperation.Flush()
		// The last operation finished with error.
		if w.LastOperation.Err != nil {
			return RefreshResult{
				IsApplying:         w.LastOperation.Type == "apply",
				IsDestroying:       w.LastOperation.Type == "destroy",
				LastOperationError: errors.Wrapf(w.LastOperation.Err, "%s operation failed", w.LastOperation.Type),
			}, nil
		}
		// The deletion is completed so there is no resource to refresh.
		if w.LastOperation.Type == "destroy" {
			return RefreshResult{}, NewNotFound()
		}
	}
	cmd := exec.CommandContext(ctx, "terraform", "apply", "-refresh-only", "-auto-approve", "-input=false", "-lock=false", "-json")
	cmd.Dir = w.dir
	out, err := cmd.CombinedOutput()
	w.logger.Debug("refresh ended", "out", string(out))
	if err != nil {
		return RefreshResult{}, errors.Wrapf(err, "cannot refresh: %s", string(out))
	}
	raw, err := os.ReadFile(filepath.Join(w.dir, "terraform.tfstate"))
	if err != nil {
		return RefreshResult{}, errors.Wrap(err, "cannot read terraform state file")
	}
	s := &json.StateV4{}
	if err := json.JSParser.Unmarshal(raw, s); err != nil {
		return RefreshResult{}, errors.Wrap(err, "cannot unmarshal tfstate file")
	}
	if len(s.Resources) == 0 || len(s.Resources[0].Instances) == 0 {
		return RefreshResult{}, NewNotFound()
	}
	return RefreshResult{State: s}, nil
}

type PlanResult struct {
	Exists   bool
	UpToDate bool
}

func (w *Workspace) Plan(ctx context.Context) (PlanResult, error) {
	// The last operation is still ongoing.
	if w.LastOperation.IsInProgress() {
		return PlanResult{}, errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	cmd := exec.CommandContext(ctx, "terraform", "plan", "-refresh=false", "-input=false", "-lock=false", "-json")
	cmd.Dir = w.dir
	out, err := cmd.CombinedOutput()
	w.logger.Debug("plan ended", "out", string(out))
	if err != nil {
		return PlanResult{}, errors.Wrapf(err, "cannot plan: %s", string(out))
	}
	line := ""
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, `"type":"change_summary"`) {
			line = l
			break
		}
	}
	if line == "" {
		return PlanResult{}, errors.Errorf("cannot find the change summary line in plan log: %s", string(out))
	}
	type plan struct {
		Changes struct {
			Add    float64 `json:"add,omitempty"`
			Change float64 `json:"change,omitempty"`
		} `json:"changes,omitempty"`
	}
	p := &plan{}
	if err := json.JSParser.Unmarshal([]byte(line), p); err != nil {
		return PlanResult{}, errors.Wrap(err, "cannot unmarshal change summary json")
	}
	return PlanResult{
		Exists:   p.Changes.Add == 0,
		UpToDate: p.Changes.Change == 0,
	}, nil
}
