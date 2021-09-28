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

package tfcli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/crossplane-contrib/terrajet/pkg/json"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
)

func (w *Workspace) Refresh(ctx context.Context) (model.RefreshResult, error) {
	if w.LastOperation.StartTime != nil {
		// The last operation is still ongoing.
		if w.LastOperation.EndTime == nil {
			return model.RefreshResult{
				IsCreating:   w.LastOperation.Type == "apply",
				IsDestroying: w.LastOperation.Type == "destroy",
			}, nil
		}
		// We know that the operation finished, so we need to flush so that new
		// operation can be started.
		defer w.LastOperation.Flush()

		// The last operation finished with error.
		if w.LastOperation.err != nil {
			return model.RefreshResult{
				IsCreating:         w.LastOperation.Type == "apply",
				IsDestroying:       w.LastOperation.Type == "destroy",
				LastOperationError: errors.Wrapf(w.LastOperation.err, "%s operation failed", w.LastOperation.Type),
			}, nil
		}
		// The deletion is completed so there is no resource to refresh.
		if w.LastOperation.Type == "destroy" {
			return model.RefreshResult{}, kerrors.NewNotFound(schema.GroupResource{}, "")
		}
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := exec.CommandContext(ctx, "terraform", "apply", "-refresh-only", "-auto-approve", "-input=false", "-no-color", "-detailed-exitcode", "-json")
	cmd.Dir = w.dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return model.RefreshResult{}, errors.Wrapf(err, "cannot refresh: %s", stderr.String())
	}
	raw, err := os.ReadFile(filepath.Join(w.dir, "terraform.tfstate"))
	if err != nil {
		return model.RefreshResult{}, errors.Wrap(err, "cannot read terraform state file")
	}
	s := &json.StateV4{}
	if err := json.JSParser.Unmarshal(raw, s); err != nil {
		return model.RefreshResult{}, errors.Wrap(err, "cannot unmarshal tfstate file")
	}
	return model.RefreshResult{State: s}, nil
}
