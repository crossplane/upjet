// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/crossplane/upjet/v2/pkg/config"
)

func TestTerraformedGeneratorGenerate(t *testing.T) {
	type args struct {
		kind                string
		observationTypeName string
	}
	type want struct {
		setObservation []string
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"ObservationTypeNamedAfterKind": {
			reason: "SetObservation should decode the observation into a zero value of the observation type.",
			args: args{
				kind:                "Collection",
				observationTypeName: "CollectionObservation",
			},
			want: want{
				setObservation: []string{
					"p, err := json.TFParser.Marshal(obs)",
					"if err != nil {\n\treturn err\n}",
					"observation := CollectionObservation{}",
					"if err := json.TFParser.Unmarshal(p, &observation); err != nil {\n\treturn err\n}",
					"tr.Status.AtProvider = observation",
					"return nil",
				},
			},
		},
		"SuffixedObservationTypeName": {
			reason: "SetObservation should use the generated observation type name, which is suffixed when it collides with another type.",
			args: args{
				kind:                "Routine",
				observationTypeName: "RoutineObservation_2",
			},
			want: want{
				setObservation: []string{
					"p, err := json.TFParser.Marshal(obs)",
					"if err != nil {\n\treturn err\n}",
					"observation := RoutineObservation_2{}",
					"if err := json.TFParser.Unmarshal(p, &observation); err != nil {\n\treturn err\n}",
					"tr.Status.AtProvider = observation",
					"return nil",
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := renderSetObservation(t, tc.args.kind, tc.args.observationTypeName)
			if diff := cmp.Diff(tc.want.setObservation, got); diff != "" {
				t.Errorf("\n%s\nGenerate(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

// renderSetObservation renders the default Terraformed template and returns
// the statements of the generated SetObservation method.
func renderSetObservation(t *testing.T, kind, observationTypeName string) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "boilerplate.go.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewTerraformedGenerator(types.NewPackage("example.org/apis/test/v1alpha1", "v1alpha1"), dir, dir, "test.example.org", "v1alpha1")
	in := &terraformedInput{
		Resource: &config.Resource{
			Name:              "test_resource",
			Kind:              kind,
			TerraformResource: &schema.Resource{},
		},
		ParametersTypeName:  kind + "Parameters",
		ObservationTypeName: observationTypeName,
	}
	if err := g.Generate([]*terraformedInput{in}, "v1alpha1"); err != nil {
		t.Fatalf("Generate(...): %v", err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(g.LocalDirectoryPath, "zz_"+strings.ToLower(kind)+"_terraformed.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "SetObservation" {
			continue
		}
		stmts := make([]string, 0, len(fn.Body.List))
		for _, s := range fn.Body.List {
			var b bytes.Buffer
			if err := printer.Fprint(&b, fset, s); err != nil {
				t.Fatal(err)
			}
			stmts = append(stmts, b.String())
		}
		return stmts
	}
	t.Fatal("the generated file has no SetObservation method")
	return nil
}
