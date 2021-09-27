package types

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/google/go-cmp/cmp"
	twtypes "github.com/muvaf/typewriter/pkg/types"

	"github.com/crossplane-contrib/terrajet/pkg/config"
)

func TestBuilder_getReferenceFields(t *testing.T) {
	tp := types.NewPackage("github.com/crossplane-contrib/terrajet/pkg/types", "tjtypes")

	type args struct {
		t *types.TypeName
		f *types.Var
		r config.Reference
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
				f: types.NewField(token.NoPos, tp, "TestField", types.Universe.Lookup("string").Type(), false),
				r: config.Reference{
					Type: "testObject",
				},
			}, want: want{
				outFields: []*types.Var{
					types.NewField(token.NoPos, tp, "TestFieldRef", types.NewPointer(typeXPRef), false),
					types.NewField(token.NoPos, tp, "TestFieldSelector", types.NewPointer(typeXPSelector), false),
				},
				outTags: []string{
					`json:"testFieldRef,omitempty" tf:"-"`,
					`json:"testFieldSelector,omitempty" tf:"-"`,
				},
				outComments: twtypes.Comments{
					"github.com/crossplane-contrib/terrajet/pkg/types.Params:TestFieldRef":      "// +kubebuilder:validation:Optional\n",
					"github.com/crossplane-contrib/terrajet/pkg/types.Params:TestFieldSelector": "// +kubebuilder:validation:Optional\n",
				},
			},
		},
		"OnlyRefTypeSlice": {
			args: args{
				t: types.NewTypeName(token.NoPos, tp, "Params", types.Universe.Lookup("string").Type()),
				f: types.NewField(token.NoPos, tp, "TestField", types.NewSlice(types.Universe.Lookup("string").Type()), false),
				r: config.Reference{
					Type: "testObject",
				},
			}, want: want{
				outFields: []*types.Var{
					types.NewField(token.NoPos, tp, "TestFieldRefs", types.NewSlice(typeXPRef), false),
					types.NewField(token.NoPos, tp, "TestFieldSelector", types.NewPointer(typeXPSelector), false),
				},
				outTags: []string{
					`json:"testFieldRefs,omitempty" tf:"-"`,
					`json:"testFieldSelector,omitempty" tf:"-"`,
				},
				outComments: twtypes.Comments{
					"github.com/crossplane-contrib/terrajet/pkg/types.Params:TestFieldRefs":     "// +kubebuilder:validation:Optional\n",
					"github.com/crossplane-contrib/terrajet/pkg/types.Params:TestFieldSelector": "// +kubebuilder:validation:Optional\n",
				},
			},
		},
		"WithCustomFieldName": {
			args: args{
				t: types.NewTypeName(token.NoPos, tp, "Params", types.Universe.Lookup("string").Type()),
				f: types.NewField(token.NoPos, tp, "TestField", types.Universe.Lookup("string").Type(), false),
				r: config.Reference{
					Type:         "TestObject",
					RefFieldName: "CustomRef",
				},
			}, want: want{
				outFields: []*types.Var{
					types.NewField(token.NoPos, tp, "CustomRef", types.NewPointer(typeXPRef), false),
					types.NewField(token.NoPos, tp, "TestFieldSelector", types.NewPointer(typeXPSelector), false),
				},
				outTags: []string{
					`json:"customRef,omitempty" tf:"-"`,
					`json:"testFieldSelector,omitempty" tf:"-"`,
				},
				outComments: twtypes.Comments{
					"github.com/crossplane-contrib/terrajet/pkg/types.Params:CustomRef":         "// +kubebuilder:validation:Optional\n",
					"github.com/crossplane-contrib/terrajet/pkg/types.Params:TestFieldSelector": "// +kubebuilder:validation:Optional\n",
				},
			},
		},
		"WithCustomSelectorName": {
			args: args{
				t: types.NewTypeName(token.NoPos, tp, "Params", types.Universe.Lookup("string").Type()),
				f: types.NewField(token.NoPos, tp, "TestField", types.Universe.Lookup("string").Type(), false),
				r: config.Reference{
					Type:              "TestObject",
					SelectorFieldName: "CustomSelector",
				},
			}, want: want{
				outFields: []*types.Var{
					types.NewField(token.NoPos, tp, "TestFieldRef", types.NewPointer(typeXPRef), false),
					types.NewField(token.NoPos, tp, "CustomSelector", types.NewPointer(typeXPSelector), false),
				},
				outTags: []string{
					`json:"testFieldRef,omitempty" tf:"-"`,
					`json:"customSelector,omitempty" tf:"-"`,
				},
				outComments: twtypes.Comments{
					"github.com/crossplane-contrib/terrajet/pkg/types.Params:TestFieldRef":   "// +kubebuilder:validation:Optional\n",
					"github.com/crossplane-contrib/terrajet/pkg/types.Params:CustomSelector": "// +kubebuilder:validation:Optional\n",
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			g := &Builder{
				comments: twtypes.Comments{},
			}
			gotFields, gotTags := g.getReferenceFields(tc.args.t, tc.args.f, tc.args.r)
			if diff := cmp.Diff(tc.want.outFields, gotFields, cmp.Comparer(func(a, b *types.Var) bool {
				return a.String() == b.String()
			})); diff != "" {
				t.Errorf("getReferenceFields() fields = %v, want %v", gotFields, tc.want.outFields)
			}
			if diff := cmp.Diff(tc.want.outTags, gotTags); diff != "" {
				t.Errorf("getReferenceFields() tags = %v, want %v", gotTags, tc.want.outTags)
			}
			if diff := cmp.Diff(tc.want.outComments, g.comments); diff != "" {
				t.Errorf("getReferenceFields() comments = %v, want %v", g.comments, tc.want.outComments)
			}
		})
	}
}
