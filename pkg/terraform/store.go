/*
Copyright 2021 Upbound Inc.
*/

package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/resource"
)

const (
	fmtEnv = "%s=%s"
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

// WithProviderRunner sets the ProviderRunner to be used.
func WithProviderRunner(pr ProviderRunner) WorkspaceStoreOption {
	return func(ws *WorkspaceStore) {
		ws.providerRunner = pr
	}
}

// NewWorkspaceStore returns a new WorkspaceStore.
func NewWorkspaceStore(l logging.Logger, opts ...WorkspaceStoreOption) *WorkspaceStore {
	ws := &WorkspaceStore{
		store:          map[types.UID]*Workspace{},
		logger:         l,
		mu:             sync.Mutex{},
		fs:             afero.Afero{Fs: afero.NewOsFs()},
		executor:       exec.New(),
		providerRunner: NewNoOpProviderRunner(),
	}
	for _, f := range opts {
		f(ws)
	}
	return ws
}

// WorkspaceStore allows you to manage multiple Terraform workspaces.
type WorkspaceStore struct {
	// store holds information about ongoing operations of given resource.
	// Since there can be multiple calls that add/remove values from the map at
	// the same time, it has to be safe for concurrency since those operations
	// cause rehashing in some cases.
	store          map[types.UID]*Workspace
	logger         logging.Logger
	providerRunner ProviderRunner
	mu             sync.Mutex

	fs       afero.Afero
	executor exec.Interface
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

	if err = fp.EnsureTFState(ctx); err != nil {
		return nil, errors.Wrap(err, "cannot ensure tfstate file")
	}
	w.trID, err = fp.Config.ExternalName.GetIDFn(ctx, meta.GetExternalName(fp.Resource), fp.parameters, fp.Setup.Map())
	if err != nil {
		return nil, errors.Wrap(err, errGetID)
	}
	isNeedProviderUpgrade, err := fp.needProviderUpgrade()
	if err != nil {
		return nil, errors.Wrap(err, "cannot check if a Terraform dependency update is required")
	}
	if err := fp.WriteMainTF(); err != nil {
		return nil, errors.Wrap(err, "cannot write main tf file")
	}
	if isNeedProviderUpgrade {
		cmd := w.executor.CommandContext(ctx, "terraform", "init", "-upgrade", "-input=false")
		cmd.SetDir(w.dir)
		out, err := cmd.CombinedOutput()
		w.logger.Debug("init -upgrade ended", "out", string(out))
		if err != nil {
			return w, errors.Wrapf(err, "cannot upgrade workspace: %s", string(out))
		}
	}
	attachmentConfig, err := ws.providerRunner.Start()
	if err != nil {
		return nil, err
	}
	_, err = ws.fs.Stat(filepath.Join(dir, ".terraform.lock.hcl"))
	if xpresource.Ignore(os.IsNotExist, err) != nil {
		return nil, errors.Wrap(err, "cannot stat init lock file")
	}
	w.env = append(w.env, fmt.Sprintf(fmtEnv, envReattachConfig, attachmentConfig))

	// We need to initialize only if the workspace hasn't been initialized yet.
	if !os.IsNotExist(err) {
		return w, nil
	}
	cmd := w.executor.CommandContext(ctx, "terraform", "init", "-input=false")
	cmd.SetDir(w.dir)
	out, err := cmd.CombinedOutput()
	w.logger.Debug("init ended", "out", string(out))
	return w, errors.Wrapf(err, "cannot init workspace: %s", string(out))
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

func (ts Setup) filterSensitiveInformation(s string) string {
	for _, v := range ts.Configuration {
		if str, ok := v.(string); ok && str != "" {
			s = strings.ReplaceAll(s, str, "REDACTED")
		}
	}
	return s
}
