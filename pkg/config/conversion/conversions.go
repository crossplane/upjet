// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"fmt"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// AllVersions denotes that a Conversion is applicable for all versions
	// of an API with which the Conversion is registered. It can be used for
	// both the conversion source or target API versions.
	AllVersions = "*"
)

const (
	pathForProvider = "spec.forProvider"
)

// Conversion is the interface for the API version converters.
// Conversion implementations registered for a source, target
// pair are called in chain so Conversion implementations can be modular, e.g.,
// a Conversion implementation registered for a specific source and target
// versions does not have to contain all the needed API conversions between
// these two versions.
type Conversion interface {
	// Applicable should return true if this Conversion is applicable while
	// converting the API of the `src` object to the API of the `dst` object.
	Applicable(src, dst runtime.Object) bool
}

// PavedConversion is an optimized Conversion between two fieldpath.Paved
// objects. PavedConversion implementations for a specific source and target
// version pair are chained together and the source and the destination objects
// are paved once at the beginning of the chained PavedConversion.ConvertPaved
// calls. The target fieldpath.Paved object is then converted into the original
// resource.Terraformed object at the end of the chained calls. This prevents
// the intermediate conversions between fieldpath.Paved and
// the resource.Terraformed representations of the same object, and the
// fieldpath.Paved representation is convenient for writing generic
// Conversion implementations not bound to a specific type.
type PavedConversion interface {
	Conversion
	// ConvertPaved converts from the `src` paved object to the `dst`
	// paved object and returns `true` if the conversion has been done,
	// `false` otherwise, together with any errors encountered.
	ConvertPaved(src, target *fieldpath.Paved) (bool, error)
}

// ManagedConversion defines a Conversion from a specific source
// resource.Managed type to a target one. Generic Conversion
// implementations may prefer to implement the PavedConversion interface.
// Implementations of ManagedConversion can do type assertions to
// specific source and target types, and so, they are expected to be
// strongly typed.
type ManagedConversion interface {
	Conversion
	// ConvertManaged converts from the `src` managed resource to the `dst`
	// managed resource and returns `true` if the conversion has been done,
	// `false` otherwise, together with any errors encountered.
	ConvertManaged(src, target resource.Managed) (bool, error)
}

type baseConversion struct {
	sourceVersion string
	targetVersion string
}

func (c *baseConversion) String() string {
	return fmt.Sprintf("source API version %q, target API version %q", c.sourceVersion, c.targetVersion)
}

func newBaseConversion(sourceVersion, targetVersion string) baseConversion {
	return baseConversion{
		sourceVersion: sourceVersion,
		targetVersion: targetVersion,
	}
}

func (c *baseConversion) Applicable(src, dst runtime.Object) bool {
	return (c.sourceVersion == AllVersions || c.sourceVersion == src.GetObjectKind().GroupVersionKind().Version) &&
		(c.targetVersion == AllVersions || c.targetVersion == dst.GetObjectKind().GroupVersionKind().Version)
}

type fieldCopy struct {
	baseConversion
	sourceField string
	targetField string
}

func (f *fieldCopy) ConvertPaved(src, target *fieldpath.Paved) (bool, error) {
	if !f.Applicable(&unstructured.Unstructured{Object: src.UnstructuredContent()},
		&unstructured.Unstructured{Object: target.UnstructuredContent()}) {
		return false, nil
	}
	v, err := src.GetValue(f.sourceField)
	// TODO: the field might actually exist in the schema and
	// missing in the object. Or, it may not exist in the schema.
	// For a field that does not exist in the schema, we had better error.
	if fieldpath.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "failed to get the field %q from the conversion source object", f.sourceField)
	}
	return true, errors.Wrapf(target.SetValue(f.targetField, v), "failed to set the field %q of the conversion target object", f.targetField)
}

// NewFieldRenameConversion returns a new Conversion that implements a
// field renaming conversion from the specified `sourceVersion` to the specified
// `targetVersion` of an API. The field's name in the `sourceVersion` is given
// with the `sourceField` parameter and its name in the `targetVersion` is
// given with `targetField` parameter.
func NewFieldRenameConversion(sourceVersion, sourceField, targetVersion, targetField string) Conversion {
	return &fieldCopy{
		baseConversion: newBaseConversion(sourceVersion, targetVersion),
		sourceField:    sourceField,
		targetField:    targetField,
	}
}

type customConverter func(src, target resource.Managed) error

type customConversion struct {
	baseConversion
	customConverter customConverter
}

func (cc *customConversion) ConvertManaged(src, target resource.Managed) (bool, error) {
	if !cc.Applicable(src, target) || cc.customConverter == nil {
		return false, nil
	}
	return true, errors.Wrap(cc.customConverter(src, target), "failed to apply the converter function")
}

// NewCustomConverter returns a new Conversion from the specified
// `sourceVersion` of an API to the specified `targetVersion` and invokes
// the specified converter function to perform the conversion on the
// managed resources.
func NewCustomConverter(sourceVersion, targetVersion string, converter func(src, target resource.Managed) error) Conversion {
	return &customConversion{
		baseConversion:  newBaseConversion(sourceVersion, targetVersion),
		customConverter: converter,
	}
}

type singletonListConverter struct {
	baseConversion
	crdPaths []string
	mode     Mode
}

// NewSingletonListConversion returns a new Conversion from the specified
// sourceVersion of an API to the specified targetVersion and uses the
// CRD field paths given in crdPaths to convert between the singleton
// lists and embedded objects in the given conversion mode.
func NewSingletonListConversion(sourceVersion, targetVersion string, crdPaths []string, mode Mode) Conversion {
	return &singletonListConverter{
		baseConversion: newBaseConversion(sourceVersion, targetVersion),
		crdPaths:       crdPaths,
		mode:           mode,
	}
}

func (s *singletonListConverter) ConvertPaved(src, target *fieldpath.Paved) (bool, error) {
	if len(s.crdPaths) == 0 {
		return false, nil
	}
	v, err := src.GetValue(pathForProvider)
	if err != nil {
		return true, errors.Wrapf(err, "failed to read the %s value for conversion in mode %q", pathForProvider, s.mode)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return true, errors.Errorf("value at path %s is not a map[string]any", pathForProvider)
	}
	if _, err := Convert(m, s.crdPaths, s.mode); err != nil {
		return true, errors.Wrapf(err, "failed to convert the source map in mode %q with %s", s.mode, s.baseConversion)
	}
	return true, errors.Wrapf(target.SetValue(pathForProvider, m), "failed to set the %s value for conversion in mode %q", pathForProvider, s.mode)
}
