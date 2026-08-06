// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"go/token"
	"go/types"

	"github.com/crossplane/upjet/pkg/types/internal"
)

const (
	packageName = "types"
	packagePath = "github.com/crossplane/upjet/pkg/" + packageName
)

// StringOrBool is stored canonically as a string, but can also decode
// from bools.
type StringOrBool string

func (b *StringOrBool) UnmarshalJSON(data []byte) error {
	g := internal.StringOrPrimitive[bool](*b)
	if err := g.UnmarshalJSON(data); err != nil {
		return err
	}
	*b = StringOrBool(g)
	return nil
}

func (b *StringOrBool) MarshalJSON() ([]byte, error) {
	g := internal.StringOrPrimitive[bool](*b)
	return g.MarshalJSON()
}

// NewStringOrBoolType returns a types.Type representing the StringOrBool type.
// The returned type is a pointer type.
func NewStringOrBoolType() types.Type {
	tn := types.NewTypeName(token.NoPos, types.NewPackage(packagePath, packageName), "StringOrBool", nil)
	t := types.NewNamed(tn, types.Universe.Lookup("string").Type(), nil)
	return types.NewPointer(t)
}
