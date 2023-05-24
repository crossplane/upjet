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
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// AllCompositions matches all v1.Composition names.
	AllCompositions = regexp.MustCompile(`.*`)
	// AllConfigurations matches all metav1.Configuration names.
	AllConfigurations = regexp.MustCompile(`.*`)
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

type configurationConverter struct {
	// re is the regular expression against which a Configuration's name
	// will be matched to determine whether the conversion function
	// will be invoked.
	re *regexp.Regexp
	// converter is the ConfigurationConverter to be run on the Configuration's
	// metadata.
	converter ConfigurationConverter
}

// Registry is a registry of `migration.Converter`s keyed with
// the associated `schema.GroupVersionKind`s and an associated
// runtime.Scheme with which the corresponding types are registered.
type Registry struct {
	resourceConverters      map[schema.GroupVersionKind]ResourceConverter
	templateConverters      map[schema.GroupVersionKind]ComposedTemplateConverter
	patchSetConverters      []patchSetConverter
	configurationConverters []configurationConverter
	scheme                  *runtime.Scheme
	claimTypes              []schema.GroupVersionKind
	compositeTypes          []schema.GroupVersionKind
}

// NewRegistry returns a new Registry initialized with
// the specified runtime.Scheme.
func NewRegistry(scheme *runtime.Scheme) *Registry {
	return &Registry{
		resourceConverters: make(map[schema.GroupVersionKind]ResourceConverter),
		templateConverters: make(map[schema.GroupVersionKind]ComposedTemplateConverter),
		scheme:             scheme,
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

// RegisterConfigurationConverter registers the given ConfigurationConverter
// for the configurations whose name match the given regular expression.
func (r *Registry) RegisterConfigurationConverter(re *regexp.Regexp, confConv ConfigurationConverter) {
	r.configurationConverters = append(r.configurationConverters, configurationConverter{
		re:        re,
		converter: confConv,
	})
}

func (r *Registry) RegisterConfigurationV1ConversionFunction(re *regexp.Regexp, confConversionFn ConfigurationV1ConversionFn) {
	r.RegisterConfigurationConverter(re, &delegatingConverter{
		confV1Fn: confConversionFn,
	})
}

func (r *Registry) RegisterConfigurationV1Alpha1ConversionFunction(re *regexp.Regexp, confConversionFn ConfigurationV1Alpha1ConversionFn) {
	r.RegisterConfigurationConverter(re, &delegatingConverter{
		confV1Alpha1Fn: confConversionFn,
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
// metav1.ConfigurationGroupVersionKind and
// metav1alpha1.ConfigurationGroupVersionKind.
func (r *Registry) GetAllRegisteredGVKs() []schema.GroupVersionKind {
	gvks := make([]schema.GroupVersionKind, 0, len(r.claimTypes)+len(r.compositeTypes)+len(r.resourceConverters)+len(r.templateConverters)+1)
	gvks = append(gvks, r.claimTypes...)
	gvks = append(gvks, r.compositeTypes...)
	gvks = append(gvks, r.GetManagedResourceGVKs()...)
	gvks = append(gvks, xpv1.CompositionGroupVersionKind, xpmetav1.ConfigurationGroupVersionKind, xpmetav1alpha1.ConfigurationGroupVersionKind)
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

// ConfigurationV1ConversionFn is a function that converts the specified
// migration source Configuration v1 metadata to the migration target
// Configuration metadata.
type ConfigurationV1ConversionFn func(configuration *xpmetav1.Configuration) error

// ConfigurationV1Alpha1ConversionFn is a function that converts the specified
// migration source Configuration v1alpha1 metadata to the migration target
// Configuration metadata.
type ConfigurationV1Alpha1ConversionFn func(configuration *xpmetav1alpha1.Configuration) error

type delegatingConverter struct {
	rFn            ResourceConversionFn
	cmpFn          ComposedTemplateConversionFn
	psFn           PatchSetsConversionFn
	confV1Fn       ConfigurationV1ConversionFn
	confV1Alpha1Fn ConfigurationV1Alpha1ConversionFn
}

func (d *delegatingConverter) ConfigurationV1(c *xpmetav1.Configuration) error {
	if d.confV1Fn == nil {
		return nil
	}
	return d.confV1Fn(c)
}

func (d *delegatingConverter) ConfigurationV1Alpha1(c *xpmetav1alpha1.Configuration) error {
	if d.confV1Alpha1Fn == nil {
		return nil
	}
	return d.confV1Alpha1Fn(c)
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

// RegisterConversionFunctions registers the supplied ResourceConversionFn and
// ComposedTemplateConversionFn for the specified GVK, and the supplied
// PatchSetsConversionFn for all the discovered Compositions.
// The specified GVK must belong to a Crossplane managed resource type and
// the type must already have been registered with this registry's scheme
// by calling Registry.AddToScheme.
func (r *Registry) RegisterConversionFunctions(gvk schema.GroupVersionKind, rFn ResourceConversionFn, cmpFn ComposedTemplateConversionFn, psFn PatchSetsConversionFn) {
	d := &delegatingConverter{
		rFn:   rFn,
		cmpFn: cmpFn,
		psFn:  psFn,
	}
	r.RegisterPatchSetConverter(AllCompositions, d)
	r.RegisterCompositionConverter(gvk, d)
}
