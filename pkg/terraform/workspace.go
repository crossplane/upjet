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

	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane-contrib/terrajet/pkg/resource/json"
	tferrors "github.com/crossplane-contrib/terrajet/pkg/terraform/errors"
)

const (
	defaultAsyncTimeout = 1 * time.Hour
)

// WorkspaceOption allows you to configure Workspace objects.
type WorkspaceOption func(*Workspace)

// WithLogger sets the logger of Workspace.
func WithLogger(l logging.Logger) WorkspaceOption {
	return func(w *Workspace) {
		w.logger = l
	}
}

// NewWorkspace returns a new Workspace object that operates in the given
// directory.
func NewWorkspace(dir string, opts ...WorkspaceOption) *Workspace {
	w := &Workspace{
		LastOperation: &Operation{},
		dir:           dir,
	}
	for _, f := range opts {
		f(w)
	}
	return w
}

// CallbackFn is the type of accepted function that can be called after an async
// operation is completed.
type CallbackFn func(error, context.Context) error

// Workspace runs Terraform operations in its directory and holds the information
// about their statuses.
type Workspace struct {
	// LastOperation contains information about the last operation performed.
	LastOperation *Operation

	dir    string
	env    []string
	logger logging.Logger
}

// ApplyAsync makes a terraform apply call without blocking and calls the given
// function once that apply call finishes.
func (w *Workspace) ApplyAsync(callback CallbackFn) error {
	if w.LastOperation.IsRunning() {
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	w.LastOperation.MarkStart("apply")
	ctx, cancel := context.WithDeadline(context.TODO(), w.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()
		cmd := exec.CommandContext(ctx, "terraform", "apply", "-auto-approve", "-input=false", "-lock=false", "-json")
		w.configureCmd(cmd)
		out, err := cmd.CombinedOutput()
		w.LastOperation.MarkEnd()
		w.logger.Debug("apply async ended", "out", string(out))
		defer func() {
			if cErr := callback(err, ctx); cErr != nil {
				w.logger.Info("callback failed", "error", cErr.Error())
			}
		}()
		if err != nil {
			err = tferrors.NewApplyFailed(out)
		}
	}()
	return nil
}

// ApplyResult contains the state after the apply operation.
type ApplyResult struct {
	State *json.StateV4
}

// Apply makes a blocking terraform apply call.
func (w *Workspace) Apply(ctx context.Context) (ApplyResult, error) {
	if w.LastOperation.IsRunning() {
		return ApplyResult{}, errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	cmd := exec.CommandContext(ctx, "terraform", "apply", "-auto-approve", "-input=false", "-lock=false", "-json")
	w.configureCmd(cmd)
	out, err := cmd.CombinedOutput()
	w.logger.Debug("apply ended", "out", string(out))
	if err != nil {
		return ApplyResult{}, tferrors.NewApplyFailed(out)
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

// DestroyAsync makes a non-blocking terraform destroy call. It doesn't accept
// a callback because destroy operations are not time sensitive as ApplyAsync
// where you might need to store the server-side computed information as soon
// as possible.
func (w *Workspace) DestroyAsync(callback CallbackFn) error {
	switch {
	// Destroy call is idempotent and can be called repeatedly.
	case w.LastOperation.Type == "destroy":
		return nil
	// We cannot run destroy until current non-destroy operation is completed.
	// TODO(muvaf): Gracefully terminate the ongoing apply operation?
	case w.LastOperation.IsRunning():
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	w.LastOperation.MarkStart("destroy")
	ctx, cancel := context.WithDeadline(context.TODO(), w.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()
		cmd := exec.CommandContext(ctx, "terraform", "destroy", "-auto-approve", "-input=false", "-lock=false", "-json")
		w.configureCmd(cmd)
		out, err := cmd.CombinedOutput()
		w.LastOperation.MarkEnd()
		w.logger.Debug("destroy async ended", "out", string(out))
		defer func() {
			if cErr := callback(err, ctx); cErr != nil {
				w.logger.Info("callback failed", "error", cErr.Error())
			}
		}()
		if err != nil {
			err = tferrors.NewDestroyFailed(out)
		}
	}()
	return nil
}

// Destroy makes a blocking terraform destroy call.
func (w *Workspace) Destroy(ctx context.Context) error {
	if w.LastOperation.IsRunning() {
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	cmd := exec.CommandContext(ctx, "terraform", "destroy", "-auto-approve", "-input=false", "-lock=false", "-json")
	w.configureCmd(cmd)
	out, err := cmd.CombinedOutput()
	w.logger.Debug("destroy ended", "out", string(out))
	if err != nil {
		return tferrors.NewDestroyFailed(out)
	}
	return nil
}

// RefreshResult contains information about the current state of the resource.
type RefreshResult struct {
	Exists       bool
	IsApplying   bool
	IsDestroying bool
	State        *json.StateV4
}

// Refresh makes a blocking terraform apply -refresh-only call where only the state file
// is changed with the current state of the resource.
func (w *Workspace) Refresh(ctx context.Context) (RefreshResult, error) {
	switch {
	case w.LastOperation.IsRunning():
		return RefreshResult{
			IsApplying:   w.LastOperation.Type == "apply",
			IsDestroying: w.LastOperation.Type == "destroy",
		}, nil
	case w.LastOperation.IsEnded():
		defer w.LastOperation.Flush()
	}
	cmd := exec.CommandContext(ctx, "terraform", "apply", "-refresh-only", "-auto-approve", "-input=false", "-lock=false", "-json")
	w.configureCmd(cmd)
	out, err := cmd.CombinedOutput()
	w.logger.Debug("refresh ended", "out", string(out))
	if err != nil {
		return RefreshResult{}, tferrors.NewRefreshFailed(out)
	}
	raw, err := os.ReadFile(filepath.Join(w.dir, "terraform.tfstate"))
	if err != nil {
		return RefreshResult{}, errors.Wrap(err, "cannot read terraform state file")
	}
	s := &json.StateV4{}
	if err := json.JSParser.Unmarshal(raw, s); err != nil {
		return RefreshResult{}, errors.Wrap(err, "cannot unmarshal tfstate file")
	}
	return RefreshResult{
		Exists: s.GetAttributes() != nil,
		State:  s,
	}, nil
}

// PlanResult returns a summary of comparison between desired and current state
// of the resource.
type PlanResult struct {
	Exists   bool
	UpToDate bool
}

// Plan makes a blocking terraform plan call.
func (w *Workspace) Plan(ctx context.Context) (PlanResult, error) {
	// The last operation is still ongoing.
	if w.LastOperation.IsRunning() {
		return PlanResult{}, errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	cmd := exec.CommandContext(ctx, "terraform", "plan", "-refresh=false", "-input=false", "-lock=false", "-json")
	w.configureCmd(cmd)
	out, err := cmd.CombinedOutput()
	w.logger.Debug("plan ended", "out", string(out))
	if err != nil {
		return PlanResult{}, tferrors.NewPlanFailed(out)
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

func (w *Workspace) configureCmd(cmd *exec.Cmd) {
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, w.env...)
	cmd.Dir = w.dir
}
