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
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
)

type RefreshResult struct {
	UpToDate bool
	Exists   bool
	State    []byte
}

type ApplyResult struct {
	Completed bool
	State     []byte
}

type DestroyResult struct {
	Completed bool
}

type Client interface {
	Refresh(ctx context.Context, id string) (RefreshResult, error)
	Apply(ctx context.Context) (ApplyResult, error)
	Destroy(ctx context.Context) (DestroyResult, error)
	Close(ctx context.Context) error
}

type Builder interface {
	RequiresProvider
	RequiresResource
	RequiresState
	RequiresTimeout
	RequiresLogger
	BuildClient() (Client, error)
}

type RequiresLogger interface {
	WithLogger(logger logging.Logger) Builder
}

type RequiresTimeout interface {
	WithTimeout(d time.Duration) Builder
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
