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
	privateRawWithTimeout, err := insertTimeoutsMeta(privateRaw, fp.Config.OperationTimeouts)
	if err != nil {
		return errors.Wrap(err, "cannot insert timeouts meta")
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
					PrivateRaw:    privateRawWithTimeout,
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
	tp := timeoutsAsParameter(fp.Config.OperationTimeouts)
	if len(tp) != 0 {
		fp.parameters["timeouts"] = tp
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

func timeoutsAsParameter(ot config.OperationTimeouts) map[string]string {
	timeouts := make(map[string]string)
	if t := ot.Read.String(); t != "0s" {
		timeouts["read"] = t
	}
	if t := ot.Create.String(); t != "0s" {
		timeouts["create"] = t
	}
	if t := ot.Update.String(); t != "0s" {
		timeouts["update"] = t
	}
	if t := ot.Delete.String(); t != "0s" {
		timeouts["delete"] = t
	}
	return timeouts
}

// "e2bfb730-ecaa-11e6-8f88-34363bc7c4c0" is the TimeoutKey:
// https://github.com/hashicorp/terraform-plugin-sdk/blob/112e2164c381d80e8ada3170dac9a8a5db01079a/helper/schema/resource_timeout.go#L14
const tfMetaTimeoutKey = "e2bfb730-ecaa-11e6-8f88-34363bc7c4c0"

func insertTimeoutsMeta(rawMeta []byte, ot config.OperationTimeouts) ([]byte, error) {
	m := make(map[string]interface{})
	if t := ot.Read.String(); t != "0s" {
		m["read"] = ot.Read.Nanoseconds()
	}
	if t := ot.Create.String(); t != "0s" {
		m["create"] = ot.Create.Nanoseconds()
	}
	if t := ot.Update.String(); t != "0s" {
		m["update"] = ot.Update.Nanoseconds()
	}
	if t := ot.Delete.String(); t != "0s" {
		m["delete"] = ot.Delete.Nanoseconds()
	}
	if len(m) == 0 {
		// no timeout configured
		return rawMeta, nil
	}
	meta := make(map[string]interface{})
	if len(rawMeta) == 0 {
		meta[tfMetaTimeoutKey] = m
		return json.JSParser.Marshal(meta)
	}
	if err := json.JSParser.Unmarshal(rawMeta, &meta); err != nil {
		return rawMeta, errors.Wrap(err, "cannot unmarshall private raw meta")
	}
	meta[tfMetaTimeoutKey] = m
	return json.JSParser.Marshal(meta)
}
