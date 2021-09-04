package markers

import (
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

func Test_parseIfMarkerForField(t *testing.T) {
	customTF := "custom-tf"
	customJSON := "custom-json"

	type args struct {
		cfg  *Options
		line string
	}
	type want struct {
		cfg *Options
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"CannotParseTagMarker": {
			args: args{
				cfg:  &Options{},
				line: fmt.Sprintf("%s%s:unknownkey=custom", Prefix, markerPrefixCRDTag),
			},
			want: want{
				cfg: &Options{},
				err: errors.Wrapf(errors.New("[unknown argument \"unknownkey\" (at <input>:1:11) extra arguments provided: \"custom\" (at <input>:1:12)]"),
					errFmtCannotParseMarkerLine, fmt.Sprintf("%s%s:unknownkey=custom", Prefix, markerPrefixCRDTag)),
			},
		},
		"CRDTagTFOnly": {
			args: args{
				cfg:  &Options{},
				line: fmt.Sprintf("%s%s:tf=%s", Prefix, markerPrefixCRDTag, customTF),
			},
			want: want{
				cfg: &Options{
					CRDTag: CRDTag{
						TF: &customTF,
					},
				},
			},
		},
		"CRDTagJsonOnly": {
			args: args{
				cfg:  &Options{},
				line: fmt.Sprintf("%s%s:json=%s", Prefix, markerPrefixCRDTag, customJSON),
			},
			want: want{
				cfg: &Options{
					CRDTag: CRDTag{
						JSON: &customJSON,
					},
				},
			},
		},
		"CRDTagBoth": {
			args: args{
				cfg:  &Options{},
				line: fmt.Sprintf("%s%s:tf=%s,json=%s", Prefix, markerPrefixCRDTag, customTF, customJSON),
			},
			want: want{
				cfg: &Options{
					CRDTag: CRDTag{
						TF:   &customTF,
						JSON: &customJSON,
					},
				},
			},
		},
		"UnknownMarker": {
			args: args{
				cfg:  &Options{},
				line: "+some:other:marker:key=value",
			},
			want: want{
				cfg: &Options{},
				err: nil,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := tc.args.cfg
			gotErr := ParseIfMarkerForField(cfg, tc.args.line)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Errorf("ParseIfMarkerForField() error = %v, wantErr %v", gotErr, tc.want.err)
			}
			if diff := cmp.Diff(tc.want.cfg, cfg); diff != "" {
				t.Errorf("ParseIfMarkerForField() cfg = %v, wantCfg %v", cfg, tc.want.cfg)
			}
		})
	}
}
