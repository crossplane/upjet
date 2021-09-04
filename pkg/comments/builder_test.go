package comments

import (
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/markers"
)

func TestBuilder_BuildAndOpts(t *testing.T) {
	customArg := "custom"

	type args struct {
		comments []string
	}
	type want struct {
		out  string
		opts *markers.Options
		err  error
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
				out:  `// hello world`,
				opts: &markers.Options{},
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
				opts: &markers.Options{},
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
				opts: &markers.Options{},
			},
		},
		"CannotBuildOptions": {
			args: args{
				comments: []string{
					"hello world",
					"+terrajet:crdschema:Tag:unknownkey=custom",
					"yes, this is a test",
				},
			},
			want: want{
				out: `// hello world
// yes, this is a test
// +terrajet:crdschema:Tag:unknownkey=custom`,
				err: errors.Wrap(errors.Wrapf(errors.New("[unknown argument \"unknownkey\" (at <input>:1:11) extra arguments provided: \"custom\" (at <input>:1:12)]"),
					"cannot parse marker line: %s", "+terrajet:crdschema:Tag:unknownkey=custom"), errCannotParseAsFieldMarker),
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
				opts: &markers.Options{
					CRDTag: markers.CRDTag{
						TF: &customArg,
					},
				},
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
				opts: &markers.Options{
					CRDTag: markers.CRDTag{
						TF: &customArg,
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := &Builder{}

			for _, c := range tc.args.comments {
				b.AddComment(c)
			}
			got := b.Build()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("Build() out = %v, want %v", got, tc.want.out)
			}
			gotOpts, gotErr := b.BuildOptions()
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Errorf("GetOptions() error = %v, wantErr %v", gotErr, tc.want.err)
			}
			if diff := cmp.Diff(tc.want.opts, gotOpts); diff != "" {
				t.Errorf("GetOptions() out = %v, want %v", gotOpts, tc.want.opts)
			}
		})
	}
}
