package comments

import (
	"testing"

	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"

	"github.com/crossplane-contrib/terrajet/pkg/markers"
)

func TestComment_Build(t *testing.T) {
	tftag := "-"
	type args struct {
		text string
		opts []Option
	}
	type want struct {
		out   string
		mopts markers.Options
		err   error
	}

	cases := map[string]struct {
		args
		want
	}{
		"OnlyTextNoMarker": {
			args: args{
				text: "hello world!",
			},
			want: want{
				out:   "// hello world!\n",
				mopts: markers.Options{},
			},
		},
		"MultilineTextNoMarker": {
			args: args{
				text: `hello world!
this is a test
yes, this is a test`,
			},
			want: want{
				out: `// hello world!
// this is a test
// yes, this is a test
`,
				mopts: markers.Options{},
			},
		},
		"TextWithTerrajetMarker": {
			args: args{
				text: `hello world!
+terrajet:crdfield:TFTag=-
`,
			},
			want: want{
				out: `// hello world!
// +terrajet:crdfield:TFTag=-
`,
				mopts: markers.Options{
					TerrajetOptions: markers.TerrajetOptions{
						FieldTFTag: &tftag,
					},
				},
			},
		},
		"TextWithOtherMarker": {
			args: args{
				text: `hello world!
+kubebuilder:validation:Required
`,
			},
			want: want{
				out: `// hello world!
// +kubebuilder:validation:Required
`,
				mopts: markers.Options{},
			},
		},
		"CommentWithTerrajetOptions": {
			args: args{
				text: `hello world!`,
				opts: []Option{
					WithTFTag("-"),
				},
			},
			want: want{
				out: `// hello world!
// +terrajet:crdfield:TFTag=-
`,
				mopts: markers.Options{
					TerrajetOptions: markers.TerrajetOptions{
						FieldTFTag: &tftag,
					},
				},
			},
		},
		"CommentWithMixedOptions": {
			args: args{
				text: `hello world!`,
				opts: []Option{
					WithTFTag("-"),
					WithReferenceTo("Subnet"),
				},
			},
			want: want{
				out: `// hello world!
// +terrajet:crdfield:TFTag=-
// +crossplane:generate:reference:type=Subnet
`,
				mopts: markers.Options{
					TerrajetOptions: markers.TerrajetOptions{
						FieldTFTag: &tftag,
					},
					CrossplaneOptions: markers.CrossplaneOptions{
						ReferenceToType: "Subnet",
					},
				},
			},
		},
		"CommentWithUnsupportedTerrajetMarker": {
			args: args{
				text: `hello world!
+terrajet:crdfield:TFTag=-
+terrajet:unsupported:key=value
`,
			},
			want: want{
				err: errors.New("cannot parse as a terrajet prefix: +terrajet:unsupported:key=value"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, gotErr := New(tc.text, tc.opts...)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("comment.New(...): -want error, +got error: %s", diff)
			}
			if gotErr != nil {
				return
			}
			if diff := cmp.Diff(tc.want.mopts, c.Options); diff != "" {
				t.Errorf("comment.New(...) opts = %v, want %v", c.Options, tc.want.mopts)
			}
			got := c.Build()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("Build() out = %v, want %v", got, tc.want.out)
			}
		})
	}
}
