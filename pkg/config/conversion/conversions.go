// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"fmt"
	"slices"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
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
	pathForProvider  = "spec.forProvider"
	pathInitProvider = "spec.initProvider"
	pathAtProvider   = "status.atProvider"
)

var (
	_ PrioritizedManagedConversion = &identityConversion{}
	_ PavedConversion              = &fieldCopy{}
	_ PavedConversion              = &singletonListConverter{}
)

// Conversion is the interface for the CRD API version converters.
// Conversion implementations registered for a source, target
// pair are called in chain so Conversion implementations can be modular, e.g.,
// a Conversion implementation registered for a specific source and target
// versions does not have to contain all the needed API conversions between
// these two versions. All PavedConversions are run in their registration
// order before the ManagedConversions. Conversions are run in three stages:
//  1. PrioritizedManagedConversions are run.
//  2. The source and destination objects are paved and the PavedConversions are
//     run in chain without unpaving the unstructured representation between
//     conversions.
//  3. The destination paved object is converted back to a managed resource and
//     ManagedConversions are run in the order they are registered.
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

// PrioritizedManagedConversion is a ManagedConversion that take precedence
// over all the other converters. PrioritizedManagedConversions are run,
// in their registration order, before the PavedConversions.
type PrioritizedManagedConversion interface {
	ManagedConversion
	Prioritized()
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
	pathPrefixes   []string
	crdPaths       []string
	mode           ListConversionMode
	convertOptions *ConvertOptions
}

type SingletonListConversionOption func(*singletonListConverter)

// WithConvertOptions sets the ConvertOptions for the singleton list conversion.
func WithConvertOptions(opts *ConvertOptions) SingletonListConversionOption {
	return func(s *singletonListConverter) {
		s.convertOptions = opts
	}
}

// NewSingletonListConversion returns a new Conversion from the specified
// sourceVersion of an API to the specified targetVersion and uses the
// CRD field paths given in crdPaths to convert between the singleton
// lists and embedded objects in the given conversion mode.
func NewSingletonListConversion(sourceVersion, targetVersion string, pathPrefixes []string, crdPaths []string, mode ListConversionMode, opts ...SingletonListConversionOption) Conversion {
	s := &singletonListConverter{
		baseConversion: newBaseConversion(sourceVersion, targetVersion),
		pathPrefixes:   pathPrefixes,
		crdPaths:       crdPaths,
		mode:           mode,
	}

	for _, o := range opts {
		o(s)
	}

	return s
}

func (s *singletonListConverter) ConvertPaved(src, target *fieldpath.Paved) (bool, error) {
	if !s.Applicable(&unstructured.Unstructured{Object: src.UnstructuredContent()},
		&unstructured.Unstructured{Object: target.UnstructuredContent()}) {
		return false, nil
	}
	if len(s.crdPaths) == 0 {
		return false, nil
	}

	for _, p := range s.pathPrefixes {
		v, err := src.GetValue(p)
		if err != nil {
			return true, errors.Wrapf(err, "failed to read the %s value for conversion in mode %q", p, s.mode)
		}
		m, ok := v.(map[string]any)
		if !ok {
			return true, errors.Errorf("value at path %s is not a map[string]any", p)
		}
		if _, err := Convert(m, s.crdPaths, s.mode, s.convertOptions); err != nil {
			return true, errors.Wrapf(err, "failed to convert the source map in mode %q with %s", s.mode, s.baseConversion.String())
		}
		if err := target.SetValue(p, m); err != nil {
			return true, errors.Wrapf(err, "failed to set the %s value for conversion in mode %q", p, s.mode)
		}
	}
	return true, nil
}

type identityConversion struct {
	baseConversion
	excludePaths []string
}

func (i *identityConversion) ConvertManaged(src, target resource.Managed) (bool, error) {
	if !i.Applicable(src, target) {
		return false, nil
	}

	srcCopy := src.DeepCopyObject()
	srcRaw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(srcCopy)
	if err != nil {
		return false, errors.Wrap(err, "cannot convert the source managed resource into an unstructured representation")
	}

	// remove excluded fields
	if len(i.excludePaths) > 0 {
		pv := fieldpath.Pave(srcRaw)
		for _, ex := range i.excludePaths {
			exPaths, err := pv.ExpandWildcards(ex)
			if err != nil {
				return false, errors.Wrapf(err, "cannot expand wildcards in the fieldpath expression %s", ex)
			}
			for _, p := range exPaths {
				if err := pv.DeleteField(p); err != nil {
					return false, errors.Wrapf(err, "cannot delete a field in the conversion source object")
				}
			}
		}
	}

	// copy the remaining fields
	gvk := target.GetObjectKind().GroupVersionKind()
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(srcRaw, target); err != nil {
		return true, errors.Wrap(err, "cannot convert the map[string]any representation of the source object to the conversion target object")
	}
	// restore the original GVK for the conversion destination
	target.GetObjectKind().SetGroupVersionKind(gvk)
	return true, nil
}

func (i *identityConversion) Prioritized() {}

// newIdentityConversion returns a new Conversion from the specified
// sourceVersion of an API to the specified targetVersion, which copies the
// identical paths from the source to the target. excludePaths can be used
// to ignore certain field paths while copying.
func newIdentityConversion(sourceVersion, targetVersion string, excludePaths ...string) Conversion {
	return &identityConversion{
		baseConversion: newBaseConversion(sourceVersion, targetVersion),
		excludePaths:   excludePaths,
	}
}

// NewIdentityConversionExpandPaths returns a new Conversion from the specified
// sourceVersion of an API to the specified targetVersion, which copies the
// identical paths from the source to the target. excludePaths can be used
// to ignore certain field paths while copying. Exclude paths must be specified
// in standard crossplane-runtime fieldpath library syntax, i.e., with proper
// indices for traversing map and slice types (e.g., a.b[*].c).
// The field paths in excludePaths are sorted in lexical order and are prefixed
// with each of the path prefixes specified with pathPrefixes. So if an
// exclude path "x" is specified with the prefix slice ["a", "b"], then
// paths a.x and b.x will both be skipped while copying fields from a source to
// a target.
func NewIdentityConversionExpandPaths(sourceVersion, targetVersion string, pathPrefixes []string, excludePaths ...string) Conversion {
	return newIdentityConversion(sourceVersion, targetVersion, ExpandParameters(pathPrefixes, excludePaths...)...)
}

// ExpandParameters sorts and expands the given list of field path suffixes
// with the given prefixes.
func ExpandParameters(prefixes []string, excludePaths ...string) []string {
	slices.Sort(excludePaths)
	if len(prefixes) == 0 {
		return excludePaths
	}

	r := make([]string, 0, len(prefixes)*len(excludePaths))
	for _, p := range prefixes {
		for _, ex := range excludePaths {
			r = append(r, fmt.Sprintf("%s.%s", p, ex))
		}
	}
	return r
}

// DefaultPathPrefixes returns the list of the default path prefixes for
// excluding paths in the identity conversion. The returned value is
// ["spec.forProvider", "spec.initProvider", "status.atProvider"].
func DefaultPathPrefixes() []string {
	return []string{pathForProvider, pathInitProvider, pathAtProvider}
}
