package types

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"

	"k8s.io/utils/pointer"

	"github.com/upbound/upjet/pkg/types/comments"
	"github.com/upbound/upjet/pkg/types/markers"
	"github.com/upbound/upjet/pkg/types/name"
)

const (
	// PackagePathXPCommonAPIs is the go path for the Crossplane Runtime package
	// with common APIs
	PackagePathXPCommonAPIs = "github.com/crossplane/crossplane-runtime/apis/common/v1"
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
	commentOptional = &comments.Comment{
		Options: markers.Options{
			KubebuilderOptions: markers.KubebuilderOptions{
				Required: pointer.Bool(false),
			},
		},
	}
)

func (g *Builder) generateReferenceFields(t *types.TypeName, f *Field) (fields []*types.Var, tags []string) {
	_, isSlice := f.FieldType.(*types.Slice)

	// We try to rely on snake for name calculations because it is more accurate
	// in cases where two words are all acronyms and full capital, i.e. APIID
	// would be converted to apiid when you convert it to lower camel computed
	// but if you start with api_id, then it becomes apiId as lower camel computed
	// and APIID as camel, which is what we want.
	rfn := name.NewFromCamel(f.Reference.RefFieldName)
	if f.Reference.RefFieldName == "" {
		temp := f.Name.Snake + "_ref"
		if isSlice {
			temp += "s"
		}
		rfn = name.NewFromSnake(temp)
	}

	sfn := name.NewFromCamel(f.Reference.SelectorFieldName)
	if f.Reference.SelectorFieldName == "" {
		sfn = name.NewFromSnake(f.Name.Snake + "_selector")
	}

	refTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, rfn.LowerCamelComputed)
	selTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, sfn.LowerCamelComputed)

	var tr types.Type
	tr = types.NewPointer(typeReferenceField)
	if isSlice {
		tr = types.NewSlice(typeReferenceField)
	}
	ref := types.NewField(token.NoPos, g.Package, rfn.Camel, tr, false)
	sel := types.NewField(token.NoPos, g.Package, sfn.Camel, types.NewPointer(typeSelectorField), false)

	g.comments.AddFieldComment(t, rfn.Camel, commentOptional.Build())
	g.comments.AddFieldComment(t, sfn.Camel, commentOptional.Build())
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
