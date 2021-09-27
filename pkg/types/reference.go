package types

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"

	"github.com/pkg/errors"
	"golang.org/x/tools/go/packages"

	"github.com/crossplane-contrib/terrajet/pkg/comments"
	"github.com/crossplane-contrib/terrajet/pkg/config"
)

const (
	// PackagePathXPCommonAPIs is the go path for the Crossplane Runtime package
	// with common APIs
	PackagePathXPCommonAPIs = "github.com/crossplane/crossplane-runtime/apis/common/v1"

	loadMode = packages.NeedName | packages.NeedImports | packages.NeedTypes
)

var typeXPRef types.Type
var typeXPSelector types.Type
var commentOptional *comments.Comment

func (g *Builder) getReferenceFields(t *types.TypeName, f *types.Var, r config.Reference) ([]*types.Var, []string) {
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
	refTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, rn.LowerCamel)
	selTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, sn.LowerCamel)

	var tr types.Type
	tr = types.NewPointer(typeXPRef)
	if isSlice {
		tr = types.NewSlice(typeXPRef)
	}
	ref := types.NewField(token.NoPos, g.Package, rfn, tr, false)
	sel := types.NewField(token.NoPos, g.Package, sfn, types.NewPointer(typeXPSelector), false)

	g.comments.AddFieldComment(t, rfn, commentOptional.Build())
	g.comments.AddFieldComment(t, sfn, commentOptional.Build())

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
	typeXPRef = pkgs[0].Types.Scope().Lookup("Reference").Type()
	typeXPSelector = pkgs[0].Types.Scope().Lookup("Selector").Type()

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
