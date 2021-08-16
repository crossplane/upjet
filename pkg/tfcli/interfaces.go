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

type ObserveResult struct {
	UpToDate  bool
	Completed bool
	Exists    bool
}

type Client interface {
	Observe(id string) (ObserveResult, error)
	Create() (bool, error)
	Update() (bool, error)
	Delete() (bool, error)
	GetHandle() string
	GetState() []byte
	Destroy() error
}

type Builder interface {
	RequiresProvider
	RequiresResource
	RequiresState
	RequiresContext
	RequiresTimeout
	RequiresLogger
	BuildObserveClient() (Client, error)
	BuildCreateClient() (Client, error)
	BuildUpdateClient() (Client, error)
	BuildDeletionClient() (Client, error)
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
