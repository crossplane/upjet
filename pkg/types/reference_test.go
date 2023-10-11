// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/types/name"
	"github.com/google/go-cmp/cmp"
	twtypes "github.com/muvaf/typewriter/pkg/types"
)

func TestBuilder_generateReferenceFields(t *testing.T) {
	tp := types.NewPackage("github.com/crossplane/upjet/pkg/types", "tjtypes")

	type args struct {
		t *types.TypeName
		f *Field
	}
	type want struct {
		outFields   []*types.Var
		outTags     []string
		outComments twtypes.Comments
	}
	cases := map[string]struct {
		args
		want
	}{
		"OnlyRefType": {
			args: args{
				t: types.NewTypeName(token.NoPos, tp, "Params", types.Universe.Lookup("string").Type()),
				f: &Field{
					Name: name.NewFromCamel("TestField"),
					Reference: &config.Reference{
						Type: "testObject",
					},
					FieldType: types.Universe.Lookup("string").Type(),
				},
			}, want: want{
				outFields: []*types.Var{
					types.NewField(token.NoPos, tp, "TestFieldRef", types.NewPointer(typeReferenceField), false),
					types.NewField(token.NoPos, tp, "TestFieldSelector", types.NewPointer(typeSelectorField), false),
				},
				outTags: []string{
					`json:"testFieldRef,omitempty" tf:"-"`,
					`json:"testFieldSelector,omitempty" tf:"-"`,
				},
				outComments: twtypes.Comments{
					"github.com/crossplane/upjet/pkg/types.Params:TestFieldRef":      "// Reference to a testObject to populate testField.\n// +kubebuilder:validation:Optional\n",
					"github.com/crossplane/upjet/pkg/types.Params:TestFieldSelector": "// Selector for a testObject to populate testField.\n// +kubebuilder:validation:Optional\n",
				},
			},
		},
		"OnlyRefTypeSlice": {
			args: args{
				t: types.NewTypeName(token.NoPos, tp, "Params", types.Universe.Lookup("string").Type()),
				f: &Field{
					Name: name.NewFromCamel("TestField"),
					Reference: &config.Reference{
						Type: "testObject",
					},
					FieldType: types.NewSlice(types.Universe.Lookup("string").Type()),
				},
			}, want: want{
				outFields: []*types.Var{
					types.NewField(token.NoPos, tp, "TestFieldRefs", types.NewSlice(typeReferenceField), false),
					types.NewField(token.NoPos, tp, "TestFieldSelector", types.NewPointer(typeSelectorField), false),
				},
				outTags: []string{
					`json:"testFieldRefs,omitempty" tf:"-"`,
					`json:"testFieldSelector,omitempty" tf:"-"`,
				},
				outComments: twtypes.Comments{
					"github.com/crossplane/upjet/pkg/types.Params:TestFieldRefs":     "// References to testObject to populate testField.\n// +kubebuilder:validation:Optional\n",
					"github.com/crossplane/upjet/pkg/types.Params:TestFieldSelector": "// Selector for a list of testObject to populate testField.\n// +kubebuilder:validation:Optional\n",
				},
			},
		},
		"WithCustomFieldName": {
			args: args{
				t: types.NewTypeName(token.NoPos, tp, "Params", types.Universe.Lookup("string").Type()),
				f: &Field{
					Name: name.NewFromCamel("TestField"),
					Reference: &config.Reference{
						Type:         "TestObject",
						RefFieldName: "CustomRef",
					},
					FieldType: types.Universe.Lookup("string").Type(),
				},
			}, want: want{
				outFields: []*types.Var{
					types.NewField(token.NoPos, tp, "CustomRef", types.NewPointer(typeReferenceField), false),
					types.NewField(token.NoPos, tp, "TestFieldSelector", types.NewPointer(typeSelectorField), false),
				},
				outTags: []string{
					`json:"customRef,omitempty" tf:"-"`,
					`json:"testFieldSelector,omitempty" tf:"-"`,
				},
				outComments: twtypes.Comments{
					"github.com/crossplane/upjet/pkg/types.Params:CustomRef":         "// Reference to a TestObject to populate testField.\n// +kubebuilder:validation:Optional\n",
					"github.com/crossplane/upjet/pkg/types.Params:TestFieldSelector": "// Selector for a TestObject to populate testField.\n// +kubebuilder:validation:Optional\n",
				},
			},
		},
		"WithCustomSelectorName": {
			args: args{
				t: types.NewTypeName(token.NoPos, tp, "Params", types.Universe.Lookup("string").Type()),
				f: &Field{
					Name: name.NewFromCamel("TestField"),
					Reference: &config.Reference{
						Type:              "TestObject",
						SelectorFieldName: "CustomSelector",
					},
					FieldType: types.Universe.Lookup("string").Type(),
				},
			}, want: want{
				outFields: []*types.Var{
					types.NewField(token.NoPos, tp, "TestFieldRef", types.NewPointer(typeReferenceField), false),
					types.NewField(token.NoPos, tp, "CustomSelector", types.NewPointer(typeSelectorField), false),
				},
				outTags: []string{
					`json:"testFieldRef,omitempty" tf:"-"`,
					`json:"customSelector,omitempty" tf:"-"`,
				},
				outComments: twtypes.Comments{
					"github.com/crossplane/upjet/pkg/types.Params:TestFieldRef":   "// Reference to a TestObject to populate testField.\n// +kubebuilder:validation:Optional\n",
					"github.com/crossplane/upjet/pkg/types.Params:CustomSelector": "// Selector for a TestObject to populate testField.\n// +kubebuilder:validation:Optional\n",
				},
			},
		},
		"ReferenceToAnotherPackage": {
			args: args{
				t: types.NewTypeName(token.NoPos, tp, "Params", types.Universe.Lookup("string").Type()),
				f: &Field{
					Name: name.NewFromCamel("TestField"),
					Reference: &config.Reference{
						Type: "github.com/upbound/official-providers/provider-aws/apis/somepackage/v1beta1.TestObject",
					},
					FieldType: types.Universe.Lookup("string").Type(),
				},
			}, want: want{
				outFields: []*types.Var{
					types.NewField(token.NoPos, tp, "TestFieldRef", types.NewPointer(typeReferenceField), false),
					types.NewField(token.NoPos, tp, "TestFieldSelector", types.NewPointer(typeSelectorField), false),
				},
				outTags: []string{
					`json:"testFieldRef,omitempty" tf:"-"`,
					`json:"testFieldSelector,omitempty" tf:"-"`,
				},
				outComments: twtypes.Comments{
					"github.com/crossplane/upjet/pkg/types.Params:TestFieldRef":      "// Reference to a TestObject in somepackage to populate testField.\n// +kubebuilder:validation:Optional\n",
					"github.com/crossplane/upjet/pkg/types.Params:TestFieldSelector": "// Selector for a TestObject in somepackage to populate testField.\n// +kubebuilder:validation:Optional\n",
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := &Builder{
				comments: twtypes.Comments{},
			}
			gotFields, gotTags := g.generateReferenceFields(tc.args.t, tc.args.f)
			if diff := cmp.Diff(tc.want.outFields, gotFields, cmp.Comparer(func(a, b *types.Var) bool {
				return a.String() == b.String()
			})); diff != "" {
				t.Errorf("generateReferenceFields(): fields: +got, -want: %s", diff)
			}
			if diff := cmp.Diff(tc.want.outTags, gotTags); diff != "" {
				t.Errorf("generateReferenceFields(): tags: +got, -want: %s", diff)
			}
			if diff := cmp.Diff(tc.want.outComments, g.comments); diff != "" {
				t.Errorf("generateReferenceFields(): comments: +got, -want: %s", diff)
			}
		})
	}
}
