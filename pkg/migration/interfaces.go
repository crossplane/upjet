// Copyright 2022 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package migration

import (
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
)

// Converter converts a managed resource or a Composition's ComposedTemplate
// from the migration source provider's schema to the migration target
// provider's schema.
type Converter interface {
	// Resources takes a managed resource and returns zero or more managed
	// resources to be created.
	Resources(mg resource.Managed) ([]resource.Managed, error)

	// ComposedTemplates takes a ComposedTemplate entry and returns zero or more
	// ComposedTemplate with the new shape, including the necessary changes in
	// its patches. Conversion of the v1.ComposedTemplate.Bases is handled
	// via Converter.Resources and Converter.ComposedTemplates must only
	// convert the other fields (`Patches`, `ConnectionDetails`, etc.)
	ComposedTemplates(cmp v1.ComposedTemplate, convertedBase ...*v1.ComposedTemplate) error
}

// Source is a source for reading resource manifests
type Source interface {
	// HasNext returns `true` if the Source implementation has a next manifest
	// available to return with a call to Next. Any errors encountered while
	// determining whether a next manifest exists will also be reported.
	HasNext() (bool, error)
	// Next returns the next resource manifest available or
	// any errors encountered while reading the next resource manifest.
	Next() (UnstructuredWithMetadata, error)
}

// Target is a target where resource manifests can be manipulated
// (e.g., added, deleted, patched, etc.)
type Target interface {
	// Put writes a resource manifest to this Target
	Put(o UnstructuredWithMetadata) error
	// Delete deletes a resource manifest from this Target
	Delete(o UnstructuredWithMetadata) error
}
