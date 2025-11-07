// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package kubebuilder

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestOptionsString(t *testing.T) {
	required := true
	optional := false
	minVal := 1
	maxVal := 3
	default10 := `"10"`

	type args struct {
		required   *bool
		minimum    *int
		maximum    *int
		defaultVal *string
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
				minimum:  &minVal,
				maximum:  &maxVal,
			},
			want: want{
				out: `+kubebuilder:validation:Optional
+kubebuilder:validation:Minimum=1
+kubebuilder:validation:Maximum=3
`,
			},
		},
		"OptionalWithDefault": {
			args: args{
				required:   &optional,
				defaultVal: &default10,
			},
			want: want{
				out: `+kubebuilder:validation:Optional
+kubebuilder:default:="10"
`,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o := Options{
				Required: tc.required,
				Minimum:  tc.minimum,
				Maximum:  tc.maximum,
				Default:  tc.defaultVal,
			}
			got := o.String()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("KubebuilderOptions.String(): -want result, +got result: %s", diff)
			}
		})
	}
}

func TestOptionsOverrideFrom(t *testing.T) {
	trueVal := true
	falseVal := false
	min1 := 1
	min5 := 5
	max10 := 10
	max20 := 20
	default1 := "default1"
	default2 := "default2"

	type args struct {
		o   *Options
		opt *Options
	}
	type want struct {
		result Options
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NilOverride": {
			reason: "OverrideFrom should return a deep copy when override is nil.",
			args: args{
				o: &Options{
					Required: &trueVal,
					Minimum:  &min1,
					Maximum:  &max10,
					Default:  &default1,
				},
				opt: nil,
			},
			want: want{
				result: Options{
					Required: &trueVal,
					Minimum:  &min1,
					Maximum:  &max10,
					Default:  &default1,
				},
			},
		},
		"EmptyOverride": {
			reason: "OverrideFrom with empty override should return a copy of original.",
			args: args{
				o: &Options{
					Required: &trueVal,
					Minimum:  &min1,
				},
				opt: &Options{},
			},
			want: want{
				result: Options{
					Required: &trueVal,
					Minimum:  &min1,
				},
			},
		},
		"EmptyBaseAllOverride": {
			reason: "OverrideFrom should set all fields when base is empty and override has all fields.",
			args: args{
				o: &Options{},
				opt: &Options{
					Required: &falseVal,
					Minimum:  &min5,
					Maximum:  &max20,
					Default:  &default2,
				},
			},
			want: want{
				result: Options{
					Required: &falseVal,
					Minimum:  &min5,
					Maximum:  &max20,
					Default:  &default2,
				},
			},
		},
		"OverrideRequiredOnly": {
			reason: "OverrideFrom should override only the Required field when only that is set in override.",
			args: args{
				o: &Options{
					Required: &trueVal,
					Minimum:  &min1,
					Maximum:  &max10,
				},
				opt: &Options{
					Required: &falseVal,
				},
			},
			want: want{
				result: Options{
					Required: &falseVal,
					Minimum:  &min1,
					Maximum:  &max10,
				},
			},
		},
		"OverrideMinimumOnly": {
			reason: "OverrideFrom should override only the Minimum field when only that is set in override.",
			args: args{
				o: &Options{
					Required: &trueVal,
					Minimum:  &min1,
					Default:  &default1,
				},
				opt: &Options{
					Minimum: &min5,
				},
			},
			want: want{
				result: Options{
					Required: &trueVal,
					Minimum:  &min5,
					Default:  &default1,
				},
			},
		},
		"OverrideMaximumOnly": {
			reason: "OverrideFrom should override only the Maximum field when only that is set in override.",
			args: args{
				o: &Options{
					Required: &trueVal,
					Maximum:  &max10,
				},
				opt: &Options{
					Maximum: &max20,
				},
			},
			want: want{
				result: Options{
					Required: &trueVal,
					Maximum:  &max20,
				},
			},
		},
		"OverrideDefaultOnly": {
			reason: "OverrideFrom should override only the Default field when only that is set in override.",
			args: args{
				o: &Options{
					Minimum: &min1,
					Default: &default1,
				},
				opt: &Options{
					Default: &default2,
				},
			},
			want: want{
				result: Options{
					Minimum: &min1,
					Default: &default2,
				},
			},
		},
		"OverrideAllFields": {
			reason: "OverrideFrom should override all fields when all are set in override.",
			args: args{
				o: &Options{
					Required: &trueVal,
					Minimum:  &min1,
					Maximum:  &max10,
					Default:  &default1,
				},
				opt: &Options{
					Required: &falseVal,
					Minimum:  &min5,
					Maximum:  &max20,
					Default:  &default2,
				},
			},
			want: want{
				result: Options{
					Required: &falseVal,
					Minimum:  &min5,
					Maximum:  &max20,
					Default:  &default2,
				},
			},
		},
		"OverridePartialFields": {
			reason: "OverrideFrom should override only the fields set in override, preserving others.",
			args: args{
				o: &Options{
					Required: &trueVal,
					Minimum:  &min1,
					Maximum:  &max10,
					Default:  &default1,
				},
				opt: &Options{
					Minimum: &min5,
					Default: &default2,
				},
			},
			want: want{
				result: Options{
					Required: &trueVal,
					Minimum:  &min5,
					Maximum:  &max10,
					Default:  &default2,
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.o.OverrideFrom(tc.opt)

			if diff := cmp.Diff(tc.want.result, got, cmp.Comparer(compareIntPtrs), cmp.Comparer(compareBoolPtrs), cmp.Comparer(compareStringPtrs)); diff != "" {
				t.Errorf("\n%s\nOverrideFrom(...): -want, +got:\n%s", tc.reason, diff)
			}

			// Verify deep copy: ensure returned pointers are different from original
			if tc.o.Required != nil && got.Required != nil {
				if tc.o.Required == got.Required {
					t.Errorf("OverrideFrom should return deep copy: Required pointer is shared")
				}
			}
			if tc.o.Minimum != nil && got.Minimum != nil && tc.opt != nil && tc.opt.Minimum == nil {
				if tc.o.Minimum == got.Minimum {
					t.Errorf("OverrideFrom should return deep copy: Minimum pointer is shared")
				}
			}
		})
	}
}

// Helper functions to compare pointer values for cmp.Diff
func compareIntPtrs(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func compareBoolPtrs(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func compareStringPtrs(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
