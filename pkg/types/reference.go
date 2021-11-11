package types

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"

	"github.com/pkg/errors"
	"golang.org/x/tools/go/packages"

	"github.com/crossplane-contrib/terrajet/pkg/config"
	"github.com/crossplane-contrib/terrajet/pkg/types/comments"
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

func (g *Builder) generateReferenceFields(t *types.TypeName, f *types.Var, r config.Reference) (fields []*types.Var, tags []string) {
	_, isSlice := f.Type().(*types.Slice)

	rfn := r.RefFieldName
	if rfn == "" {
		rfn = f.Name() + "Ref"
		if isSlice {
			rfn += "s"
		}
	}

	sfn := r.SelectorFieldName
	if sfn == "" {
		sfn = f.Name() + "Selector"
	}

	rn := NewNameFromCamel(rfn)
	sn := NewNameFromCamel(sfn)
	refTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, rn.LowerCamelComputed)
	selTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, sn.LowerCamelComputed)

	var tr types.Type
	tr = types.NewPointer(typeReferenceField)
	if isSlice {
		tr = types.NewSlice(typeReferenceField)
	}
	ref := types.NewField(token.NoPos, g.Package, rfn, tr, false)
	sel := types.NewField(token.NoPos, g.Package, sfn, types.NewPointer(typeSelectorField), false)

	g.Comments.AddFieldComment(t, rfn, commentOptional.Build())
	g.Comments.AddFieldComment(t, sfn, commentOptional.Build())

	return []*types.Var{ref, sel}, []string{refTag, selTag}
}

func init() {
	pkgs, err := packages.Load(&packages.Config{Mode: loadMode}, PackagePathXPCommonAPIs)
	if err != nil {
		panic(errors.Wrap(err, "cannot load crossplane-runtime package to get reference Types"))
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
func TypePath(i interface{}) string {
	return reflect.TypeOf(i).PkgPath() + "." + reflect.TypeOf(i).Name()
}
