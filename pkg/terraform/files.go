/*
Copyright 2021 Upbound Inc.
*/

package terraform

import (
	"context"
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/pkg/errors"
	"github.com/spf13/afero"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/resource/json"
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
)

// FileProducerOption allows you to configure FileProducer
type FileProducerOption func(*FileProducer)

// WithFileSystem configures the filesystem to use. Used mostly for testing.
func WithFileSystem(fs afero.Fs) FileProducerOption {
	return func(fp *FileProducer) {
		fp.fs = afero.Afero{Fs: fs}
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
	}
	for _, f := range opts {
		f(fp)
	}
	params, err := tr.GetParameters()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get parameters")
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
	if err = resource.GetSensitiveObservation(ctx, client, tr.GetWriteConnectionSecretToReference(), obs); err != nil {
		return nil, errors.Wrap(err, "cannot get sensitive observation")
	}
	fp.observation = obs

	return fp, nil
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
	fs          afero.Afero
}

// WriteMainTF writes the content main configuration file that has the desired
// state configuration for Terraform.
func (fp *FileProducer) WriteMainTF() (ProviderHandle, error) {
	// If the resource is in a deletion process, we need to remove the deletion
	// protection.
	fp.parameters["lifecycle"] = map[string]bool{
		"prevent_destroy": !meta.WasDeleted(fp.Resource),
	}

	// Add operation timeouts if any timeout configured for the resource
	if tp := timeouts(fp.Config.OperationTimeouts).asParameter(); len(tp) != 0 {
		fp.parameters["timeouts"] = tp
	}

	// Note(turkenh): To use third party providers, we need to configure
	// provider name in required_providers.
	providerSource := strings.Split(fp.Setup.Requirement.Source, "/")
	m := map[string]any{
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
func (fp *FileProducer) EnsureTFState(ctx context.Context, tfID string) error { //nolint:gocyclo
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
	s.Resources = []json.ResourceStateV4{
		{
			Mode: "managed",
			Type: fp.Resource.GetTerraformResourceType(),
			Name: fp.Resource.GetName(),
			// TODO(muvaf): we should get the full URL from Dockerfile since
			// providers don't have to be hosted in registry.terraform.io
			ProviderConfig: fmt.Sprintf(`provider["registry.terraform.io/%s"]`, fp.Setup.Requirement.Source),
			Instances: []json.InstanceObjectStateV4{
				{
					SchemaVersion: uint64(fp.Resource.GetTerraformSchemaVersion()),
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
