/*
Copyright 2022 Upbound Inc.
*/

package migration

import (
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
)

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

type Source interface {
	HasNext() (bool, error)
	Next() (UnstructuredWithMetadata, error)
}

type Target interface {
	Put(o UnstructuredWithMetadata) error
	Patch(o UnstructuredWithMetadata) error
	Delete(o UnstructuredWithMetadata) error
}
