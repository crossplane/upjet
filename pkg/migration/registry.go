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
	"regexp"

	"github.com/crossplane/crossplane-runtime/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	xpmetav1 "github.com/crossplane/crossplane/apis/pkg/meta/v1"
	xpmetav1alpha1 "github.com/crossplane/crossplane/apis/pkg/meta/v1alpha1"
	xppkgv1 "github.com/crossplane/crossplane/apis/pkg/v1"
	xppkgv1beta1 "github.com/crossplane/crossplane/apis/pkg/v1beta1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// AllCompositions matches all v1.Composition names.
	AllCompositions = regexp.MustCompile(`.*`)
	// AllConfigurations matches all metav1.Configuration names.
	AllConfigurations = regexp.MustCompile(`.*`)
	// CrossplaneLockName is the Crossplane package lock's `metadata.name`
	CrossplaneLockName = regexp.MustCompile(`^lock$`)
)

const (
	errAddToScheme           = "failed to register types with the registry's scheme"
	errFmtNewObject          = "failed to instantiate a new runtime.Object using runtime.Scheme for: %s"
	errFmtNotManagedResource = "specified GVK does not belong to a managed resource: %s"
)

type patchSetConverter struct {
	// re is the regular expression against which a Composition's name
	// will be matched to determine whether the conversion function
	// will be invoked.
	re *regexp.Regexp
	// converter is the PatchSetConverter to be run on the Composition's
	// patch sets.
	converter PatchSetConverter
}

type configurationMetadataConverter struct {
	// re is the regular expression against which a Configuration's name
	// will be matched to determine whether the conversion function
	// will be invoked.
	re *regexp.Regexp
	// converter is the ConfigurationMetadataConverter to be run on the Configuration's
	// metadata.
	converter ConfigurationMetadataConverter
}

type configurationPackageConverter struct {
	// re is the regular expression against which a Configuration package's
	// reference will be matched to determine whether the conversion function
	// will be invoked.
	re *regexp.Regexp
	// converter is the ConfigurationPackageConverter to be run on the
	// Configuration package.
	converter ConfigurationPackageConverter
}

type providerPackageConverter struct {
	// re is the regular expression against which a Provider package's
	// reference will be matched to determine whether the conversion function
	// will be invoked.
	re *regexp.Regexp
	// converter is the ProviderPackageConverter to be run on the
	// Provider package.
	converter ProviderPackageConverter
}

type packageLockConverter struct {
	// re is the regular expression against which a package Lock's name
	// will be matched to determine whether the conversion function
	// will be invoked.
	re *regexp.Regexp
	// converter is the PackageLockConverter to be run on the package Lock.
	converter PackageLockConverter
}

// Registry is a registry of `migration.Converter`s keyed with
// the associated `schema.GroupVersionKind`s and an associated
// runtime.Scheme with which the corresponding types are registered.
type Registry struct {
	unstructuredPreProcessors      map[Category][]UnstructuredPreProcessor
	resourceConverters             map[schema.GroupVersionKind]ResourceConverter
	templateConverters             map[schema.GroupVersionKind]ComposedTemplateConverter
	patchSetConverters             []patchSetConverter
	configurationMetaConverters    []configurationMetadataConverter
	configurationPackageConverters []configurationPackageConverter
	providerPackageConverters      []providerPackageConverter
	packageLockConverters          []packageLockConverter
	categoricalConverters          map[Category][]CategoricalConverter
	scheme                         *runtime.Scheme
	claimTypes                     []schema.GroupVersionKind
	compositeTypes                 []schema.GroupVersionKind
}

// NewRegistry returns a new Registry initialized with
// the specified runtime.Scheme.
func NewRegistry(scheme *runtime.Scheme) *Registry {
	return &Registry{
		resourceConverters:        make(map[schema.GroupVersionKind]ResourceConverter),
		templateConverters:        make(map[schema.GroupVersionKind]ComposedTemplateConverter),
		categoricalConverters:     make(map[Category][]CategoricalConverter),
		unstructuredPreProcessors: make(map[Category][]UnstructuredPreProcessor),
		scheme:                    scheme,
	}
}

// make sure a converter is being registered for a managed resource,
// and it's registered with our runtime scheme.
// This will be needed, during runtime, for properly converting resources.
func (r *Registry) assertManagedResource(gvk schema.GroupVersionKind) {
	obj, err := r.scheme.New(gvk)
	if err != nil {
		panic(errors.Wrapf(err, errFmtNewObject, gvk))
	}
	if _, ok := obj.(resource.Managed); !ok {
		panic(errors.Errorf(errFmtNotManagedResource, gvk))
	}
}

// RegisterResourceConverter registers the specified ResourceConverter
// for the specified GVK with this Registry.
func (r *Registry) RegisterResourceConverter(gvk schema.GroupVersionKind, conv ResourceConverter) {
	r.assertManagedResource(gvk)
	r.resourceConverters[gvk] = conv
}

// RegisterTemplateConverter registers the specified ComposedTemplateConverter
// for the specified GVK with this Registry.
func (r *Registry) RegisterTemplateConverter(gvk schema.GroupVersionKind, conv ComposedTemplateConverter) {
	r.assertManagedResource(gvk)
	r.templateConverters[gvk] = conv
}

// RegisterCompositionConverter is a convenience method for registering both
// a ResourceConverter and a ComposedTemplateConverter that act on the same
// managed resource kind and are implemented by the same type.
func (r *Registry) RegisterCompositionConverter(gvk schema.GroupVersionKind, conv CompositionConverter) {
	r.RegisterResourceConverter(gvk, conv)
	r.RegisterTemplateConverter(gvk, conv)
}

// RegisterPatchSetConverter registers the given PatchSetConverter for
// the compositions whose name match the given regular expression.
func (r *Registry) RegisterPatchSetConverter(re *regexp.Regexp, psConv PatchSetConverter) {
	r.patchSetConverters = append(r.patchSetConverters, patchSetConverter{
		re:        re,
		converter: psConv,
	})
}

// RegisterConfigurationMetadataConverter registers the given ConfigurationMetadataConverter
// for the configurations whose name match the given regular expression.
func (r *Registry) RegisterConfigurationMetadataConverter(re *regexp.Regexp, confConv ConfigurationMetadataConverter) {
	r.configurationMetaConverters = append(r.configurationMetaConverters, configurationMetadataConverter{
		re:        re,
		converter: confConv,
	})
}

// RegisterConfigurationMetadataV1ConversionFunction registers the specified
// ConfigurationMetadataV1ConversionFn for the v1 configurations whose name match
// the given regular expression.
func (r *Registry) RegisterConfigurationMetadataV1ConversionFunction(re *regexp.Regexp, confConversionFn ConfigurationMetadataV1ConversionFn) {
	r.RegisterConfigurationMetadataConverter(re, &delegatingConverter{
		confMetaV1Fn: confConversionFn,
	})
}

// RegisterConfigurationMetadataV1Alpha1ConversionFunction registers the specified
// ConfigurationMetadataV1Alpha1ConversionFn for the v1alpha1 configurations
// whose name match the given regular expression.
func (r *Registry) RegisterConfigurationMetadataV1Alpha1ConversionFunction(re *regexp.Regexp, confConversionFn ConfigurationMetadataV1Alpha1ConversionFn) {
	r.RegisterConfigurationMetadataConverter(re, &delegatingConverter{
		confMetaV1Alpha1Fn: confConversionFn,
	})
}

// RegisterConfigurationPackageConverter registers the specified
// ConfigurationPackageConverter for the Configuration v1 packages whose reference
// match the given regular expression.
func (r *Registry) RegisterConfigurationPackageConverter(re *regexp.Regexp, pkgConv ConfigurationPackageConverter) {
	r.configurationPackageConverters = append(r.configurationPackageConverters, configurationPackageConverter{
		re:        re,
		converter: pkgConv,
	})
}

// RegisterConfigurationPackageV1ConversionFunction registers the specified
// ConfigurationPackageV1ConversionFn for the Configuration v1 packages whose reference
// match the given regular expression.
func (r *Registry) RegisterConfigurationPackageV1ConversionFunction(re *regexp.Regexp, confConversionFn ConfigurationPackageV1ConversionFn) {
	r.RegisterConfigurationPackageConverter(re, &delegatingConverter{
		confPackageV1Fn: confConversionFn,
	})
}

// RegisterProviderPackageConverter registers the given ProviderPackageConverter
// for the provider packages whose references match the given regular expression.
func (r *Registry) RegisterProviderPackageConverter(re *regexp.Regexp, pkgConv ProviderPackageConverter) {
	r.providerPackageConverters = append(r.providerPackageConverters, providerPackageConverter{
		re:        re,
		converter: pkgConv,
	})
}

// RegisterProviderPackageV1ConversionFunction registers the specified
// ProviderPackageV1ConversionFn for the provider v1 packages whose reference
// match the given regular expression.
func (r *Registry) RegisterProviderPackageV1ConversionFunction(re *regexp.Regexp, pkgConversionFn ProviderPackageV1ConversionFn) {
	r.RegisterProviderPackageConverter(re, &delegatingConverter{
		providerPackageV1Fn: pkgConversionFn,
	})
}

// RegisterPackageLockConverter registers the given PackageLockConverter.
func (r *Registry) RegisterPackageLockConverter(re *regexp.Regexp, lockConv PackageLockConverter) {
	r.packageLockConverters = append(r.packageLockConverters, packageLockConverter{
		re:        re,
		converter: lockConv,
	})
}

// RegisterCategoricalConverter registers the specified CategoricalConverter
// for the specified Category of resources.
func (r *Registry) RegisterCategoricalConverter(c Category, converter CategoricalConverter) {
	r.categoricalConverters[c] = append(r.categoricalConverters[c], converter)
}

// RegisterCategoricalConverterFunction registers the specified
// CategoricalConverterFunctionFn for the specified Category.
func (r *Registry) RegisterCategoricalConverterFunction(c Category, converterFn CategoricalConverterFunctionFn) {
	r.RegisterCategoricalConverter(c, &delegatingConverter{
		categoricalConverterFn: converterFn,
	})
}

// RegisterPackageLockV1Beta1ConversionFunction registers the specified
// RegisterPackageLockV1Beta1ConversionFunction for the package v1beta1 locks.
func (r *Registry) RegisterPackageLockV1Beta1ConversionFunction(re *regexp.Regexp, lockConversionFn PackageLockV1Beta1ConversionFn) {
	r.RegisterPackageLockConverter(re, &delegatingConverter{
		packageLockV1Beta1Fn: lockConversionFn,
	})
}

// AddToScheme registers types with this Registry's runtime.Scheme
func (r *Registry) AddToScheme(sb func(scheme *runtime.Scheme) error) error {
	return errors.Wrap(sb(r.scheme), errAddToScheme)
}

// AddCompositionTypes registers the Composition types with
// the registry's scheme. Only the v1 API of Compositions
// is currently supported.
func (r *Registry) AddCompositionTypes() error {
	return r.AddToScheme(xpv1.AddToScheme)
}

// AddClaimType registers a new composite resource claim type
// with the given GVK
func (r *Registry) AddClaimType(gvk schema.GroupVersionKind) {
	r.claimTypes = append(r.claimTypes, gvk)
}

// AddCompositeType registers a new composite resource type with the given GVK
func (r *Registry) AddCompositeType(gvk schema.GroupVersionKind) {
	r.compositeTypes = append(r.compositeTypes, gvk)
}

// GetManagedResourceGVKs returns a list of all registered managed resource
// GVKs
func (r *Registry) GetManagedResourceGVKs() []schema.GroupVersionKind {
	gvks := make([]schema.GroupVersionKind, 0, len(r.resourceConverters)+len(r.templateConverters))
	for gvk := range r.resourceConverters {
		gvks = append(gvks, gvk)
	}
	for gvk := range r.templateConverters {
		gvks = append(gvks, gvk)
	}
	return gvks
}

func (r *Registry) GetCompositionGVKs() []schema.GroupVersionKind {
	// Composition types are registered with this registry's scheme
	if _, ok := r.scheme.AllKnownTypes()[xpv1.CompositionGroupVersionKind]; ok {
		return []schema.GroupVersionKind{xpv1.CompositionGroupVersionKind}
	}
	return nil
}

// GetAllRegisteredGVKs returns a list of registered GVKs
// including v1.CompositionGroupVersionKind,
// metav1.ConfigurationGroupVersionKind,
// metav1alpha1.ConfigurationGroupVersionKind
// pkg.ConfigurationGroupVersionKind,
// pkg.ProviderGroupVersionKind,
// pkg.LockGroupVersionKind.
func (r *Registry) GetAllRegisteredGVKs() []schema.GroupVersionKind {
	gvks := make([]schema.GroupVersionKind, 0, len(r.claimTypes)+len(r.compositeTypes)+len(r.resourceConverters)+len(r.templateConverters)+1)
	gvks = append(gvks, r.claimTypes...)
	gvks = append(gvks, r.compositeTypes...)
	gvks = append(gvks, r.GetManagedResourceGVKs()...)
	gvks = append(gvks, xpv1.CompositionGroupVersionKind, xpmetav1.ConfigurationGroupVersionKind, xpmetav1alpha1.ConfigurationGroupVersionKind,
		xppkgv1.ConfigurationGroupVersionKind, xppkgv1.ProviderGroupVersionKind, xppkgv1beta1.LockGroupVersionKind)
	return gvks
}

// ResourceConversionFn is a function that converts the specified migration
// source managed resource to one or more migration target managed resources.
type ResourceConversionFn func(mg resource.Managed) ([]resource.Managed, error)

// ComposedTemplateConversionFn is a function that converts from the specified
// migration source v1.ComposedTemplate to one or more migration
// target v1.ComposedTemplates.
type ComposedTemplateConversionFn func(sourceTemplate xpv1.ComposedTemplate, convertedTemplates ...*xpv1.ComposedTemplate) error

// PatchSetsConversionFn is a function that converts
// the `spec.patchSets` of a Composition from the migration source provider's
// schema to the migration target provider's schema.
type PatchSetsConversionFn func(psMap map[string]*xpv1.PatchSet) error

// ConfigurationMetadataV1ConversionFn is a function that converts the specified
// migration source Configuration v1 metadata to the migration target
// Configuration metadata.
type ConfigurationMetadataV1ConversionFn func(configuration *xpmetav1.Configuration) error

// ConfigurationMetadataV1Alpha1ConversionFn is a function that converts the specified
// migration source Configuration v1alpha1 metadata to the migration target
// Configuration metadata.
type ConfigurationMetadataV1Alpha1ConversionFn func(configuration *xpmetav1alpha1.Configuration) error

// PackageLockV1Beta1ConversionFn is a function that converts the specified
// migration source package v1beta1 lock to the migration target
// package lock.
type PackageLockV1Beta1ConversionFn func(pkg *xppkgv1beta1.Lock) error

// ConfigurationPackageV1ConversionFn is a function that converts the specified
// migration source Configuration v1 package to the migration target
// Configuration package(s).
type ConfigurationPackageV1ConversionFn func(pkg *xppkgv1.Configuration) error

// ProviderPackageV1ConversionFn is a function that converts the specified
// migration source provider v1 package to the migration target
// Provider package(s).
type ProviderPackageV1ConversionFn func(pkg xppkgv1.Provider) ([]xppkgv1.Provider, error)

// CategoricalConverterFunctionFn is a function that converts resources of a
// Category. Because it receives an unstructured argument, it should be
// used for implementing generic conversion functions acting on a specific
// category.
type CategoricalConverterFunctionFn func(u *UnstructuredWithMetadata) error

type delegatingConverter struct {
	rFn                    ResourceConversionFn
	cmpFn                  ComposedTemplateConversionFn
	psFn                   PatchSetsConversionFn
	confMetaV1Fn           ConfigurationMetadataV1ConversionFn
	confMetaV1Alpha1Fn     ConfigurationMetadataV1Alpha1ConversionFn
	confPackageV1Fn        ConfigurationPackageV1ConversionFn
	providerPackageV1Fn    ProviderPackageV1ConversionFn
	packageLockV1Beta1Fn   PackageLockV1Beta1ConversionFn
	categoricalConverterFn CategoricalConverterFunctionFn
}

func (d *delegatingConverter) Convert(u *UnstructuredWithMetadata) error {
	if d.categoricalConverterFn == nil {
		return nil
	}
	return d.categoricalConverterFn(u)
}

func (d *delegatingConverter) ConfigurationPackageV1(pkg *xppkgv1.Configuration) error {
	if d.confPackageV1Fn == nil {
		return nil
	}
	return d.confPackageV1Fn(pkg)
}

func (d *delegatingConverter) PackageLockV1Beta1(lock *xppkgv1beta1.Lock) error {
	if d.packageLockV1Beta1Fn == nil {
		return nil
	}
	return d.packageLockV1Beta1Fn(lock)
}

func (d *delegatingConverter) ProviderPackageV1(pkg xppkgv1.Provider) ([]xppkgv1.Provider, error) {
	if d.providerPackageV1Fn == nil {
		return []xppkgv1.Provider{pkg}, nil
	}
	return d.providerPackageV1Fn(pkg)
}

func (d *delegatingConverter) ConfigurationMetadataV1(c *xpmetav1.Configuration) error {
	if d.confMetaV1Fn == nil {
		return nil
	}
	return d.confMetaV1Fn(c)
}

func (d *delegatingConverter) ConfigurationMetadataV1Alpha1(c *xpmetav1alpha1.Configuration) error {
	if d.confMetaV1Alpha1Fn == nil {
		return nil
	}
	return d.confMetaV1Alpha1Fn(c)
}

func (d *delegatingConverter) PatchSets(psMap map[string]*xpv1.PatchSet) error {
	if d.psFn == nil {
		return nil
	}
	return d.psFn(psMap)
}

// Resource takes a managed resource and returns zero or more managed
// resources to be created by calling the configured ResourceConversionFn.
func (d *delegatingConverter) Resource(mg resource.Managed) ([]resource.Managed, error) {
	if d.rFn == nil {
		return []resource.Managed{mg}, nil
	}
	return d.rFn(mg)
}

// ComposedTemplate converts from the specified migration source
// v1.ComposedTemplate to the migration target schema by calling the configured
// ComposedTemplateConversionFn.
func (d *delegatingConverter) ComposedTemplate(sourceTemplate xpv1.ComposedTemplate, convertedTemplates ...*xpv1.ComposedTemplate) error {
	if d.cmpFn == nil {
		return nil
	}
	return d.cmpFn(sourceTemplate, convertedTemplates...)
}

// RegisterAPIConversionFunctions registers the supplied ResourceConversionFn and
// ComposedTemplateConversionFn for the specified GVK, and the supplied
// PatchSetsConversionFn for all the discovered Compositions.
// The specified GVK must belong to a Crossplane managed resource type and
// the type must already have been registered with this registry's scheme
// by calling Registry.AddToScheme.
func (r *Registry) RegisterAPIConversionFunctions(gvk schema.GroupVersionKind, rFn ResourceConversionFn, cmpFn ComposedTemplateConversionFn, psFn PatchSetsConversionFn) {
	d := &delegatingConverter{
		rFn:   rFn,
		cmpFn: cmpFn,
		psFn:  psFn,
	}
	r.RegisterPatchSetConverter(AllCompositions, d)
	r.RegisterCompositionConverter(gvk, d)
}

// RegisterConversionFunctions registers the supplied ResourceConversionFn and
// ComposedTemplateConversionFn for the specified GVK, and the supplied
// PatchSetsConversionFn for all the discovered Compositions.
// The specified GVK must belong to a Crossplane managed resource type and
// the type must already have been registered with this registry's scheme
// by calling Registry.AddToScheme.
// Deprecated: Use RegisterAPIConversionFunctions instead.
func (r *Registry) RegisterConversionFunctions(gvk schema.GroupVersionKind, rFn ResourceConversionFn, cmpFn ComposedTemplateConversionFn, psFn PatchSetsConversionFn) {
	r.RegisterAPIConversionFunctions(gvk, rFn, cmpFn, psFn)
}

func (r *Registry) RegisterPreProcessor(category Category, pp UnstructuredPreProcessor) {
	r.unstructuredPreProcessors[category] = append(r.unstructuredPreProcessors[category], pp)
}

// PreProcessor is a function type to convert pre-processor functions to
// UnstructuredPreProcessor.
type PreProcessor func(u UnstructuredWithMetadata) error

func (pp PreProcessor) PreProcess(u UnstructuredWithMetadata) error {
	if pp == nil {
		return nil
	}
	return pp(u)
}
