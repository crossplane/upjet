// Copyright 2021 Upbound Inc.
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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/afero"
	k8sExec "k8s.io/utils/exec"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/upbound/upjet/pkg/metrics"
	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/resource/json"
	tferrors "github.com/upbound/upjet/pkg/terraform/errors"
)

const (
	defaultAsyncTimeout = 1 * time.Hour
	envReattachConfig   = "TF_REATTACH_PROVIDERS"
	fmtEnv              = "%s=%s"
)

// ExecMode is the Terraform CLI execution mode label
type ExecMode int

const (
	// ModeSync represents the synchronous execution mode
	ModeSync ExecMode = iota
	// ModeASync represents the asynchronous execution mode
	ModeASync
)

// String converts an execMode to string
func (em ExecMode) String() string {
	switch em {
	case ModeSync:
		return "sync"
	case ModeASync:
		return "async"
	default:
		return "unknown"
	}
}

// WorkspaceOption allows you to configure Workspace objects.
type WorkspaceOption func(*Workspace)

// WithLogger sets the logger of Workspace.
func WithLogger(l logging.Logger) WorkspaceOption {
	return func(w *Workspace) {
		w.logger = l
	}
}

// WithExecutor sets the executor of Workspace.
func WithExecutor(e k8sExec.Interface) WorkspaceOption {
	return func(w *Workspace) {
		w.executor = e
	}
}

// WithLastOperation sets the Last Operation of Workspace.
func WithLastOperation(lo *Operation) WorkspaceOption {
	return func(w *Workspace) {
		w.LastOperation = lo
	}
}

// WithAferoFs lets you set the fs of WorkspaceStore.
func WithAferoFs(fs afero.Fs) WorkspaceOption {
	return func(ws *Workspace) {
		ws.fs = afero.Afero{Fs: fs}
	}
}

// WithFilterFn configures the debug log sensitive information filtering func.
func WithFilterFn(filterFn func(string) string) WorkspaceOption {
	return func(w *Workspace) {
		w.filterFn = filterFn
	}
}

// WithProviderInUse configures an InUse for keeping track of
// the shared provider InUse by this Terraform workspace.
func WithProviderInUse(providerInUse InUse) WorkspaceOption {
	return func(w *Workspace) {
		w.providerInUse = providerInUse
	}
}

// NewWorkspace returns a new Workspace object that operates in the given
// directory.
func NewWorkspace(dir string, opts ...WorkspaceOption) *Workspace {
	w := &Workspace{
		LastOperation: &Operation{},
		dir:           dir,
		logger:        logging.NewNopLogger(),
		fs:            afero.Afero{Fs: afero.NewOsFs()},
		providerInUse: noopInUse{},
		mu:            &sync.Mutex{},
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
	// ProviderHandle is the handle of the associated native Terraform provider
	// computed from the generated provider resource configuration block
	// of the Terraform workspace.
	ProviderHandle ProviderHandle

	dir string
	env []string

	logger        logging.Logger
	executor      k8sExec.Interface
	providerInUse InUse
	fs            afero.Afero
	mu            *sync.Mutex

	filterFn func(string) string

	terraformID string
}

// UseProvider shares a native provider with the receiver Workspace.
func (w *Workspace) UseProvider(inuse InUse, attachmentConfig string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// remove existing reattach configs
	env := make([]string, 0, len(w.env))
	prefix := fmt.Sprintf(fmtEnv, envReattachConfig, "")
	for _, e := range w.env {
		if !strings.HasPrefix(e, prefix) {
			env = append(env, e)
		}
	}
	env = append(env, prefix+attachmentConfig)
	w.env = env
	w.providerInUse = inuse
}

// ApplyAsync makes a terraform apply call without blocking and calls the given
// function once that apply call finishes.
func (w *Workspace) ApplyAsync(callback CallbackFn) error {
	if !w.LastOperation.MarkStart("apply") {
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	ctx, cancel := context.WithDeadline(context.TODO(), w.LastOperation.StartTime().Add(defaultAsyncTimeout))
	w.providerInUse.Increment()
	go func() {
		defer cancel()
		out, err := w.runTF(ctx, ModeASync, "apply", "-auto-approve", "-input=false", "-lock=false", "-json")
		if err != nil {
			err = tferrors.NewApplyFailed(out)
		}
		w.LastOperation.MarkEnd()
		w.logger.Debug("apply async ended", "out", w.filterFn(string(out)))
		defer func() {
			if cErr := callback(err, ctx); cErr != nil {
				w.logger.Info("callback failed", "error", cErr.Error())
			}
		}()
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
	out, err := w.runTF(ctx, ModeSync, "apply", "-auto-approve", "-input=false", "-lock=false", "-json")
	w.logger.Debug("apply ended", "out", w.filterFn(string(out)))
	if err != nil {
		return ApplyResult{}, tferrors.NewApplyFailed(out)
	}
	raw, err := w.fs.ReadFile(filepath.Join(w.dir, "terraform.tfstate"))
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
	case !w.LastOperation.MarkStart("destroy"):
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	ctx, cancel := context.WithDeadline(context.TODO(), w.LastOperation.StartTime().Add(defaultAsyncTimeout))
	w.providerInUse.Increment()
	go func() {
		defer cancel()
		out, err := w.runTF(ctx, ModeASync, "destroy", "-auto-approve", "-input=false", "-lock=false", "-json")
		if err != nil {
			err = tferrors.NewDestroyFailed(out)
		}
		w.LastOperation.MarkEnd()
		w.logger.Debug("destroy async ended", "out", w.filterFn(string(out)))
		defer func() {
			if cErr := callback(err, ctx); cErr != nil {
				w.logger.Info("callback failed", "error", cErr.Error())
			}
		}()
	}()
	return nil
}

// Destroy makes a blocking terraform destroy call.
func (w *Workspace) Destroy(ctx context.Context) error {
	if w.LastOperation.IsRunning() {
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	out, err := w.runTF(ctx, ModeSync, "destroy", "-auto-approve", "-input=false", "-lock=false", "-json")
	w.logger.Debug("destroy ended", "out", w.filterFn(string(out)))
	if err != nil {
		return tferrors.NewDestroyFailed(out)
	}
	return nil
}

// RefreshResult contains information about the current state of the resource.
type RefreshResult struct {
	Exists          bool
	ASyncInProgress bool
	State           *json.StateV4
}

// Refresh makes a blocking terraform apply -refresh-only call where only the state file
// is changed with the current state of the resource.
func (w *Workspace) Refresh(ctx context.Context) (RefreshResult, error) {
	switch {
	case w.LastOperation.IsRunning():
		return RefreshResult{
			ASyncInProgress: w.LastOperation.Type == "apply" || w.LastOperation.Type == "destroy",
		}, nil
	case w.LastOperation.IsEnded():
		defer w.LastOperation.Flush()
	}
	out, err := w.runTF(ctx, ModeSync, "apply", "-refresh-only", "-auto-approve", "-input=false", "-lock=false", "-json")
	w.logger.Debug("refresh ended", "out", w.filterFn(string(out)))
	if err != nil {
		return RefreshResult{}, tferrors.NewRefreshFailed(out)
	}
	raw, err := w.fs.ReadFile(filepath.Join(w.dir, "terraform.tfstate"))
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
	out, err := w.runTF(ctx, ModeSync, "plan", "-refresh=false", "-input=false", "-lock=false", "-json")
	w.logger.Debug("plan ended", "out", w.filterFn(string(out)))
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

// ImportResult contains information about the current state of the resource.
// Same as RefreshResult.
type ImportResult RefreshResult

// Import makes a blocking terraform import call where only the state file
// is changed with the current state of the resource.
func (w *Workspace) Import(ctx context.Context, tr resource.Terraformed) (ImportResult, error) { // nolint:gocyclo
	switch {
	case w.LastOperation.IsRunning():
		return ImportResult{
			ASyncInProgress: w.LastOperation.Type == "apply" || w.LastOperation.Type == "destroy",
		}, nil
	case w.LastOperation.IsEnded():
		defer w.LastOperation.Flush()
	}
	// Note(turkenh): This resource does not have an ID, we cannot import it. This happens with identifier from
	// provider case, and we simply return does not exist in this case.
	if len(w.terraformID) == 0 {
		return ImportResult{
			Exists: false,
		}, nil
	}

	// Note(turkenh): We remove the state file since the import command wouldn't work if tfstate contains
	// the resource already.
	if err := w.fs.Remove(filepath.Join(w.dir, "terraform.tfstate")); err != nil && !os.IsNotExist(err) {
		return ImportResult{}, errors.Wrap(err, "cannot remove terraform.tfstate file")
	}

	out, err := w.runTF(ctx, ModeSync, "import", "-input=false", "-lock=false", fmt.Sprintf("%s.%s", tr.GetTerraformResourceType(), tr.GetName()), w.terraformID)
	w.logger.Debug("import ended", "out", w.filterFn(string(out)))
	if err != nil {
		// Note(turkenh): This is not a great way to check if the resource does not exist, but it is the only
		// way we can do it for now. Terraform import does not return a proper exit code for this case or
		// does not support -json flag to parse the returning error in a better way.
		// https://github.com/hashicorp/terraform/blob/93f9cff99ffbb8d536b276a1be40a2c45ca4a67f/internal/terraform/node_resource_import.go#L235
		if strings.Contains(string(out), "Cannot import non-existent remote object") {
			return ImportResult{
				Exists: false,
			}, nil
		}
		return ImportResult{}, errors.WithMessage(errors.New("import failed"), w.filterFn(string(out)))
	}
	raw, err := w.fs.ReadFile(filepath.Join(w.dir, "terraform.tfstate"))
	if err != nil {
		return ImportResult{}, errors.Wrap(err, "cannot read terraform state file")
	}
	s := &json.StateV4{}
	if err := json.JSParser.Unmarshal(raw, s); err != nil {
		return ImportResult{}, errors.Wrap(err, "cannot unmarshal tfstate file")
	}
	return ImportResult{
		Exists: s.GetAttributes() != nil,
		State:  s,
	}, nil
}

func (w *Workspace) runTF(ctx context.Context, execMode ExecMode, args ...string) ([]byte, error) {
	if len(args) < 1 {
		return nil, errors.New("args cannot be empty")
	}
	w.logger.Debug("Running terraform", "args", args)
	if execMode == ModeSync {
		w.providerInUse.Increment()
	}
	defer w.providerInUse.Decrement()
	w.mu.Lock()
	defer w.mu.Unlock()
	cmd := w.executor.CommandContext(ctx, "terraform", args...)
	cmd.SetEnv(append(os.Environ(), w.env...))
	cmd.SetDir(w.dir)
	metrics.CLIExecutions.WithLabelValues(args[0], execMode.String()).Inc()
	start := time.Now()
	defer func() {
		metrics.CLITime.WithLabelValues(args[0], execMode.String()).Observe(time.Since(start).Seconds())
		metrics.CLIExecutions.WithLabelValues(args[0], execMode.String()).Dec()
	}()
	return cmd.CombinedOutput()
}
