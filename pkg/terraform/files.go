// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package terraform

import (
	"context"
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"strings"

	"dario.cat/mergo"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/feature"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"github.com/spf13/afero"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/resource"
	"github.com/crossplane/upjet/pkg/resource/json"
)

const (
	errWriteTFStateFile  = "cannot write terraform.tfstate file"
	errWriteMainTFFile   = "cannot write main.tf.json file"
	errCheckIfStateEmpty = "cannot check whether the state is empty"
	errMarshalAttributes = "cannot marshal produced state attributes"
	errInsertTimeouts    = "cannot insert timeouts metadata to private raw"
	errReadTFState       = "cannot read terraform.tfstate file"
	errMarshalState      = "cannot marshal state object"
	errUnmarshalAttr     = "cannot unmarshal state attributes"
	errUnmarshalTFState  = "cannot unmarshal tfstate file"
	errFmtNonString      = "cannot work with a non-string id: %s"
	errReadMainTF        = "cannot read main.tf.json file"

	defaultRegistry = `provider["registry.terraform.io/%s"]`
)

// FileProducerOption allows you to configure FileProducer
type FileProducerOption func(*FileProducer)

// WithFileSystem configures the filesystem to use. Used mostly for testing.
func WithFileSystem(fs afero.Fs) FileProducerOption {
	return func(fp *FileProducer) {
		fp.fs = afero.Afero{Fs: fs}
	}
}

// WithFileProducerFeatures configures the active features for the FileProducer.
func WithFileProducerFeatures(f *feature.Flags) FileProducerOption {
	return func(fp *FileProducer) {
		fp.features = f
	}
}

// NewFileProducer returns a new FileProducer.
func NewFileProducer(ctx context.Context, client resource.SecretClient, dir string, tr resource.Terraformed, ts Setup, cfg *config.Resource, opts ...FileProducerOption) (*FileProducer, error) {
	fp := &FileProducer{
		Resource: tr,
		Setup:    ts,
		Dir:      dir,
		Config:   cfg,
		fs:       afero.Afero{Fs: afero.NewOsFs()},
		features: &feature.Flags{},
	}
	for _, f := range opts {
		f(fp)
	}

	params, err := tr.GetParameters()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get parameters")
	}

	// Note(lsviben):We need to check if the management policies feature is
	// enabled before attempting to get the ignorable fields or merge them
	// with the forProvider fields.
	if fp.features.Enabled(feature.EnableBetaManagementPolicies) {
		initParams, err := tr.GetInitParameters()
		if err != nil {
			return nil, errors.Wrapf(err, "cannot get the init parameters for the resource %q", tr.GetName())
		}

		// get fields which should be in the ignore_changes lifecycle block
		fp.ignored = resource.GetTerraformIgnoreChanges(params, initParams)

		// Note(lsviben): mergo.WithSliceDeepCopy is needed to merge the
		// slices from the initProvider to forProvider. As it also sets
		// overwrite to true, we need to set it back to false, we don't
		// want to overwrite the forProvider fields with the initProvider
		// fields.
		err = mergo.Merge(&params, initParams, mergo.WithSliceDeepCopy, func(c *mergo.Config) {
			c.Overwrite = false
		})
		if err != nil {
			return nil, errors.Wrapf(err, "cannot merge the spec.initProvider and spec.forProvider parameters for the resource %q", tr.GetName())
		}
	}

	if err = resource.GetSensitiveParameters(ctx, client, tr, params, tr.GetConnectionDetailsMapping()); err != nil {
		return nil, errors.Wrap(err, "cannot get sensitive parameters")
	}
	fp.Config.ExternalName.SetIdentifierArgumentFn(params, meta.GetExternalName(tr))
	fp.parameters = params

	obs, err := tr.GetObservation()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get observation")
	}

	secretRef, err := getConnectionSecretRef(tr)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get connection secret ref")
	}
	if err = resource.GetSensitiveObservation(ctx, client, secretRef, obs); err != nil {
		return nil, errors.Wrap(err, "cannot get sensitive observation")
	}
	fp.observation = obs

	return fp, nil
}

func getConnectionSecretRef(tr resource.Terraformed) (*xpv1.SecretReference, error) {
	switch trt := tr.(type) {
	case xpresource.ConnectionSecretWriterTo:
		return trt.GetWriteConnectionSecretToReference(), nil
	case xpresource.LocalConnectionSecretWriterTo:
		if trt.GetWriteConnectionSecretToReference() == nil {
			return nil, nil
		}
		return &xpv1.SecretReference{
			Name:      trt.GetWriteConnectionSecretToReference().Name,
			Namespace: tr.GetNamespace(),
		}, nil
	}
	return nil, errors.New("unknown managed resource type")
}

// FileProducer exist to serve as cache for the data that is costly to produce
// every time like parameters and observation maps.
type FileProducer struct {
	Resource resource.Terraformed
	Setup    Setup
	Dir      string
	Config   *config.Resource

	parameters  map[string]any
	observation map[string]any
	ignored     []string
	fs          afero.Afero
	features    *feature.Flags
}

// BuildMainTF produces the contents of the mainTF file as a map.  This format is conducive to
// inspection for tests.  WriteMainTF calls this function an serializes the result to a file as JSON.
func (fp *FileProducer) BuildMainTF() map[string]any {
	// If the resource is in a deletion process, we need to remove the deletion
	// protection.
	lifecycle := map[string]any{
		"prevent_destroy": !meta.WasDeleted(fp.Resource),
	}

	if len(fp.ignored) != 0 {
		lifecycle["ignore_changes"] = fp.ignored
	}

	fp.parameters["lifecycle"] = lifecycle

	// Add operation timeouts if any timeout configured for the resource
	if tp := timeouts(fp.Config.OperationTimeouts).asParameter(); len(tp) != 0 {
		fp.parameters["timeouts"] = tp
	}

	// Note(turkenh): To use third party providers, we need to configure
	// provider name in required_providers.
	providerSource := strings.Split(fp.Setup.Requirement.Source, "/")
	return map[string]any{
		"terraform": map[string]any{
			"required_providers": map[string]any{
				providerSource[len(providerSource)-1]: map[string]string{
					"source":  fp.Setup.Requirement.Source,
					"version": fp.Setup.Requirement.Version,
				},
			},
		},
		"provider": map[string]any{
			providerSource[len(providerSource)-1]: fp.Setup.Configuration,
		},
		"resource": map[string]any{
			fp.Resource.GetTerraformResourceType(): map[string]any{
				fp.Resource.GetName(): fp.parameters,
			},
		},
	}
}

// WriteMainTF writes the content main configuration file that has the desired
// state configuration for Terraform.
func (fp *FileProducer) WriteMainTF() (ProviderHandle, error) {
	m := fp.BuildMainTF()
	rawMainTF, err := json.JSParser.Marshal(m)
	if err != nil {
		return InvalidProviderHandle, errors.Wrap(err, "cannot marshal main hcl object")
	}
	h, err := fp.Setup.Configuration.ToProviderHandle()
	if err != nil {
		return InvalidProviderHandle, errors.Wrap(err, "cannot get scheduler handle")
	}
	return h, errors.Wrap(fp.fs.WriteFile(filepath.Join(fp.Dir, "main.tf.json"), rawMainTF, 0600), errWriteMainTFFile)
}

// EnsureTFState writes the Terraform state that should exist in the filesystem
// to start any Terraform operation.
func (fp *FileProducer) EnsureTFState(_ context.Context, tfID string) error { //nolint:gocyclo // easier to follow as a unit
	// TODO(muvaf): Reduce the cyclomatic complexity by separating the attributes
	// generation into its own function/interface.
	empty, err := fp.isStateEmpty()
	if err != nil {
		return errors.Wrap(err, errCheckIfStateEmpty)
	}
	// We don't fill up the TF state during deletion because Terraform's removal
	// of them from the TF state file signals that the deletion was successful.
	// This is especially useful for resources whose deletion are scheduled for
	// a long period of time, where if we fill the ID, the queries would actually
	// succeed, i.e. GCP KMS KeyRing.
	if !empty || meta.WasDeleted(fp.Resource) {
		return nil
	}
	base := make(map[string]any)
	// NOTE(muvaf): Since we try to produce the current state, observation
	// takes precedence over parameters.
	for k, v := range fp.parameters {
		base[k] = v
	}
	for k, v := range fp.observation {
		base[k] = v
	}
	base["id"] = tfID
	attr, err := json.JSParser.Marshal(base)
	if err != nil {
		return errors.Wrap(err, errMarshalAttributes)
	}
	var privateRaw []byte
	if pr, ok := fp.Resource.GetAnnotations()[resource.AnnotationKeyPrivateRawAttribute]; ok {
		privateRaw = []byte(pr)
	}
	if privateRaw, err = insertTimeoutsMeta(privateRaw, timeouts(fp.Config.OperationTimeouts)); err != nil {
		return errors.Wrap(err, errInsertTimeouts)
	}
	s := json.NewStateV4()
	s.TerraformVersion = fp.Setup.Version
	s.Lineage = string(fp.Resource.GetUID())

	registry := fp.Setup.Requirement.Registry
	if registry == "" {
		registry = defaultRegistry
	}

	s.Resources = []json.ResourceStateV4{
		{
			Mode: "managed",
			Type: fp.Resource.GetTerraformResourceType(),
			Name: fp.Resource.GetName(),
			// Support for private/non-default registries
			ProviderConfig: fmt.Sprintf(registry, fp.Setup.Requirement.Source),
			Instances: []json.InstanceObjectStateV4{
				{
					SchemaVersion: uint64(fp.Resource.GetTerraformSchemaVersion()), //nolint:gosec
					PrivateRaw:    privateRaw,
					AttributesRaw: attr,
				},
			},
		},
	}

	rawState, err := json.JSParser.Marshal(s)
	if err != nil {
		return errors.Wrap(err, errMarshalState)
	}
	return errors.Wrap(fp.fs.WriteFile(filepath.Join(fp.Dir, "terraform.tfstate"), rawState, 0600), errWriteTFStateFile)
}

// isStateEmpty returns whether the Terraform state includes a resource or not.
func (fp *FileProducer) isStateEmpty() (bool, error) {
	data, err := fp.fs.ReadFile(filepath.Join(fp.Dir, "terraform.tfstate"))
	if errors.Is(err, iofs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, errors.Wrap(err, errReadTFState)
	}
	s := &json.StateV4{}
	if err := json.JSParser.Unmarshal(data, s); err != nil {
		return false, errors.Wrap(err, errUnmarshalTFState)
	}
	attrData := s.GetAttributes()
	if attrData == nil {
		return true, nil
	}
	attr := map[string]any{}
	if err := json.JSParser.Unmarshal(attrData, &attr); err != nil {
		return false, errors.Wrap(err, errUnmarshalAttr)
	}
	id, ok := attr["id"]
	if !ok {
		return true, nil
	}
	sid, ok := id.(string)
	if !ok {
		return false, errors.Errorf(errFmtNonString, fmt.Sprint(id))
	}
	return sid == "", nil
}

type MainConfiguration struct {
	Terraform Terraform `json:"terraform,omitempty"`
}

type Terraform struct {
	RequiredProviders map[string]any `json:"required_providers,omitempty"`
}

func (fp *FileProducer) needProviderUpgrade() (bool, error) {
	data, err := fp.fs.ReadFile(filepath.Join(fp.Dir, "main.tf.json"))
	if errors.Is(err, iofs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, errReadMainTF)
	}
	mainConfiguration := MainConfiguration{}
	if err := json.JSParser.Unmarshal(data, &mainConfiguration); err != nil {
		return false, errors.Wrap(err, errReadMainTF)
	}
	providerSource := strings.Split(fp.Setup.Requirement.Source, "/")
	providerConfiguration, ok := mainConfiguration.Terraform.RequiredProviders[providerSource[len(providerSource)-1]]
	if !ok {
		return false, errors.New("cannot get provider configuration")
	}
	v, ok := providerConfiguration.(map[string]any)["version"]
	if !ok {
		return false, errors.New("cannot get version")
	}
	return v != fp.Setup.Requirement.Version, nil
}
