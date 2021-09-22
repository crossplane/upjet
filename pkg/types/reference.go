package types

import (
	"fmt"
	"go/token"
	"go/types"

	"github.com/pkg/errors"
	"golang.org/x/tools/go/packages"

	"github.com/crossplane-contrib/terrajet/pkg/comments"
	"github.com/crossplane-contrib/terrajet/pkg/markers"
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

func (g *Builder) getReferenceFields(t *types.TypeName, opts markers.CrossplaneOptions) ([]*types.Var, []string) {
	if opts.ReferenceToType == "" {
		return nil, nil
	}

	rfn := opts.ReferenceFieldName
	if rfn == "" {
		rfn = defaultReferenceFieldName()
	}
	sfn := opts.ReferenceSelectorFieldName
	if sfn == "" {
		sfn = defaultReferenceSelectorName()
	}

	rn := NewNameFromCamel(rfn)
	sn := NewNameFromCamel(sfn)
	refTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, rn.LowerCamel)
	selTag := fmt.Sprintf(`json:"%s,omitempty" tf:"-"`, sn.LowerCamel)

	ref := types.NewField(token.NoPos, g.Package, rfn, typeXPRef, false)
	sel := types.NewField(token.NoPos, g.Package, sfn, types.NewPointer(typeXPSelector), false)

	g.comments.AddFieldComment(t, rfn, commentOptional.Build())
	g.comments.AddFieldComment(t, sfn, commentOptional.Build())

	return []*types.Var{ref, sel}, []string{refTag, selTag}
}

func defaultReferenceFieldName() string {
	return ""
}

func defaultReferenceSelectorName() string {
	return ""
}

func init() {
	pkgs, err := packages.Load(&packages.Config{Mode: loadMode}, PackagePathXPCommonAPIs)
	if err != nil {
		panic(errors.Wrap(err, "cannot load crossplane-runtime package to get reference types"))
	}
	if len(pkgs) != 1 && pkgs[0].Name != "v1" {
		panic(errors.Wrapf(err, "unexpected package name %s", pkgs[0].Name))
	}
	typeXPRef = pkgs[0].Types.Scope().Lookup("Reference").Type()
	typeXPSelector = pkgs[0].Types.Scope().Lookup("Selector").Type()

	commentOptional, err = comments.New("")
	if err != nil {
		panic(errors.Wrap(err, "cannot build new comment for reference fields"))
	}
	req := false
	commentOptional.KubebuilderOptions.Required = &req
}
