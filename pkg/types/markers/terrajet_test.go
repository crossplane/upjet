// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package markers

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/test"
)

func Test_parseAsUpjetOption(t *testing.T) {
	customTF := "custom-tf"
	customJSON := "custom-json"

	type args struct {
		opts *UpjetOptions
		line string
	}
	type want struct {
		opts   *UpjetOptions
		parsed bool
		err    error
	}
	cases := map[string]struct {
		args
		want
	}{
		"CRDTagTFOnly": {
			args: args{
				opts: &UpjetOptions{},
				line: fmt.Sprintf("%s%s", markerPrefixCRDTFTag, customTF),
			},
			want: want{
				opts: &UpjetOptions{
					FieldTFTag: &customTF,
				},
				parsed: true,
			},
		},
		"CRDBothTags": {
			args: args{
				opts: &UpjetOptions{
					FieldTFTag: &customTF,
				},
				line: fmt.Sprintf("%s%s\n", markerPrefixCRDJSONTag, customJSON),
			},
			want: want{
				opts: &UpjetOptions{
					FieldTFTag:   &customTF,
					FieldJSONTag: &customJSON,
				},
				parsed: true,
			},
		},
		"UnknownMarker": {
			args: args{
				opts: &UpjetOptions{},
				line: "+some:other:marker:key=value",
			},
			want: want{
				opts:   &UpjetOptions{},
				parsed: false,
				err:    nil,
			},
		},
		"CannotParse": {
			args: args{
				opts: &UpjetOptions{},
				line: "+upjet:unknownmarker:key=value",
			},
			want: want{
				opts:   &UpjetOptions{},
				parsed: false,
				err:    errors.Errorf(errFmtCannotParseAsUpjet, "+upjet:unknownmarker:key=value"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts := tc.args.opts
			gotParsed, gotErr := ParseAsUpjetOption(opts, tc.args.line)

			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("ParseAsUpjetOption(...): -want error, +got error: %s", diff)
			}

			if diff := cmp.Diff(tc.want.parsed, gotParsed); diff != "" {
				t.Errorf("ParseAsUpjetOption() parsed = %v, wantParsed %v", gotParsed, tc.want.parsed)
			}

			if diff := cmp.Diff(tc.want.opts, opts); diff != "" {
				t.Errorf("ParseAsUpjetOption() opts = %v, wantOpts %v", opts, tc.want.opts)
			}
		})
	}
}
