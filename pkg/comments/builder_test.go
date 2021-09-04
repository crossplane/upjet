package comments

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/crossplane-contrib/terrajet/pkg/markers"
)

func TestBuilder_Build(t *testing.T) {
	customArg := "custom"

	type args struct {
		comments []string
	}
	type want struct {
		out string
	}

	cases := map[string]struct {
		args
		want
	}{
		"OnlySingleComment": {
			args: args{
				comments: []string{
					"hello world",
				},
			},
			want: want{
				out: `// hello world`,
			},
		},
		"MultipleCommentLines": {
			args: args{
				comments: []string{
					"hello world",
					"this is a test",
					"yes, this is a test",
				},
			},
			want: want{
				out: `// hello world
// this is a test
// yes, this is a test`,
			},
		},
		"TrimsSpacesIgnoresEmptyLines": {
			args: args{
				comments: []string{
					"hello world ",
					" ",
					"   this is a test",
					"yes, this is a test",
				},
			},
			want: want{
				out: `// hello world
// this is a test
// yes, this is a test`,
			},
		},
		"MixedMarkersAndComments": {
			args: args{
				comments: []string{
					"hello world",
					markers.Must(markers.MarkerForConfig(markers.CRDTag{TF: &customArg})),
					"yes, this is a test",
				},
			},
			want: want{
				out: fmt.Sprintf(`// hello world
// yes, this is a test
// %s`, markers.Must(markers.MarkerForConfig(markers.CRDTag{TF: &customArg}))),
			},
		},
		"MixedMarkersAndCommentsAlsoWithKubebuilder": {
			args: args{
				comments: []string{
					"hello world",
					markers.Must(markers.MarkerForConfig(markers.CRDTag{TF: &customArg})),
					markers.Must(markers.MarkerForConfig(markers.ValidationRequired{})),
					markers.Must(markers.MarkerForConfig(markers.ValidationMinimum{Minimum: 1})),
					"yes, this is a test",
				},
			},
			want: want{
				out: fmt.Sprintf(`// hello world
// yes, this is a test
// %s
// +kubebuilder:validation:Required
// +kubebuilder:validation:Minimum=1`, markers.Must(markers.MarkerForConfig(markers.CRDTag{TF: &customArg}))),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := &Builder{}

			for _, c := range tc.args.comments {
				if err := b.AddComment(c); err != nil {
					t.Errorf("unexpected error during AddComment: %v", err)
				}
			}
			got := b.Build()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("Build() out = %v, want %v", got, tc.want.out)
			}
		})
	}
}
