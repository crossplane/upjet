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
	fmtErrValidationProvider = "invalid provider specification: both source and version are required: source=%q and version=%q"

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
func WithResourceBody(resourceBody []byte) ClientOption {
	return func(c *Client) {
		// common controller serializes resource parameter block into
		// a JSON object. However, we would like to add new fields
		// to this JSON object and thus here we capture only the parameters.
		c.resource.Body = resourceBody[1 : len(resourceBody)-1]
	}
}

// WithProviderConfiguration sets the Terraform provider
// configuration block to be used in the generated Terraform configuration
func WithProviderConfiguration(conf []byte) ClientOption {
	return func(c *Client) {
		c.provider.Configuration = conf
	}
}

// WithProviderSource sets the Terraform provider
// source to be used in the generated Terraform configuration
func WithProviderSource(source string) ClientOption {
	return func(c *Client) {
		c.provider.Source = source
	}
}

// WithProviderVersion sets the Terraform provider
// version to be used in the generated Terraform configuration
func WithProviderVersion(version string) ClientOption {
	return func(c *Client) {
		c.provider.Version = version
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
// (WithResource*), all Terraform provider block generation options
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
	if err := c.provider.validate(); err != nil {
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
	Body      []byte
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

// Provider holds values for the Terraform HCL provider block's source, version and configuration body
type Provider struct {
	Source        string
	Version       string
	Configuration []byte
}

func (p Provider) validate() error {
	if p.Source == "" || p.Version == "" {
		return errors.Errorf(fmtErrValidationProvider, p.Source, p.Version)
	}
	return nil
}
