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

	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"

	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/json"
)

func (w *Workspace) ApplyAsync(_ context.Context) error {
	if w.LastOperation.EndTime == nil {
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime.String())
	}
	w.LastOperation.MarkStart("apply")
	ctx, cancel := context.WithDeadline(context.TODO(), w.LastOperation.StartTime.Add(defaultAsyncTimeout))
	go func() {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd := exec.CommandContext(ctx, "terraform", "apply", "-auto-approve", "-input=false", "-no-color", "-detailed-exitcode", "-json")
		cmd.Dir = w.dir
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			w.LastOperation.err = errors.Wrapf(err, "cannot apply: %s", stderr.String())
		}
		w.LastOperation.MarkEnd()

		// After the operation is completed, we need to get the results saved on
		// the custom resource as soon as possible. We can wait for the next
		// reconciliation, enqueue manually or update the CR independent of the
		// reconciliation.
		w.Enqueue()
		cancel()
	}()
	return nil
}

func (w *Workspace) Apply(ctx context.Context) (model.ApplyResult, error) {
	if w.LastOperation.EndTime == nil {
		return model.ApplyResult{}, errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime.String())
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := exec.CommandContext(ctx, "terraform", "apply", "-auto-approve", "-input=false", "-no-color", "-detailed-exitcode", "-json")
	cmd.Dir = w.dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return model.ApplyResult{}, errors.Wrapf(err, "cannot apply: %s", stderr.String())
	}
	raw, err := os.ReadFile(filepath.Join(w.dir, "terraform.tfstate"))
	if err != nil {
		return model.ApplyResult{}, errors.Wrap(err, "cannot read terraform state file")
	}
	s := &json.StateV4{}
	if err := json.JSParser.Unmarshal(raw, s); err != nil {
		return model.ApplyResult{}, errors.Wrap(err, "cannot unmarshal tfstate file")
	}
	return model.ApplyResult{State: s}, nil
}
