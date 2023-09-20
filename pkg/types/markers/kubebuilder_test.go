// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package markers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestKubebuilderOptions_String(t *testing.T) {
	required := true
	optional := false
	min := 1
	max := 3

	type args struct {
		required *bool
		minimum  *int
		maximum  *int
	}
	type want struct {
		out string
	}
	cases := map[string]struct {
		args
		want
	}{
		"NoOption": {
			args: args{},
			want: want{
				out: "",
			},
		},
		"OnlyRequired": {
			args: args{
				required: &required,
			},
			want: want{
				out: "+kubebuilder:validation:Required\n",
			},
		},
		"OptionalWithMinMax": {
			args: args{
				required: &optional,
				minimum:  &min,
				maximum:  &max,
			},
			want: want{
				out: `+kubebuilder:validation:Optional
+kubebuilder:validation:Minimum=1
+kubebuilder:validation:Maximum=3
`,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o := KubebuilderOptions{
				Required: tc.required,
				Minimum:  tc.minimum,
				Maximum:  tc.maximum,
			}
			got := o.String()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("KubebuilderOptions.String(): -want result, +got result: %s", diff)
			}
		})
	}
}
