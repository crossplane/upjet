// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
)

const (
	AllVersions = "*"
)

const (
	pathObjectMeta = "ObjectMeta"
)

type Conversion interface {
	GetSourceVersion() string
	GetTargetVersion() string
}

type PavedConversion interface {
	Conversion
	ConvertPaved(src, target fieldpath.Paved) error
}

type ManagedConversion interface {
	Conversion
	ConvertTerraformed(src, target resource.Managed)
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

func (c *baseConversion) GetSourceVersion() string {
	return c.sourceVersion
}

func (c *baseConversion) GetTargetVersion() string {
	return c.targetVersion
}

type fieldCopy struct {
	baseConversion
	sourceField string
	targetField string
}

func (f *fieldCopy) ConvertPaved(src, target fieldpath.Paved) error {
	v, err := src.GetValue(f.sourceField)
	if err != nil {
		return errors.Wrapf(err, "failed to get the field %q from the conversion source object", f.sourceField)
	}
	return errors.Wrapf(target.SetValue(f.targetField, v), "failed to set the field %q of the conversion target object", f.targetField)
}

func NewObjectMetaConversion() Conversion {
	return &fieldCopy{
		baseConversion: newBaseConversion(AllVersions, AllVersions),
		sourceField:    pathObjectMeta,
		targetField:    pathObjectMeta,
	}
}

func NewFieldRenameConversion(sourceVersion, sourceField, targetVersion, targetField string) Conversion {
	return &fieldCopy{
		baseConversion: newBaseConversion(sourceVersion, targetVersion),
		sourceField:    sourceField,
		targetField:    targetField,
	}
}
