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

	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane-contrib/terrajet/pkg/version"
)

const (
	// error format strings
	errValidationNoLogger    = "no logger has been configured"
	fmtErrValidationResource = "invalid resource specification: both type and name are required: type=%q and name=%q"
	fmtErrValidationProvider = "invalid provider specification: both source and version are required: source=%q and version=%q"

	fmtResourceAddress = "%s.%s"
)

type ClientOption func(c *Client)

func WithState(tfState []byte) ClientOption {
	return func(c *Client) {
		c.tfState = tfState
	}
}

func WithLogger(logger logging.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger.WithValues("tfcli-version", version.Version)
	}
}

func WithResourceName(resourceName string) ClientOption {
	return func(c *Client) {
		c.resource.LabelName = resourceName
	}
}

func WithResourceType(resourceType string) ClientOption {
	return func(c *Client) {
		c.resource.LabelType = resourceType
	}
}

func WithResourceBody(resourceBody []byte) ClientOption {
	return func(c *Client) {
		c.resource.Body = resourceBody
	}
}

func WithProviderConfiguration(conf []byte) ClientOption {
	return func(c *Client) {
		c.provider.Configuration = conf
	}
}

func WithProviderSource(source string) ClientOption {
	return func(c *Client) {
		c.provider.Source = source
	}
}

func WithProviderVersion(version string) ClientOption {
	return func(c *Client) {
		c.provider.Version = version
	}
}

func WithHandle(h string) ClientOption {
	return func(c *Client) {
		c.handle = h
	}
}

func NewClient(ctx context.Context, opts ...ClientOption) (*Client, error) {
	c := &Client{}
	for _, o := range opts {
		o(c)
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
	return nil
}

// Resource holds values for the Terraform HCL resource block's two labels and body
type Resource struct {
	LabelType string
	LabelName string
	Body      []byte
}

func (r Resource) validate() error {
	if r.LabelName == "" || r.LabelType == "" {
		return errors.Errorf(fmtErrValidationResource, r.LabelType, r.LabelName)
	}
	return nil
}

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
