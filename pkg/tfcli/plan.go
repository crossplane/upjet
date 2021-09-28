package tfcli

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/json"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli/model"
)

func (w *Workspace) Plan(ctx context.Context) (model.PlanResult, error) {
	// The last operation is still ongoing.
	if w.LastOperation.StartTime != nil && w.LastOperation.EndTime == nil {
		return model.PlanResult{}, errors.Errorf("%s operation that started at %s is still running", w.LastOperation.Type, w.LastOperation.StartTime.String())
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := exec.CommandContext(ctx, "terraform", "plan", "-refresh=false", "-input=false", "-no-color", "-detailed-exitcode", "-json")
	cmd.Dir = w.dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return model.PlanResult{}, errors.Wrapf(err, "cannot plan: %s", stderr.String())
	}
	line := ""
	for _, l := range strings.Split(stdout.String(), "\n") {
		if strings.Contains(l, `"type":"change_summary"`) {
			line = l
			break
		}
	}
	if line == "" {
		return model.PlanResult{}, errors.Errorf("cannot find the change summary line in plan log: %s", stdout.String())
	}
	type plan struct {
		Changes struct {
			Add    float64 `json:"add,omitempty"`
			Change float64 `json:"change,omitempty"`
		} `json:"changes,omitempty"`
	}
	p := &plan{}
	if err := json.JSParser.Unmarshal([]byte(line), p); err != nil {
		return model.PlanResult{}, errors.Wrap(err, "cannot unmarshal change summary json")
	}
	return model.PlanResult{
		Exists:   p.Changes.Add == 0,
		UpToDate: p.Changes.Change == 0,
	}, nil
}
