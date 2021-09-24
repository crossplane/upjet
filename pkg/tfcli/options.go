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
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/afero"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane-contrib/terrajet/pkg/version"
)

const (
	defaultAsyncTimeout = 2 * time.Minute
	// error format strings
	errValidationNoLogger    = "no logger has been configured"
	errValidationNoHandle    = "no workspace handle has been configured"
	fmtErrValidationResource = "invalid resource specification: both type and name are required: type=%q and name=%q"
	fmtErrValidationProvider = "invalid setup.requirement specification: both source and version are required: source=%q and version=%q"
	fmtErrValidationVersion  = "invalid setup specification, Terraform version not provided"

	fmtResourceAddress = "%s.%s"
)

// A ClientOption configures a Client
type ClientOption func(c *Client)

// WithState sets the Terraform state cache of a Client
func WithState(tfState []byte) ClientOption {
	return func(c *Client) {
		c.tfState = tfState
	}
}

// WithLogger configures the logger to be used by a Client
func WithLogger(logger logging.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger.WithValues("tfcli-version", version.Version)
	}
}

// WithResourceName sets the Terraform resource name to be used in the
// generated Terraform configuration
func WithResourceName(resourceName string) ClientOption {
	return func(c *Client) {
		c.resource.LabelName = resourceName
	}
}

// WithResourceType sets the Terraform resource type to be used in the
// generated Terraform configuration
func WithResourceType(resourceType string) ClientOption {
	return func(c *Client) {
		c.resource.LabelType = resourceType
	}
}

// WithResourceBody sets the Terraform resource body parameter block to
// be used in the generated  Terraform configuration. resourceBody
// must be a whole JSON document containing the serialized resource
// parameters.
func WithResourceBody(body map[string]interface{}) ClientOption {
	return func(c *Client) {
		c.resource.Body = body
	}
}

// WithTerraformSetup sets the Terraform configuration which
// contains provider requirement, configuration and Terraform version
func WithTerraformSetup(setup TerraformSetup) ClientOption {
	return func(c *Client) {
		c.setup = setup
	}
}

// WithHandle is a unique ID used by the Client to associate a
// requested Terraform pipeline with a Terraform workspace
func WithHandle(h string) ClientOption {
	return func(c *Client) {
		c.handle = h
	}
}

// WithAsyncTimeout configures the timeout used for async operations
func WithAsyncTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.timeout = &timeout
	}
}

// WithStateStoreFs configures the filesystem to be used by a
// Client. Client uses this filesystem to store locks including
// Terraform locks & Terraform command pipeline results
func WithStateStoreFs(fs afero.Fs) ClientOption {
	return func(c *Client) {
		c.fs = fs
	}
}

// NewClient returns an initialized Client that is used to run
// Terraform Refresh, Apply, Destroy command pipelines.
// The workspace configured with WithHandle option is initialized
// for the returned Client. All Terraform resource block generation options
// (WithResource*), all Terraform setup block generation options
// (WithProvider*), the workspace handle option (WithHandle) and a
// logger (WithLogger) must have been configured for the Client.
// Returns an error if the supplied options cannot be validated, or
// if the Terraform init operation run for workspace initialization
// fails.
func NewClient(ctx context.Context, opts ...ClientOption) (*Client, error) {
	c := &Client{}
	for _, o := range opts {
		o(c)
	}
	// for state store filesystem, default to OS filesystem
	if c.fs == nil {
		c.fs = afero.NewOsFs()
	}
	// configure default async timeout
	if c.timeout == nil {
		d := defaultAsyncTimeout
		c.timeout = &d
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	if err := c.init(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c Client) validate() error {
	if err := c.resource.validate(); err != nil {
		return err
	}
	if err := c.setup.validate(); err != nil {
		return err
	}
	if c.logger == nil {
		return errors.New(errValidationNoLogger)
	}
	if c.handle == "" {
		return errors.New(errValidationNoHandle)
	}
	return nil
}

// Resource holds values for the Terraform HCL resource block's two labels and body
type Resource struct {
	LabelType string
	LabelName string
	Body      map[string]interface{}
	Lifecycle Lifecycle
}

// Lifecycle holds values for the Terraform HCL resource block's lifecycle options:
// https://www.terraform.io/docs/language/meta-arguments/lifecycle.html
type Lifecycle struct {
	PreventDestroy bool
}

func (r Resource) validate() error {
	if r.LabelName == "" || r.LabelType == "" {
		return errors.Errorf(fmtErrValidationResource, r.LabelType, r.LabelName)
	}
	return nil
}

// GetAddress returns the Terraform configuration resource address of
// the receiver Resource
func (r Resource) GetAddress() string {
	return fmt.Sprintf(fmtResourceAddress, r.LabelType, r.LabelName)
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
