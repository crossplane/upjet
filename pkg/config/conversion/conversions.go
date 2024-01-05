// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	AllVersions = "*"
)

type Conversion interface {
	Applicable(src, dst runtime.Object) bool
}

type PavedConversion interface {
	Conversion
	// ConvertPaved converts from the `src` paved object to the `dst`
	// paved object and returns `true` if the conversion has been done,
	// `false` otherwise, together with any errors encountered.
	ConvertPaved(src, target *fieldpath.Paved) (bool, error)
}

type TerraformedConversion interface {
	Conversion
	// ConvertTerraformed converts from the `src` managed resource to the `dst`
	// managed resource and returns `true` if the conversion has been done,
	// `false` otherwise, together with any errors encountered.
	ConvertTerraformed(src, target resource.Managed) (bool, error)
}

type baseConversion struct {
	sourceVersion string
	targetVersion string
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

func NewFieldRenameConversion(sourceVersion, sourceField, targetVersion, targetField string) Conversion {
	return &fieldCopy{
		baseConversion: newBaseConversion(sourceVersion, targetVersion),
		sourceField:    sourceField,
		targetField:    targetField,
	}
}
