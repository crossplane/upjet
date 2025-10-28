// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package comments

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/types/markers"
	"github.com/crossplane/upjet/v2/pkg/types/structtag"
)

func TestCommentBuild(t *testing.T) {
	tftag := structtag.NewTF(structtag.WithOmit(structtag.OmitAlways))
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
		"TextWithUpjetMarker": {
			args: args{
				text: `hello world!
+upjet:crd:field:TFTag=-
`,
			},
			want: want{
				out: `// hello world!
// +upjet:crd:field:TFTag=-
`,
				mopts: markers.Options{
					UpjetOptions: markers.UpjetOptions{
						FieldTFTag: tftag,
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
		"CommentWithUpjetOptions": {
			args: args{
				text: `hello world!`,
				opts: []Option{
					WithTFTag("-"),
				},
			},
			want: want{
				out: `// hello world!
// +upjet:crd:field:TFTag=-
`,
				mopts: markers.Options{
					UpjetOptions: markers.UpjetOptions{
						FieldTFTag: tftag,
					},
				},
			},
		},
		"CommentWithMixedOptions": {
			args: args{
				text: `hello world!`,
				opts: []Option{
					WithTFTag("-"),
					WithReferenceConfig(config.Reference{
						Type: reflect.TypeOf(Comment{}).String(),
					}),
				},
			},
			want: want{
				out: `// hello world!
// +upjet:crd:field:TFTag=-
// +crossplane:generate:reference:type=comments.Comment
`,
				mopts: markers.Options{
					UpjetOptions: markers.UpjetOptions{
						FieldTFTag: tftag,
					},
					CrossplaneOptions: markers.CrossplaneOptions{
						Reference: config.Reference{
							Type: "comments.Comment",
						},
					},
				},
			},
		},
		"CommentWithUnsupportedUpjetMarker": {
			args: args{
				text: `hello world!
+upjet:crd:field:TFTag=-
+upjet:unsupported:key=value
`,
			},
			want: want{
				err: errors.New("cannot parse as an upjet prefix: +upjet:unsupported:key=value"),
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
			if diff := cmp.Diff(tc.want.mopts, c.Options, cmp.Comparer(func(v1, v2 structtag.Value) bool {
				return v1.String() == v2.String()
			})); diff != "" {
				t.Errorf("comment.New(...) opts = %v, want %v", c.Options, tc.want.mopts)
			}
			got := c.Build()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("Build() out = %v, want %v", got, tc.want.out)
			}
		})
	}
}
