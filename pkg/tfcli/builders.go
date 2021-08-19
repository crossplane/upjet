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
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane-contrib/terrajet/pkg/log"
	"github.com/crossplane-contrib/terrajet/pkg/version"
)

const (
	// error format strings
	errValidationState       = "invalid Terraform state: serialized state size should be greater than zero"
	errValidationNoLogger    = "no logger has been configured"
	fmtErrValidationResource = "invalid resource specification: both type and name are required: type=%q and name=%q"
	fmtErrValidationProvider = "invalid provider specification: both source and version are required: source=%q and version=%q"
	// env variable names
	envTFProviderVersion       = "XP_TERRAFORM_PROVIDER_VERSION"
	envTFProviderSource        = "XP_TERRAFORM_PROVIDER_SOURCE"
	envTFProviderConfiguration = "XP_TERRAFORM_PROVIDER_CONFIG"
	envTFExecTimeout           = "XP_TERRAFORM_CLI_EXEC_TIMEOUT"

	fmtResourceAddress = "%s.%s"
)

type Builder interface {
	RequiresProvider
	RequiresResource
	RequiresState
	RequiresTimeout
	RequiresLogger
	Build() (Client, error)
}

type RequiresLogger interface {
	WithLogger(logger logging.Logger) Builder
}

type RequiresTimeout interface {
	WithTimeout(d time.Duration) Builder
}

type RequiresContext interface {
	WithContext(ctx context.Context) Builder
}

type RequiresState interface {
	WithState(tfState []byte) Builder
}

type RequiresResource interface {
	WithResourceType(labelType string) Builder
	WithResourceName(labelName string) Builder
	WithResourceBody(body []byte) Builder
	WithHandle(handle string) Builder
}

type RequiresProvider interface {
	WithProviderSource(source string) Builder
	WithProviderVersion(version string) Builder
	WithProviderConfiguration(conf []byte) Builder
}

type clientBuilder struct {
	c *client
}

func NewClientBuilder() *clientBuilder {
	c := defaultClient()
	return &clientBuilder{
		c: c,
	}
}

func (cb clientBuilder) validateNoState() error {
	if err := cb.c.resource.validate(); err != nil {
		return err
	}
	if err := cb.c.provider.validate(); err != nil {
		return err
	}
	if err := cb.c.logger.validate(); err != nil {
		return err
	}
	return nil
}

func (cb clientBuilder) validateWithState() error {
	if err := cb.c.state.validate(); err != nil {
		return err
	}
	return cb.validateNoState()
}

func (cb clientBuilder) Build() (Client, error) {
	return cb.c, cb.validateNoState()
}

func defaultClient() *client {
	return &client{
		state:       &withState{},
		provider:    providerFromEnv(),
		resource:    &withResource{},
		execTimeout: timeoutFromEnv(),
		logger: &withLogger{
			log: logging.NewLogrLogger(
				log.NewLoggerWithServiceContext("tfcli", version.Version, false)),
		},
		mu: &sync.Mutex{},
	}
}

type withTimeout struct {
	to time.Duration
}

func timeoutFromEnv() *withTimeout {
	result := &withTimeout{}
	toStr := os.Getenv(envTFExecTimeout)
	if toStr == "" {
		return result
	}

	// silently ignore unparsable timeout errors, we will observe
	// execution timeout values in logs
	result.to, _ = time.ParseDuration(toStr)
	return result
}

type withLogger struct {
	log logging.Logger
}

func (l withLogger) validate() error {
	if l.log == nil {
		return errors.New(errValidationNoLogger)
	}
	return nil
}

type withState struct {
	tfState []byte
}

func (s withState) validate() error {
	if len(s.tfState) == 0 {
		return errors.New(errValidationState)
	}
	return nil
}

// holds values for the Terraform HCL resource block's two labels and body
type withResource struct {
	labelType string
	labelName string
	body      []byte
	handle    string
}

func (r withResource) validate() error {
	if r.labelName == "" || r.labelType == "" {
		return errors.Errorf(fmtErrValidationResource, r.labelType, r.labelName)
	}
	return nil
}

func (r withResource) GetAddress() string {
	return fmt.Sprintf(fmtResourceAddress, r.labelType, r.labelName)
}

// holds values for the Terraform HCL provider block's source, version and configuration body
type withProvider struct {
	source        string
	version       string
	configuration []byte
}

func (p withProvider) validate() error {
	if p.source == "" || p.version == "" {
		return errors.Errorf(fmtErrValidationProvider, p.source, p.version)
	}
	return nil
}

func providerFromEnv() *withProvider {
	p := &withProvider{}
	if p.source == "" {
		p.source = os.Getenv(envTFProviderSource)
	}
	if p.version == "" {
		p.version = os.Getenv(envTFProviderVersion)
	}
	if p.configuration == nil {
		p.configuration = []byte(os.Getenv(envTFProviderConfiguration))
	}

	return p
}

func (cb *clientBuilder) WithLogger(logger logging.Logger) Builder {
	cb.c.logger.log = logger
	return cb
}

func (cb *clientBuilder) WithTimeout(to time.Duration) Builder {
	cb.c.execTimeout.to = to
	return cb
}

func (cb *clientBuilder) WithState(tfState []byte) Builder {
	cb.c.state.tfState = tfState
	return cb
}

func (cb *clientBuilder) WithHandle(handle string) Builder {
	cb.c.resource.handle = handle
	return cb
}

func (cb *clientBuilder) WithResourceBody(body []byte) Builder {
	cb.c.resource.body = body
	return cb
}

func (cb *clientBuilder) WithResourceType(labelType string) Builder {
	cb.c.resource.labelType = labelType
	return cb
}

func (cb *clientBuilder) WithResourceName(labelName string) Builder {
	cb.c.resource.labelName = labelName
	return cb
}

func (cb *clientBuilder) WithProviderConfiguration(conf []byte) Builder {
	cb.c.provider.configuration = conf
	return cb
}

func (cb *clientBuilder) WithProviderSource(source string) Builder {
	cb.c.provider.source = source
	return cb
}

func (cb *clientBuilder) WithProviderVersion(version string) Builder {
	cb.c.provider.version = version
	return cb
}
