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
	xpv1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	xpmetav1 "github.com/crossplane/crossplane/apis/pkg/meta/v1"
	xpmetav1alpha1 "github.com/crossplane/crossplane/apis/pkg/meta/v1alpha1"
	xppkgv1 "github.com/crossplane/crossplane/apis/pkg/v1"
	xppkgv1beta1 "github.com/crossplane/crossplane/apis/pkg/v1beta1"
)

// ResourceConverter converts a managed resource from
// the migration source provider's schema to the migration target
// provider's schema.
type ResourceConverter interface {
	// Resource takes a managed resource and returns zero or more managed
	// resources to be created.
	Resource(mg resource.Managed) ([]resource.Managed, error)
}

// ComposedTemplateConverter converts a Composition's ComposedTemplate
// from the migration source provider's schema to the migration target
// provider's schema. Conversion of the `Base` must be handled by
// a ResourceConverter.
type ComposedTemplateConverter interface {
	// ComposedTemplate receives a migration source v1.ComposedTemplate
	// that has been converted, by a resource converter, to the
	// v1.ComposedTemplates with the new shapes specified in the
	// `convertedTemplates` argument.
	// Conversion of the v1.ComposedTemplate.Bases is handled
	// via ResourceConverter.Resource and ComposedTemplate must only
	// convert the other fields (`Patches`, `ConnectionDetails`,
	// `PatchSet`s, etc.)
	// Returns any errors encountered.
	ComposedTemplate(sourceTemplate xpv1.ComposedTemplate, convertedTemplates ...*xpv1.ComposedTemplate) error
}

// CompositionConverter converts a managed resource and a Composition's
// ComposedTemplate that composes a managed resource of the same kind
// from the migration source provider's schema to the migration target
// provider's schema.
type CompositionConverter interface {
	ResourceConverter
	ComposedTemplateConverter
}

// PatchSetConverter converts patch sets of Compositions.
// Any registered PatchSetConverters
// will be called before any resource or ComposedTemplate conversion is done.
// The rationale is to convert the Composition-wide patch sets before
// any resource-specific conversions so that migration targets can
// automatically inherit converted patch sets if their schemas match them.
// Registered PatchSetConverters will be called in the order
// they are registered.
type PatchSetConverter interface {
	// PatchSets converts the `spec.patchSets` of a Composition
	// from the migration source provider's schema to the migration target
	// provider's schema.
	PatchSets(psMap map[string]*xpv1.PatchSet) error
}

// ConfigurationConverter converts a Crossplane Configuration's metadata.
type ConfigurationConverter interface {
	// ConfigurationV1 takes a Crossplane Configuration v1 metadata,
	// converts it, and stores the converted metadata in its argument.
	// Returns any errors encountered during the conversion.
	ConfigurationV1(configuration *xpmetav1.Configuration) error
	// ConfigurationV1Alpha1 takes a Crossplane Configuration v1alpha1
	// metadata, converts it, and stores the converted metadata in its
	// argument. Returns any errors encountered during the conversion.
	ConfigurationV1Alpha1(configuration *xpmetav1alpha1.Configuration) error
}

// ProviderPackageConverter converts a Crossplane provider package.
type ProviderPackageConverter interface {
	// ProviderPackageV1 takes a Crossplane Provider v1 package,
	// converts it possibly to multiple packages and returns the
	// converted provider packages.
	// Returns any errors encountered during the conversion.
	ProviderPackageV1(pkg xppkgv1.Provider) ([]xppkgv1.Provider, error)
}

// PackageLockConverter converts a Crossplane package lock.
type PackageLockConverter interface {
	// PackageLockV1Beta1 takes a Crossplane v1beta1 package lock,
	// converts it, and stores the converted lock in its argument.
	// Returns any errors encountered during the conversion.
	PackageLockV1Beta1(lock *xppkgv1beta1.Lock) error
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

// Executor is a migration plan executor.
type Executor interface {
	// Init initializes an executor using the supplied executor specific
	// configuration data.
	Init(config any) error
	// Step asks the executor to execute the next step passing any available
	// context from the previous step, and returns any new context to be passed
	// to the next step if there exists one.
	Step(s Step, ctx any) (any, error)
	// Destroy is called when all the steps have been executed,
	// or a step has returned an error, and we would like to stop
	// executing the plan.
	Destroy() error
}
