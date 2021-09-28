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
	"os/exec"

	"github.com/pkg/errors"
)

func (w *Workspace) Destroy(_ context.Context) error {
	switch {
	// Destroy call is idempotent and can be called repeatedly.
	case w.LastOperation.Type == "destroy":
		return nil
	// We cannot run destroy until current non-destroy operation is completed.
	// TODO(muvaf): Gracefully terminate the ongoing apply operation?
	case w.LastOperation.Type != "destroy" && w.LastOperation.EndTime == nil:
		return errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime.String())
	}
	w.LastOperation.MarkStart("destroy")
	ctx, cancel := context.WithDeadline(context.TODO(), w.LastOperation.StartTime.Add(defaultAsyncTimeout))
	go func() {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd := exec.CommandContext(ctx, "terraform", "destroy", "-auto-approve", "-input=false", "-no-color", "-detailed-exitcode", "-json")
		cmd.Dir = w.dir
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			w.LastOperation.err = errors.Wrapf(err, "cannot destroy: %s", stderr.String())
		}
		w.LastOperation.MarkEnd()

		// After the operation is completed, we need to get the results saved on
		// the custom resource as soon as possible. We can wait for the next
		// reconcilitaion, enqueue manually or update the CR independent of the
		// reconciliation.
		w.Enqueue()
		cancel()
	}()
	return nil
}
