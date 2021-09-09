package markers

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_parseAsTerrajetOption(t *testing.T) {
	customTF := "custom-tf"
	customJSON := "custom-json"

	type args struct {
		opts *TerrajetOptions
		line string
	}
	type want struct {
		opts *TerrajetOptions
		err  error
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
			},
		},
		"CRDBothTags": {
			args: args{
				opts: &TerrajetOptions{
					FieldTFTag: &customTF,
				},
				line: fmt.Sprintf("%s%s\n", markerPrefixCRDJsonTag, customJSON),
			},
			want: want{
				opts: &TerrajetOptions{
					FieldTFTag:   &customTF,
					FieldJsonTag: &customJSON,
				},
			},
		},
		"UnknownMarker": {
			args: args{
				opts: &TerrajetOptions{},
				line: "+some:other:marker:key=value",
			},
			want: want{
				opts: &TerrajetOptions{},
				err:  nil,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts := tc.args.opts
			ParseAsTerrajetOption(opts, tc.args.line)
			if diff := cmp.Diff(tc.want.opts, opts); diff != "" {
				t.Errorf("ParseAsTerrajetOption() opts = %v, wantOpts %v", opts, tc.want.opts)
			}
		})
	}
}
