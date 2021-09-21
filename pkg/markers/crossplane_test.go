package markers

import (
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

func TestCrossplaneOptions_String(t *testing.T) {
	type args struct {
		referenceToType            string
		referenceExtractor         string
		referenceFieldName         string
		referenceSelectorFieldName string
	}
	type want struct {
		out string
	}
	cases := map[string]struct {
		args
		want
	}{
		"NoOption": {
			args: args{
				referenceToType: "",
			},
			want: want{
				out: "",
			},
		},
		"WithType": {
			args: args{
				referenceToType: "SecurityGroup",
			},
			want: want{
				out: "+crossplane:generate:reference:type=SecurityGroup\n",
			},
		},
		"WithAll": {
			args: args{
				referenceToType:            "github.com/crossplane/provider-aws/apis/ec2/v1beta1.Subnet",
				referenceExtractor:         "github.com/crossplane/provider-aws/apis/ec2/v1beta1.SubnetARN()",
				referenceFieldName:         "SubnetIDRefs",
				referenceSelectorFieldName: "SubnetIDSelector",
			},
			want: want{
				out: `+crossplane:generate:reference:type=github.com/crossplane/provider-aws/apis/ec2/v1beta1.Subnet
+crossplane:generate:reference:extractor=github.com/crossplane/provider-aws/apis/ec2/v1beta1.SubnetARN()
+crossplane:generate:reference:refFieldName=SubnetIDRefs
+crossplane:generate:reference:selectorFieldName=SubnetIDSelector
`,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o := CrossplaneOptions{
				ReferenceToType:            tc.referenceToType,
				ReferenceExtractor:         tc.referenceExtractor,
				ReferenceFieldName:         tc.referenceFieldName,
				ReferenceSelectorFieldName: tc.referenceSelectorFieldName,
			}
			got := o.String()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("CrossplaneOptions.String(): -want result, +got result: %s", diff)
			}
		})
	}
}

func Test_parseAsCrossplaneOption(t *testing.T) {
	refType := "customType"
	refExtractor := "customExtractor"
	refFieldName := "customFieldRefs"
	refFieldSelector := "customFieldSelector"

	type args struct {
		opts *CrossplaneOptions
		line string
	}
	type want struct {
		opts   *CrossplaneOptions
		parsed bool
		err    error
	}
	cases := map[string]struct {
		args
		want
	}{
		"RefTypeOnly": {
			args: args{
				opts: &CrossplaneOptions{},
				line: fmt.Sprintf("%s%s", markerPrefixRefType, refType),
			},
			want: want{
				opts: &CrossplaneOptions{
					ReferenceToType: refType,
				},
				parsed: true,
			},
		},
		"AddExtractor": {
			args: args{
				opts: &CrossplaneOptions{
					ReferenceToType: refType,
				},
				line: fmt.Sprintf("%s%s\n", markerPrefixRefExtractor, refExtractor),
			},
			want: want{
				opts: &CrossplaneOptions{
					ReferenceToType:    refType,
					ReferenceExtractor: refExtractor,
				},
				parsed: true,
			},
		},
		"AddRefName": {
			args: args{
				opts: &CrossplaneOptions{
					ReferenceToType:    refType,
					ReferenceExtractor: refExtractor,
				},
				line: fmt.Sprintf("%s%s\n", markerPrefixRefFieldName, refFieldName),
			},
			want: want{
				opts: &CrossplaneOptions{
					ReferenceToType:    refType,
					ReferenceExtractor: refExtractor,
					ReferenceFieldName: refFieldName,
				},
				parsed: true,
			},
		},
		"AddRefSelector": {
			args: args{
				opts: &CrossplaneOptions{
					ReferenceToType:    refType,
					ReferenceExtractor: refExtractor,
					ReferenceFieldName: refFieldName,
				},
				line: fmt.Sprintf("%s%s\n", markerPrefixRefSelectorName, refFieldSelector),
			},
			want: want{
				opts: &CrossplaneOptions{
					ReferenceToType:            refType,
					ReferenceExtractor:         refExtractor,
					ReferenceFieldName:         refFieldName,
					ReferenceSelectorFieldName: refFieldSelector,
				},
				parsed: true,
			},
		},
		"UnknownMarker": {
			args: args{
				opts: &CrossplaneOptions{},
				line: "+some:other:marker:key=value",
			},
			want: want{
				opts:   &CrossplaneOptions{},
				parsed: false,
				err:    nil,
			},
		},
		"CannotParse": {
			args: args{
				opts: &CrossplaneOptions{},
				line: "+crossplane:unknownmarker:key=value",
			},
			want: want{
				opts:   &CrossplaneOptions{},
				parsed: false,
				err:    errors.Errorf(errFmtCannotParseAsCrossplane, "+crossplane:unknownmarker:key=value"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts := tc.args.opts
			gotParsed, gotErr := ParseAsCrossplaneOption(opts, tc.args.line)

			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("ParseAsCrossplaneOption(...): -want error, +got error: %s", diff)
			}

			if diff := cmp.Diff(tc.want.parsed, gotParsed); diff != "" {
				t.Errorf("ParseAsCrossplaneOption() parsed = %v, wantParsed %v", gotParsed, tc.want.parsed)
			}

			if diff := cmp.Diff(tc.want.opts, opts); diff != "" {
				t.Errorf("ParseAsCrossplaneOption() opts = %v, wantOpts %v", opts, tc.want.opts)
			}
		})
	}
}
