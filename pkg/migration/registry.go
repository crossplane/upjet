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
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// AllCompositions matches all v1.Composition names.
	AllCompositions = regexp.MustCompile(`.*`)
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

// Registry is a registry of `migration.Converter`s keyed with
// the associated `schema.GroupVersionKind`s and an associated
// runtime.Scheme with which the corresponding types are registered.
type Registry struct {
	resourceConverters map[schema.GroupVersionKind]ResourceConverter
	templateConverters map[schema.GroupVersionKind]ComposedTemplateConverter
	patchSetConverters []patchSetConverter
	scheme             *runtime.Scheme
	claimTypes         []schema.GroupVersionKind
	compositeTypes     []schema.GroupVersionKind
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

// RegisterPatchSetConverter registers the given PatchSetConversionFn for
// the compositions whose name match the given regular expression.
func (r *Registry) RegisterPatchSetConverter(re *regexp.Regexp, psConv PatchSetConverter) {
	r.patchSetConverters = append(r.patchSetConverters, patchSetConverter{
		re:        re,
		converter: psConv,
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
// including v1.CompositionGroupVersionKind
func (r *Registry) GetAllRegisteredGVKs() []schema.GroupVersionKind {
	gvks := make([]schema.GroupVersionKind, 0, len(r.claimTypes)+len(r.compositeTypes)+len(r.resourceConverters)+len(r.templateConverters)+1)
	gvks = append(gvks, r.claimTypes...)
	gvks = append(gvks, r.compositeTypes...)
	gvks = append(gvks, r.GetManagedResourceGVKs()...)
	gvks = append(gvks, xpv1.CompositionGroupVersionKind)
	return gvks
}
