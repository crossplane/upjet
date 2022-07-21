package types

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"

	"github.com/pkg/errors"
	"golang.org/x/tools/go/packages"

	"github.com/upbound/upjet/pkg/types/comments"
	"github.com/upbound/upjet/pkg/types/name"
)

const (
	// PackagePathXPCommonAPIs is the go path for the Crossplane Runtime package
	// with common APIs
	PackagePathXPCommonAPIs = "github.com/crossplane/crossplane-runtime/apis/common/v1"

	loadMode = packages.NeedName | packages.NeedImports | packages.NeedTypes
)

var typeReferenceField types.Type
var typeSelectorField types.Type
var typeSecretKeySelector types.Type
var commentOptional *comments.Comment

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

	return []*types.Var{ref, sel}, []string{refTag, selTag}
}

func init() {
	pkgs, err := packages.Load(&packages.Config{Mode: loadMode}, PackagePathXPCommonAPIs)
	if err != nil {
		panic(errors.Wrap(err, "cannot load crossplane-runtime package to get reference types"))
	}
	if len(pkgs) != 1 && pkgs[0].Name != "v1" {
		panic(errors.Errorf("unexpected package name %s", pkgs[0].Name))
	}
	typeReferenceField = pkgs[0].Types.Scope().Lookup("Reference").Type()
	typeSelectorField = pkgs[0].Types.Scope().Lookup("Selector").Type()
	typeSecretKeySelector = pkgs[0].Types.Scope().Lookup("SecretKeySelector").Type()

	commentOptional, err = comments.New("")
	if err != nil {
		panic(errors.Errorf("cannot build new comment for reference fields"))
	}
	req := false
	commentOptional.KubebuilderOptions.Required = &req
}

// TypePath returns go package path for the input type. This is a helper
// function to be used whenever this information is needed, like configuring to
// reference to a type. Should not be used if the type is in the same package as
// the caller.
func TypePath(i any) string {
	return reflect.TypeOf(i).PkgPath() + "." + reflect.TypeOf(i).Name()
}
