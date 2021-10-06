package markers

import (
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

func Test_parseAsTerrajetOption(t *testing.T) {
	customTF := "custom-tf"
	customJSON := "custom-json"

	type args struct {
		opts *TerrajetOptions
		line string
	}
	type want struct {
		opts   *TerrajetOptions
		parsed bool
		err    error
	}
	cases := map[string]struct {
		args
		want
	}{
		"CRDTagTFOnly": {
			args: args{
				opts: &TerrajetOptions{},
				line: fmt.Sprintf("%s%s", markerPrefixCRDTFTag, customTF),
			},
			want: want{
				opts: &TerrajetOptions{
					FieldTFTag: &customTF,
				},
				parsed: true,
			},
		},
		"CRDBothTags": {
			args: args{
				opts: &TerrajetOptions{
					FieldTFTag: &customTF,
				},
				line: fmt.Sprintf("%s%s\n", markerPrefixCRDJSONTag, customJSON),
			},
			want: want{
				opts: &TerrajetOptions{
					FieldTFTag:   &customTF,
					FieldJSONTag: &customJSON,
				},
				parsed: true,
			},
		},
		"UnknownMarker": {
			args: args{
				opts: &TerrajetOptions{},
				line: "+some:other:marker:key=value",
			},
			want: want{
				opts:   &TerrajetOptions{},
				parsed: false,
				err:    nil,
			},
		},
		"CannotParse": {
			args: args{
				opts: &TerrajetOptions{},
				line: "+terrajet:unknownmarker:key=value",
			},
			want: want{
				opts:   &TerrajetOptions{},
				parsed: false,
				err:    errors.Errorf(errFmtCannotParseAsTerrajet, "+terrajet:unknownmarker:key=value"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts := tc.args.opts
			gotParsed, gotErr := ParseAsTerrajetOption(opts, tc.args.line)

			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("ParseAsTerrajetOption(...): -want error, +got error: %s", diff)
			}

			if diff := cmp.Diff(tc.want.parsed, gotParsed); diff != "" {
				t.Errorf("ParseAsTerrajetOption() parsed = %v, wantParsed %v", gotParsed, tc.want.parsed)
			}

			if diff := cmp.Diff(tc.want.opts, opts); diff != "" {
				t.Errorf("ParseAsTerrajetOption() opts = %v, wantOpts %v", opts, tc.want.opts)
			}
		})
	}
}
