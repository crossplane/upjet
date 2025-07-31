// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"k8s.io/utils/ptr"

	"github.com/crossplane/upjet/pkg/types/comments"
	"github.com/crossplane/upjet/pkg/types/markers"
	"github.com/crossplane/upjet/pkg/types/name"
)

const (
	// PackagePathXPCommonAPIs is the go path for the Crossplane Runtime package
	// with common APIs
	PackagePathXPCommonAPIs = "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	// PackagePathXPV2CommonAPIs is the go path for the Crossplane Runtime package
	// with common v2 APIs
	PackagePathXPV2CommonAPIs = "github.com/crossplane/crossplane-runtime/v2/apis/common/v2"
)

// Types to use from by reference generator.
var (
	typeReferenceField types.Type = types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage(PackagePathXPCommonAPIs, "v1"), "Reference", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	typeSelectorField types.Type = types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage(PackagePathXPCommonAPIs, "v1"), "Selector", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	typeSecretKeySelector types.Type = types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage(PackagePathXPCommonAPIs, "v1"), "SecretKeySelector", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	typeSecretReference types.Type = types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage(PackagePathXPCommonAPIs, "v1"), "SecretReference", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	typeLocalSecretReference types.Type = types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage(PackagePathXPCommonAPIs, "v1"), "LocalSecretReference", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	typeLocalSecretKeySelector types.Type = types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage(PackagePathXPCommonAPIs, "v1"), "LocalSecretKeySelector", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	typeNamespacedReferenceField types.Type = types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage(PackagePathXPCommonAPIs, "v1"), "NamespacedReference", nil),
		types.NewStruct(nil, nil),
		nil,
	)

	typeNamespacedSelectorField types.Type = types.NewNamed(
		types.NewTypeName(token.NoPos, types.NewPackage(PackagePathXPCommonAPIs, "v1"), "NamespacedSelector", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	commentOptional = &comments.Comment{
		Options: markers.Options{
			KubebuilderOptions: markers.KubebuilderOptions{
				Required: ptr.To(false),
			},
		},
	}
)

func (g *Builder) generateReferenceFields(t *types.TypeName, f *Field) (fields []*types.Var, tags []string) {
	_, isSlice := f.FieldType.(*types.Slice)

	rfn := name.ReferenceFieldName(f.Name, isSlice, f.Reference.RefFieldName)
	sfn := name.SelectorFieldName(f.Name, f.Reference.SelectorFieldName)

	refTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, rfn.LowerCamelComputed)
	selTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, sfn.LowerCamelComputed)

	var tr types.Type
	if g.scope == CRDScopeCluster {
		tr = types.NewPointer(typeReferenceField)
	} else {
		tr = types.NewPointer(typeNamespacedReferenceField)
	}
	refComment := fmt.Sprintf("// Reference to a %s to populate %s.\n%s",
		friendlyTypeDescription(f.Reference.Type), f.Name.LowerCamelComputed, commentOptional.Build())
	selComment := fmt.Sprintf("// Selector for a %s to populate %s.\n%s",
		friendlyTypeDescription(f.Reference.Type), f.Name.LowerCamelComputed, commentOptional.Build())
	if isSlice {
		tr = types.NewSlice(typeNamespacedReferenceField)
		if g.scope == CRDScopeCluster {
			tr = types.NewSlice(typeReferenceField)
		}
		refComment = fmt.Sprintf("// References to %s to populate %s.\n%s",
			friendlyTypeDescription(f.Reference.Type), f.Name.LowerCamelComputed, commentOptional.Build())
		selComment = fmt.Sprintf("// Selector for a list of %s to populate %s.\n%s",
			friendlyTypeDescription(f.Reference.Type), f.Name.LowerCamelComputed, commentOptional.Build())
	}
	ref := types.NewField(token.NoPos, g.Package, rfn.Camel, tr, false)
	tsel := types.NewPointer(typeNamespacedSelectorField)
	if g.scope == CRDScopeCluster {
		tsel = types.NewPointer(typeSelectorField)
	}
	sel := types.NewField(token.NoPos, g.Package, sfn.Camel, tsel, false)

	g.comments.AddFieldComment(t, rfn.Camel, refComment)
	g.comments.AddFieldComment(t, sfn.Camel, selComment)
	f.TransformedName = rfn.LowerCamelComputed
	f.SelectorName = sfn.LowerCamelComputed

	return []*types.Var{ref, sel}, []string{refTag, selTag}
}

// TypePath returns go package path for the input type. This is a helper
// function to be used whenever this information is needed, like configuring to
// reference to a type. Should not be used if the type is in the same package as
// the caller.
func TypePath(i any) string {
	return reflect.TypeOf(i).PkgPath() + "." + reflect.TypeOf(i).Name()
}

func friendlyTypeDescription(path string) string {
	if !strings.Contains(path, ".") {
		return path
	}
	typeName := path[strings.LastIndex(path, ".")+1:]
	dirs := strings.Split(path, "/")
	groupName := dirs[len(dirs)-2]
	return fmt.Sprintf("%s in %s", typeName, groupName)
}
