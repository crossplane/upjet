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
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	errFmtNewObject          = "failed to instantiate a new runtime.Object using scheme.Scheme for: %s"
	errFmtNotManagedResource = "specified GVK does not belong to a managed resource: %s"
)

var (
	// the default Converter registry
	registry Registry = make(map[schema.GroupVersionKind]Converter)
)

// ResourceConversionFn is a function that converts the specified migration
// source managed resource to one or more migration target managed resources.
type ResourceConversionFn func(mg resource.Managed) ([]resource.Managed, error)

// ComposedTemplateConversionFn is a function that converts from the specified
// v1.ComposedTemplate's migration source resources to one or more migration
// target resources.
type ComposedTemplateConversionFn func(cmp v1.ComposedTemplate, convertedBase ...*v1.ComposedTemplate) error

// Registry is a registry of `migration.Converter`s keyed with
// the associated `schema.GroupVersionKind`s.
type Registry map[schema.GroupVersionKind]Converter

// RegisterConverter registers the specified migration.Converter for the
// specified GVK with the default Registry.
func RegisterConverter(gvk schema.GroupVersionKind, conv Converter) {
	// make sure a converter is being registered for a managed resource,
	// and it's registered with our runtime scheme.
	// This will be needed, during runtime, for properly converting resources
	obj, err := scheme.Scheme.New(gvk)
	if err != nil {
		panic(errors.Wrapf(err, errFmtNewObject, gvk))
	}
	if _, ok := obj.(resource.Managed); !ok {
		panic(errors.Errorf(errFmtNotManagedResource, gvk))
	}
	registry[gvk] = conv
}

type delegatingConverter struct {
	rFn   ResourceConversionFn
	cmpFn ComposedTemplateConversionFn
}

// Resources converts from the specified migration source resource to
// the migration target resources by calling the configured ResourceConversionFn.
func (d delegatingConverter) Resources(mg resource.Managed) ([]resource.Managed, error) {
	return d.rFn(mg)
}

// ComposedTemplates converts from the specified migration source
// v1.ComposedTemplate to the migration target schema by calling the configured
// ComposedTemplateConversionFn.
func (d delegatingConverter) ComposedTemplates(cmp v1.ComposedTemplate, convertedBase ...*v1.ComposedTemplate) error {
	return d.cmpFn(cmp, convertedBase...)
}

// RegisterConversionFunctions registers the supplied ResourceConversionFn and
// ComposedTemplateConversionFn for the specified GVK.
// The specified GVK must belong to a Crossplane managed resource type and
// the type must already have been registered with the client-go's
// default scheme.
func RegisterConversionFunctions(gvk schema.GroupVersionKind, rFn ResourceConversionFn, cmpFn ComposedTemplateConversionFn) {
	RegisterConverter(gvk, delegatingConverter{
		rFn:   rFn,
		cmpFn: cmpFn,
	})
}
