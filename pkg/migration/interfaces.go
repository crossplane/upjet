// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

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

// ConfigurationMetadataConverter converts a Crossplane Configuration's metadata.
type ConfigurationMetadataConverter interface {
	// ConfigurationMetadataV1 takes a Crossplane Configuration v1 metadata,
	// converts it, and stores the converted metadata in its argument.
	// Returns any errors encountered during the conversion.
	ConfigurationMetadataV1(configuration *xpmetav1.Configuration) error
	// ConfigurationMetadataV1Alpha1 takes a Crossplane Configuration v1alpha1
	// metadata, converts it, and stores the converted metadata in its
	// argument. Returns any errors encountered during the conversion.
	ConfigurationMetadataV1Alpha1(configuration *xpmetav1alpha1.Configuration) error
}

// ConfigurationPackageConverter converts a Crossplane configuration package.
type ConfigurationPackageConverter interface {
	// ConfigurationPackageV1 takes a Crossplane Configuration v1 package,
	// converts it possibly to multiple packages and returns
	// the converted configuration package.
	// Returns any errors encountered during the conversion.
	ConfigurationPackageV1(pkg *xppkgv1.Configuration) error
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
	// Reset resets the Source so that it can read the manifests
	// from the beginning. There is no guarantee that the Source
	// will return the same set of manifests or it will return
	// them in the same order after a reset.
	Reset() error
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
	Init(config map[string]any) error
	// Step asks the executor to execute the next step passing any available
	// context from the previous step, and returns any new context to be passed
	// to the next step if there exists one.
	Step(s Step, ctx map[string]any) error
	// Destroy is called when all the steps have been executed,
	// or a step has returned an error, and we would like to stop
	// executing the plan.
	Destroy() error
}

// UnstructuredPreProcessor allows manifests read by the Source
// to be pre-processed before the converters are run
// It's not possible to do any conversions via the pre-processors,
// and they only allow migrators to extract information from
// the manifests read by the Source before any converters are run.
type UnstructuredPreProcessor interface {
	// PreProcess is called for a manifest read by the Source
	// before any converters are run.
	PreProcess(u UnstructuredWithMetadata) error
}

// ManagedPreProcessor allows manifests read by the Source
// to be pre-processed before the converters are run.
// These pre-processors will work for GVKs that have ResourceConverter
// registered.
type ManagedPreProcessor interface {
	// ResourcePreProcessor is called for a manifest read by the Source
	// before any converters are run.
	ResourcePreProcessor(mg resource.Managed) error
}

// CategoricalConverter is a converter that converts resources of a given
// Category. Because it receives an unstructured argument, it should be
// used for implementing generic conversion functions acting on a specific
// category, such as setting a deletion policy on all the managed resources
// observed by the migration Source.
type CategoricalConverter interface {
	Convert(u *UnstructuredWithMetadata) error
}
