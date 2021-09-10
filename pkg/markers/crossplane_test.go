package markers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCrossplaneOptions_String(t *testing.T) {
	type args struct {
		referenceForType string
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
				referenceForType: "",
			},
			want: want{
				out: "",
			},
		},
		"WithType": {
			args: args{
				referenceForType: "SecurityGroup",
			},
			want: want{
				out: "+crossplane:generate:reference:type=SecurityGroup\n",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o := CrossplaneOptions{
				ReferenceToType: tc.referenceForType,
			}
			got := o.String()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("CrossplaneOptions.String(): -want result, +got result: %s", diff)
			}
		})
	}
}
