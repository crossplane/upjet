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
	"fmt"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/json"
	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
)

const (
	defaultAsyncTimeout = 1 * time.Hour
	// AnnotationKeyPrivateRawAttribute is the key that points to private attribute
	// of the Terraform State. It's non-sensitive and used by provider to store
	// arbitrary metadata, usually details about schema version.
	AnnotationKeyPrivateRawAttribute = "terrajet.crossplane.io/provider-meta"
)

// Error strings.
const (
	fmtErrValidationProvider = "invalid provider specification: both source and version are required: source=%q and version=%q"
	fmtErrValidationVersion  = "invalid setup specification, Terraform version not provided"
)

type WorkspaceOption func(c *Workspace)

func WithEnqueueFn(fn EnqueueFn) WorkspaceOption {
	return func(w *Workspace) {
		w.Enqueue = fn
	}
}

func NewWorkspace(dir string, opts ...WorkspaceOption) (*Workspace, error) {
	w := &Workspace{
		dir:     dir,
		Enqueue: NopEnqueueFn,
	}
	for _, f := range opts {
		f(w)
	}
	return w, nil
}

type EnqueueFn func()

func NopEnqueueFn() {}

type Workspace struct {
	LastOperation Operation
	Enqueue       EnqueueFn

	dir string
}

// NewFileProducer returns a new FileProducer.
func NewFileProducer(tr resource.Terraformed) (*FileProducer, error) {
	params, err := tr.GetParameters()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get parameters")
	}
	obs, err := tr.GetObservation()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get observation")
	}
	return &FileProducer{
		Resource:    tr,
		parameters:  params,
		observation: obs,
	}, nil
}

// FileProducer exist to serve as cache for the data that is costly to produce
// every time like parameters and observation maps.
type FileProducer struct {
	Resource resource.Terraformed
	Setup    TerraformSetup

	parameters  map[string]interface{}
	observation map[string]interface{}
}

// TFState returns the Terraform state that should exist in the filesystem to
// start any Terraform operation.
func (fp *FileProducer) TFState() (*json.StateV4, error) {
	base := make(map[string]interface{})
	// NOTE(muvaf): Since we try to produce the current state, observation
	// takes precedence over parameters.
	for k, v := range fp.parameters {
		base[k] = v
	}
	for k, v := range fp.observation {
		base[k] = v
	}
	base["id"] = meta.GetExternalName(fp.Resource)
	attr, err := json.JSParser.Marshal(base)
	if err != nil {
		return nil, errors.Wrap(err, "cannot marshal produced state attributes")
	}
	var privateRaw []byte
	if pr, ok := fp.Resource.GetAnnotations()[AnnotationKeyPrivateRawAttribute]; ok {
		privateRaw = []byte(pr)
	}
	st := json.NewStateV4()
	st.TerraformVersion = fp.Setup.Version
	st.Lineage = string(fp.Resource.GetUID())
	st.Resources = []json.ResourceStateV4{
		{
			Mode: "managed",
			Type: fp.Resource.GetTerraformResourceType(),
			Name: fp.Resource.GetName(),
			// TODO(muvaf): we should get the full URL from Dockerfile since
			// providers don't have to be hosted in registry.terraform.io
			ProviderConfig: fmt.Sprintf(`provider["registry.terraform.io/%s"]`, fp.Setup.Requirement.Source),
			Instances: []json.InstanceObjectStateV4{
				{
					SchemaVersion: 0,
					PrivateRaw:    privateRaw,
					AttributesRaw: attr,
				},
			},
		},
	}
	return st, nil
}

// MainTF returns the content main configuration file that has the desired state
// for Terraform as a map that can be written to disk as valid JSON input to
// Terraform.
func (fp *FileProducer) MainTF() map[string]interface{} {
	// If the resource is in a deletion process, we need to remove the deletion
	// protection.
	fp.parameters["prevent_destroy"] = !meta.WasDeleted(fp.Resource)
	return map[string]interface{}{
		"terraform": map[string]interface{}{
			"required_providers": map[string]interface{}{
				"tf-provider": map[string]string{
					"source":  fp.Setup.Requirement.Source,
					"version": fp.Setup.Requirement.Version,
				},
			},
		},
		"provider": map[string]interface{}{
			"tf-provider": fp.Setup.Configuration,
		},
		"resource": map[string]interface{}{
			fp.Resource.GetTerraformResourceType(): map[string]interface{}{
				fp.Resource.GetName(): fp.parameters,
			},
		},
	}
}

// ProviderRequirement holds values for the Terraform HCL setup requirements
type ProviderRequirement struct {
	Source  string
	Version string
}

// ProviderConfiguration holds the setup configuration body
type ProviderConfiguration map[string]interface{}

// TerraformSetup holds values for the Terraform version and setup
// requirements and configuration body
type TerraformSetup struct {
	Version       string
	Requirement   ProviderRequirement
	Configuration ProviderConfiguration
}

func (p TerraformSetup) validate() error {
	if p.Version == "" {
		return errors.New(fmtErrValidationVersion)
	}
	if p.Requirement.Source == "" || p.Requirement.Version == "" {
		return errors.Errorf(fmtErrValidationProvider, p.Requirement.Source, p.Requirement.Version)
	}
	return nil
}
