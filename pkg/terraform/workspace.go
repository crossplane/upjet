/*
Copyright 2021 Upbound Inc.
*/

package terraform

import (
	"context"
	"fmt"
	"github.com/upbound/upjet/pkg/resource"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/afero"
	k8sExec "k8s.io/utils/exec"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/upbound/upjet/pkg/resource/json"
	tferrors "github.com/upbound/upjet/pkg/terraform/errors"
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

func WithFilterFn(filterFn func(string) string) WorkspaceOption {
	return func(w *Workspace) {
		w.filterFn = filterFn
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

	trID string

	dir string
	env []string

	logger   logging.Logger
	executor k8sExec.Interface
	fs       afero.Afero

	filterFn func(string) string
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
		cmd := w.executor.CommandContext(ctx, "terraform", "apply", "-auto-approve", "-input=false", "-lock=false", "-json")
		cmd.SetEnv(append(os.Environ(), w.env...))
		cmd.SetDir(w.dir)
		out, err := cmd.CombinedOutput()
		w.LastOperation.MarkEnd()
		w.logger.Debug("apply async ended", "out", w.filterFn(string(out)))
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
	cmd := w.executor.CommandContext(ctx, "terraform", "apply", "-auto-approve", "-input=false", "-lock=false", "-json")
	cmd.SetEnv(append(os.Environ(), w.env...))
	cmd.SetDir(w.dir)
	out, err := cmd.CombinedOutput()
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
	case w.LastOperation.IsRunning():
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime().String())
	}
	w.LastOperation.MarkStart("destroy")
	ctx, cancel := context.WithDeadline(context.TODO(), w.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()
		cmd := w.executor.CommandContext(ctx, "terraform", "destroy", "-auto-approve", "-input=false", "-lock=false", "-json")
		cmd.SetEnv(append(os.Environ(), w.env...))
		cmd.SetDir(w.dir)
		out, err := cmd.CombinedOutput()
		w.LastOperation.MarkEnd()
		w.logger.Debug("destroy async ended", "out", w.filterFn(string(out)))
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
	cmd := w.executor.CommandContext(ctx, "terraform", "destroy", "-auto-approve", "-input=false", "-lock=false", "-json")
	cmd.SetEnv(append(os.Environ(), w.env...))
	cmd.SetDir(w.dir)
	out, err := cmd.CombinedOutput()
	w.logger.Debug("destroy ended", "out", w.filterFn(string(out)))
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
	cmd := w.executor.CommandContext(ctx, "terraform", "apply", "-refresh-only", "-auto-approve", "-input=false", "-lock=false", "-json")
	cmd.SetEnv(append(os.Environ(), w.env...))
	cmd.SetDir(w.dir)
	out, err := cmd.CombinedOutput()
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

// Import makes a blocking terraform import call where only the state file
// is changed with the current state of the resource.
func (w *Workspace) Import(ctx context.Context, tr resource.Terraformed) (RefreshResult, error) {
	switch {
	case w.LastOperation.IsRunning():
		return RefreshResult{
			IsApplying:   w.LastOperation.Type == "apply",
			IsDestroying: w.LastOperation.Type == "destroy",
		}, nil
	case w.LastOperation.IsEnded():
		defer w.LastOperation.Flush()
	}
	// Note(turkenh): This resource does not have an ID, we cannot import it. This happens with identifier from
	// provider case and we simply return does not exist in this case.
	if len(w.trID) == 0 {
		return RefreshResult{
			Exists: false,
		}, nil
	}
	// Note(turkenh): We remove the state file before import wouldn't work if tfstate contains the resource already.
	if err := w.fs.Remove(filepath.Join(w.dir, "terraform.tfstate")); err != nil {
		return RefreshResult{}, errors.Wrap(err, "cannot remove terraform.tfstate file")
	}
	cmd := w.executor.CommandContext(ctx, "terraform", "import", "-input=false", "-lock=false", fmt.Sprintf("%s.%s", tr.GetTerraformResourceType(), tr.GetName()), w.trID)
	cmd.SetEnv(append(os.Environ(), w.env...))
	cmd.SetDir(w.dir)
	out, err := cmd.CombinedOutput()
	w.logger.Debug("import ended", "out", w.filterFn(string(out)))
	if err != nil {
		if strings.Contains(string(out), "Cannot import non-existent remote object") {
			return RefreshResult{
				Exists: false,
			}, nil
		}
		return RefreshResult{}, errors.WithMessage(errors.New("import failed"), string(out))
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
	cmd := w.executor.CommandContext(ctx, "terraform", "plan", "-refresh=false", "-input=false", "-lock=false", "-json")
	cmd.SetEnv(append(os.Environ(), w.env...))
	cmd.SetDir(w.dir)
	out, err := cmd.CombinedOutput()
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
