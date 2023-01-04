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
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	errAddToScheme           = "failed to register types with the registry's scheme"
	errFmtNewObject          = "failed to instantiate a new runtime.Object using runtime.Scheme for: %s"
	errFmtNotManagedResource = "specified GVK does not belong to a managed resource: %s"
)

// ResourceConversionFn is a function that converts the specified migration
// source managed resource to one or more migration target managed resources.
type ResourceConversionFn func(mg resource.Managed) ([]resource.Managed, error)

// CompositionConversionFn is a function that converts from the specified
// v1.ComposedTemplate's migration source resources to one or more migration
// target resources.
type CompositionConversionFn func(sourcePatchSets []xpv1.PatchSet, sourceTemplate xpv1.ComposedTemplate, convertedTemplates ...*xpv1.ComposedTemplate) ([]xpv1.PatchSet, error)

// Registry is a registry of `migration.Converter`s keyed with
// the associated `schema.GroupVersionKind`s and an associated
// runtime.Scheme with which the corresponding types are registered.
type Registry struct {
	converters     map[schema.GroupVersionKind]Converter
	scheme         *runtime.Scheme
	claimTypes     []schema.GroupVersionKind
	compositeTypes []schema.GroupVersionKind
}

// NewRegistry returns a new Registry initialized with
// the specified runtime.Scheme.
func NewRegistry(scheme *runtime.Scheme) *Registry {
	return &Registry{
		converters: make(map[schema.GroupVersionKind]Converter),
		scheme:     scheme,
	}
}

// RegisterConverter registers the specified migration.Converter for the
// specified GVK with the default Registry.
func (r *Registry) RegisterConverter(gvk schema.GroupVersionKind, conv Converter) {
	// make sure a converter is being registered for a managed resource,
	// and it's registered with our runtime scheme.
	// This will be needed, during runtime, for properly converting resources
	obj, err := r.scheme.New(gvk)
	if err != nil {
		panic(errors.Wrapf(err, errFmtNewObject, gvk))
	}
	if _, ok := obj.(resource.Managed); !ok {
		panic(errors.Errorf(errFmtNotManagedResource, gvk))
	}
	r.converters[gvk] = conv
}

type delegatingConverter struct {
	rFn    ResourceConversionFn
	compFn CompositionConversionFn
}

// Resources converts from the specified migration source resource to
// the migration target resources by calling the configured ResourceConversionFn.
func (d delegatingConverter) Resources(mg resource.Managed) ([]resource.Managed, error) {
	if d.rFn == nil {
		return []resource.Managed{mg}, nil
	}
	return d.rFn(mg)
}

// Composition converts from the specified migration source
// v1.ComposedTemplate to the migration target schema by calling the configured
// ComposedTemplateConversionFn.
func (d delegatingConverter) Composition(sourcePatchSets []xpv1.PatchSet, sourceTemplate xpv1.ComposedTemplate, convertedTemplates ...*xpv1.ComposedTemplate) ([]xpv1.PatchSet, error) {
	if d.compFn == nil {
		return sourcePatchSets, nil
	}
	return d.compFn(sourcePatchSets, sourceTemplate, convertedTemplates...)
}

// RegisterConversionFunctions registers the supplied ResourceConversionFn and
// ComposedTemplateConversionFn for the specified GVK.
// The specified GVK must belong to a Crossplane managed resource type and
// the type must already have been registered with the client-go's
// default scheme.
func (r *Registry) RegisterConversionFunctions(gvk schema.GroupVersionKind, rFn ResourceConversionFn, compFn CompositionConversionFn) {
	r.RegisterConverter(gvk, delegatingConverter{
		rFn:    rFn,
		compFn: compFn,
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
	gvks := make([]schema.GroupVersionKind, 0, len(r.converters))
	for gvk := range r.converters {
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
// including v1.CompositionGroupVersionKind
func (r *Registry) GetAllRegisteredGVKs() []schema.GroupVersionKind {
	gvks := make([]schema.GroupVersionKind, 0, len(r.claimTypes)+len(r.compositeTypes)+len(r.converters)+1)
	gvks = append(gvks, r.claimTypes...)
	gvks = append(gvks, r.compositeTypes...)
	gvks = append(gvks, r.GetManagedResourceGVKs()...)
	gvks = append(gvks, xpv1.CompositionGroupVersionKind)
	return gvks
}
