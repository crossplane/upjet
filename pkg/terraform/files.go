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

package terraform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/pkg/errors"
	"github.com/spf13/afero"

	"github.com/crossplane/terrajet/pkg/config"
	"github.com/crossplane/terrajet/pkg/resource"
	"github.com/crossplane/terrajet/pkg/resource/json"
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

	parameters  map[string]interface{}
	observation map[string]interface{}
	fs          afero.Afero
}

// WriteTFState writes the Terraform state that should exist in the filesystem to
// start any Terraform operation.
func (fp *FileProducer) WriteTFState(ctx context.Context) error {
	base := make(map[string]interface{})
	// NOTE(muvaf): Since we try to produce the current state, observation
	// takes precedence over parameters.
	for k, v := range fp.parameters {
		base[k] = v
	}
	for k, v := range fp.observation {
		base[k] = v
	}
	id, err := fp.Config.ExternalName.GetIDFn(ctx, meta.GetExternalName(fp.Resource), fp.parameters, fp.Setup.Configuration)
	if err != nil {
		return errors.Wrap(err, "cannot get id")
	}
	base["id"] = id
	attr, err := json.JSParser.Marshal(base)
	if err != nil {
		return errors.Wrap(err, "cannot marshal produced state attributes")
	}
	var privateRaw []byte
	if pr, ok := fp.Resource.GetAnnotations()[resource.AnnotationKeyPrivateRawAttribute]; ok {
		privateRaw = []byte(pr)
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
		return errors.Wrap(err, "cannot marshal state object")
	}
	return errors.Wrap(fp.fs.WriteFile(filepath.Join(fp.Dir, "terraform.tfstate"), rawState, os.ModePerm), "cannot write tfstate file")
}

// WriteMainTF writes the content main configuration file that has the desired
// state configuration for Terraform.
func (fp *FileProducer) WriteMainTF() error {
	// If the resource is in a deletion process, we need to remove the deletion
	// protection.
	fp.parameters["lifecycle"] = map[string]bool{
		"prevent_destroy": !meta.WasDeleted(fp.Resource),
	}

	// Add operation timeouts if any timeout configured for the resource
	timeouts := map[string]string{}
	timeoutsConfigured := false
	if fp.Config.OperationTimeouts.Read != nil {
		timeouts["read"] = fp.Config.OperationTimeouts.Read.String()
		timeoutsConfigured = true
	}
	if fp.Config.OperationTimeouts.Create != nil {
		timeouts["create"] = fp.Config.OperationTimeouts.Create.String()
		timeoutsConfigured = true
	}
	if fp.Config.OperationTimeouts.Update != nil {
		timeouts["update"] = fp.Config.OperationTimeouts.Update.String()
		timeoutsConfigured = true
	}
	if fp.Config.OperationTimeouts.Delete != nil {
		timeouts["delete"] = fp.Config.OperationTimeouts.Delete.String()
		timeoutsConfigured = true
	}
	if timeoutsConfigured {
		fp.parameters["timeouts"] = timeouts
	}

	// Note(turkenh): To use third party providers, we need to configure
	// provider name in required_providers.
	providerSource := strings.Split(fp.Setup.Requirement.Source, "/")
	m := map[string]interface{}{
		"terraform": map[string]interface{}{
			"required_providers": map[string]interface{}{
				providerSource[1]: map[string]string{
					"source":  fp.Setup.Requirement.Source,
					"version": fp.Setup.Requirement.Version,
				},
			},
		},
		"provider": map[string]interface{}{
			providerSource[1]: fp.Setup.Configuration,
		},
		"resource": map[string]interface{}{
			fp.Resource.GetTerraformResourceType(): map[string]interface{}{
				fp.Resource.GetName(): fp.parameters,
			},
		},
	}
	rawMainTF, err := json.JSParser.Marshal(m)
	if err != nil {
		return errors.Wrap(err, "cannot marshal main hcl object")
	}
	return errors.Wrap(fp.fs.WriteFile(filepath.Join(fp.Dir, "main.tf.json"), rawMainTF, os.ModePerm), "cannot write tfstate file")
}
