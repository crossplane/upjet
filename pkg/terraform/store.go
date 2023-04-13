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
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/mitchellh/go-ps"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/metrics"
	"github.com/upbound/upjet/pkg/resource"
)

const (
	errGetID = "cannot get id"
)

// SetupFn is a function that returns Terraform setup which contains
// provider requirement, configuration and Terraform version.
type SetupFn func(ctx context.Context, client client.Client, mg xpresource.Managed) (Setup, error)

// ProviderRequirement holds values for the Terraform HCL setup requirements
type ProviderRequirement struct {
	// Source of the provider. An example value is "hashicorp/aws".
	Source string

	// Version of the provider. An example value is "4.0"
	Version string
}

// ProviderConfiguration holds the setup configuration body
type ProviderConfiguration map[string]any

// ToProviderHandle converts a provider configuration to a handle
// for the provider scheduler.
func (pc ProviderConfiguration) ToProviderHandle() (ProviderHandle, error) {
	h := strings.Join(getSortedKeyValuePairs("", pc), ",")
	hash := sha256.New()
	if _, err := hash.Write([]byte(h)); err != nil {
		return InvalidProviderHandle, errors.Wrap(err, "cannot convert provider configuration to scheduler handle")
	}
	return ProviderHandle(fmt.Sprintf("%x", hash.Sum(nil))), nil
}

func getSortedKeyValuePairs(parent string, m map[string]any) []string {
	result := make([]string, 0, len(m))
	sortedKeys := make([]string, 0, len(m))
	for k := range m {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	for _, k := range sortedKeys {
		v := m[k]
		switch t := v.(type) {
		case []string:
			result = append(result, fmt.Sprintf("%q:%q", parent+k, strings.Join(t, ",")))
		case map[string]any:
			result = append(result, getSortedKeyValuePairs(parent+k+".", t)...)
		case []map[string]any:
			cArr := make([]string, 0, len(t))
			for i, e := range t {
				cArr = append(cArr, getSortedKeyValuePairs(fmt.Sprintf("%s%s[%d].", parent, k, i), e)...)
			}
			result = append(result, fmt.Sprintf("%q:%q", parent+k, strings.Join(cArr, ",")))
		case *string:
			if t != nil {
				result = append(result, fmt.Sprintf("%q:%q", parent+k, *t))
			}
		default:
			result = append(result, fmt.Sprintf("%q:%q", parent+k, t))
		}
	}
	return result
}

// Setup holds values for the Terraform version and setup
// requirements and configuration body
type Setup struct {
	// Version is the version of Terraform that this workspace would require as
	// minimum.
	Version string

	// Requirement contains the provider requirements of the workspace to work,
	// which is mostly the version and source of the provider.
	Requirement ProviderRequirement

	// Configuration contains the provider configuration parameters of the given
	// Terraform provider, such as access token.
	Configuration ProviderConfiguration

	// ClientMetadata contains arbitrary metadata that the provider would like
	// to pass but not available as part of Terraform's provider configuration.
	// For example, AWS account id is needed for certain ID calculations but is
	// not part of the Terraform AWS Provider configuration, so it could be
	// made available only by this map.
	ClientMetadata map[string]string

	// Scheduler specifies the provider scheduler to be used for the Terraform
	// workspace being setup. If not set, no scheduler is configured and
	// the lifecycle of Terraform provider processes will be managed by
	// the Terraform CLI.
	Scheduler ProviderScheduler
}

// Map returns the Setup object in map form. The initial reason was so that
// we don't import the terraform package in places where GetIDFn is overridden
// because it can cause circular dependency.
func (s Setup) Map() map[string]any {
	return map[string]any{
		"version": s.Version,
		"requirement": map[string]string{
			"source":  s.Requirement.Source,
			"version": s.Requirement.Version,
		},
		"configuration":   s.Configuration,
		"client_metadata": s.ClientMetadata,
	}
}

// WorkspaceStoreOption lets you configure the workspace store.
type WorkspaceStoreOption func(*WorkspaceStore)

// WithFs lets you set the fs of WorkspaceStore. Used mostly for testing.
func WithFs(fs afero.Fs) WorkspaceStoreOption {
	return func(ws *WorkspaceStore) {
		ws.fs = afero.Afero{Fs: fs}
	}
}

// WithProcessReportInterval enables the upjet.terraform.running_processes
// metric, which periodically reports the total number of Terraform CLI and
// Terraform provider processes in the system.
func WithProcessReportInterval(d time.Duration) WorkspaceStoreOption {
	return func(ws *WorkspaceStore) {
		ws.processReportInterval = d
	}
}

// WithDisableInit disables `terraform init` invocations in case
// workspace initialization is not needed (e.g., when using the
// shared gRPC server runtime).
func WithDisableInit(disable bool) WorkspaceStoreOption {
	return func(ws *WorkspaceStore) {
		ws.disableInit = disable
	}
}

// NewWorkspaceStore returns a new WorkspaceStore.
func NewWorkspaceStore(l logging.Logger, opts ...WorkspaceStoreOption) *WorkspaceStore {
	ws := &WorkspaceStore{
		store:    map[types.UID]*Workspace{},
		logger:   l,
		mu:       sync.Mutex{},
		fs:       afero.Afero{Fs: afero.NewOsFs()},
		executor: exec.New(),
	}
	for _, f := range opts {
		f(ws)
	}
	ws.initMetrics()
	if ws.processReportInterval != 0 {
		go ws.reportTFProcesses(ws.processReportInterval)
	}
	return ws
}

// WorkspaceStore allows you to manage multiple Terraform workspaces.
type WorkspaceStore struct {
	// store holds information about ongoing operations of given resource.
	// Since there can be multiple calls that add/remove values from the map at
	// the same time, it has to be safe for concurrency since those operations
	// cause rehashing in some cases.
	store                 map[types.UID]*Workspace
	logger                logging.Logger
	mu                    sync.Mutex
	processReportInterval time.Duration
	fs                    afero.Afero
	executor              exec.Interface
	disableInit           bool
}

// Workspace makes sure the Terraform workspace for the given resource is ready
// to be used and returns the Workspace object configured to work in that
// workspace folder in the filesystem.
func (ws *WorkspaceStore) Workspace(ctx context.Context, c resource.SecretClient, tr resource.Terraformed, ts Setup, cfg *config.Resource) (*Workspace, error) { //nolint:gocyclo
	dir := filepath.Join(ws.fs.GetTempDir(""), string(tr.GetUID()))
	if err := ws.fs.MkdirAll(dir, os.ModePerm); err != nil {
		return nil, errors.Wrap(err, "cannot create directory for workspace")
	}
	ws.mu.Lock()
	w, ok := ws.store[tr.GetUID()]
	if !ok {
		l := ws.logger.WithValues("workspace", dir)
		ws.store[tr.GetUID()] = NewWorkspace(dir, WithLogger(l), WithExecutor(ws.executor), WithFilterFn(ts.filterSensitiveInformation))
		w = ws.store[tr.GetUID()]
	}
	ws.mu.Unlock()
	// If there is an ongoing operation, no changes should be made in the
	// workspace files.
	if w.LastOperation.IsRunning() {
		return w, nil
	}
	fp, err := NewFileProducer(ctx, c, dir, tr, ts, cfg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create a new file producer")
	}

	w.terraformID, err = fp.Config.ExternalName.GetIDFn(ctx, meta.GetExternalName(fp.Resource), fp.parameters, fp.Setup.Map())
	if err != nil {
		return nil, errors.Wrap(err, errGetID)
	}

	if err := fp.EnsureTFState(ctx, w.terraformID); err != nil {
		return nil, errors.Wrap(err, "cannot ensure tfstate file")
	}

	isNeedProviderUpgrade := false
	if !ws.disableInit {
		isNeedProviderUpgrade, err = fp.needProviderUpgrade()
		if err != nil {
			return nil, errors.Wrap(err, "cannot check if a Terraform dependency update is required")
		}
	}

	if w.ProviderHandle, err = fp.WriteMainTF(); err != nil {
		return nil, errors.Wrap(err, "cannot write main tf file")
	}
	if isNeedProviderUpgrade {
		out, err := w.runTF(ctx, ModeSync, "init", "-upgrade", "-input=false")
		w.logger.Debug("init -upgrade ended", "out", ts.filterSensitiveInformation(string(out)))
		if err != nil {
			return w, errors.Wrapf(err, "cannot upgrade workspace: %s", ts.filterSensitiveInformation(string(out)))
		}
	}
	if ws.disableInit {
		return w, nil
	}
	_, err = ws.fs.Stat(filepath.Join(dir, ".terraform.lock.hcl"))
	if xpresource.Ignore(os.IsNotExist, err) != nil {
		return nil, errors.Wrap(err, "cannot stat init lock file")
	}
	// We need to initialize only if the workspace hasn't been initialized yet.
	if !os.IsNotExist(err) {
		return w, nil
	}
	out, err := w.runTF(ctx, ModeSync, "init", "-input=false")
	w.logger.Debug("init ended", "out", ts.filterSensitiveInformation(string(out)))
	return w, errors.Wrapf(err, "cannot init workspace: %s", ts.filterSensitiveInformation(string(out)))
}

// Remove deletes the workspace directory from the filesystem and erases its
// record from the store.
func (ws *WorkspaceStore) Remove(obj xpresource.Object) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	w, ok := ws.store[obj.GetUID()]
	if !ok {
		return nil
	}
	if err := ws.fs.RemoveAll(w.dir); err != nil {
		return errors.Wrap(err, "cannot remove workspace folder")
	}
	delete(ws.store, obj.GetUID())
	return nil
}

func (ws *WorkspaceStore) initMetrics() {
	for _, mode := range []ExecMode{ModeSync, ModeASync} {
		for _, subcommand := range []string{"init", "apply", "destroy", "plan"} {
			metrics.CLIExecutions.WithLabelValues(subcommand, mode.String()).Set(0)
		}
	}
}

func (ts Setup) filterSensitiveInformation(s string) string {
	for _, v := range ts.Configuration {
		if str, ok := v.(string); ok && str != "" {
			s = strings.ReplaceAll(s, str, "REDACTED")
		}
	}
	return s
}

func (ws *WorkspaceStore) reportTFProcesses(interval time.Duration) {
	for _, t := range []string{"cli", "provider"} {
		metrics.TFProcesses.WithLabelValues(t).Set(0)
	}
	t := time.NewTicker(interval)
	for range t.C {
		processes, err := ps.Processes()
		if err != nil {
			ws.logger.Debug("Failed to list processes", "err", err)
			continue
		}
		cliCount, providerCount := 0.0, 0.0
		for _, p := range processes {
			e := p.Executable()
			switch {
			case e == "terraform":
				cliCount++
			case strings.HasPrefix(e, "terraform-"):
				providerCount++
			}
		}
		metrics.TFProcesses.WithLabelValues("cli").Set(cliCount)
		metrics.TFProcesses.WithLabelValues("provider").Set(providerCount)
	}
}
