// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package reconciliationpolicy

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func dur(d time.Duration) metav1.Duration {
	return metav1.Duration{Duration: d}
}

func TestEfrlKeyEquality(t *testing.T) {
	type args struct {
		a efrlKey
		b efrlKey
	}
	type want struct {
		equal bool
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"BothZero": {
			reason: "Two zero-valued efrlKeys should be equal.",
			args: args{
				a: efrlKey{},
				b: efrlKey{},
			},
			want: want{equal: true},
		},
		"ExplicitZeroEqualsImplicitZero": {
			reason: "An efrlKey with explicit zero durations should equal a zero-valued efrlKey (no nil/unset distinction for value types).",
			args: args{
				a: efrlKey{maxDelay: dur(0), baseDelay: dur(0)},
				b: efrlKey{},
			},
			want: want{equal: true},
		},
		"SameMaxDelayOnly": {
			reason: "Same maxDelay with both baseDelays zero should be equal.",
			args: args{
				a: efrlKey{maxDelay: dur(time.Minute)},
				b: efrlKey{maxDelay: dur(time.Minute)},
			},
			want: want{equal: true},
		},
		"SameBaseDelayOnly": {
			reason: "Same baseDelay with both maxDelays zero should be equal.",
			args: args{
				a: efrlKey{baseDelay: dur(time.Second)},
				b: efrlKey{baseDelay: dur(time.Second)},
			},
			want: want{equal: true},
		},
		"BothFieldsSame": {
			reason: "Identical maxDelay and baseDelay should be equal.",
			args: args{
				a: efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(time.Second)},
				b: efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(time.Second)},
			},
			want: want{equal: true},
		},
		"SameInstance": {
			reason: "An efrlKey value must equal itself.",
			args: func() args {
				k := efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(time.Second)}
				return args{a: k, b: k}
			}(),
			want: want{equal: true},
		},
		"NegativeDurationsEqual": {
			reason: "Negative durations with identical values should be equal.",
			args: args{
				a: efrlKey{maxDelay: dur(-time.Second)},
				b: efrlKey{maxDelay: dur(-time.Second)},
			},
			want: want{equal: true},
		},
		"DifferentMaxDelay": {
			reason: "Different maxDelay values should not be equal.",
			args: args{
				a: efrlKey{maxDelay: dur(time.Minute)},
				b: efrlKey{maxDelay: dur(2 * time.Minute)},
			},
			want: want{equal: false},
		},
		"DifferentBaseDelay": {
			reason: "Different baseDelay values should not be equal.",
			args: args{
				a: efrlKey{baseDelay: dur(time.Second)},
				b: efrlKey{baseDelay: dur(2 * time.Second)},
			},
			want: want{equal: false},
		},
		"OnlyMaxDelayDiffers": {
			reason: "Differing maxDelay with identical baseDelay should not be equal.",
			args: args{
				a: efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(time.Second)},
				b: efrlKey{maxDelay: dur(2 * time.Minute), baseDelay: dur(time.Second)},
			},
			want: want{equal: false},
		},
		"OnlyBaseDelayDiffers": {
			reason: "Differing baseDelay with identical maxDelay should not be equal.",
			args: args{
				a: efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(time.Second)},
				b: efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(2 * time.Second)},
			},
			want: want{equal: false},
		},
		"BothFieldsDiffer": {
			reason: "Both fields differing should not be equal.",
			args: args{
				a: efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(time.Second)},
				b: efrlKey{maxDelay: dur(2 * time.Minute), baseDelay: dur(2 * time.Second)},
			},
			want: want{equal: false},
		},
		"MaxDelaySetVsZero": {
			reason: "A non-zero maxDelay should not equal a zero maxDelay.",
			args: args{
				a: efrlKey{maxDelay: dur(time.Minute)},
				b: efrlKey{},
			},
			want: want{equal: false},
		},
		"BaseDelaySetVsZero": {
			reason: "A non-zero baseDelay should not equal a zero baseDelay.",
			args: args{
				a: efrlKey{baseDelay: dur(time.Second)},
				b: efrlKey{},
			},
			want: want{equal: false},
		},
		"NegativeVsPositive": {
			reason: "A negative duration should not equal its positive counterpart.",
			args: args{
				a: efrlKey{maxDelay: dur(-time.Second)},
				b: efrlKey{maxDelay: dur(time.Second)},
			},
			want: want{equal: false},
		},
		"FieldsSwapped": {
			reason: "Swapping maxDelay and baseDelay between two keys must not collapse them to equal (positional sensitivity).",
			args: args{
				a: efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(time.Second)},
				b: efrlKey{maxDelay: dur(time.Second), baseDelay: dur(time.Minute)},
			},
			want: want{equal: false},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.args.a == tc.args.b
			if diff := cmp.Diff(tc.want.equal, got); diff != "" {
				t.Errorf("\n%s\nefrlKey ==: -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestEfrlKeyUsableAsMapKey(t *testing.T) {
	// efrlKey must be usable as a map key with value-based equality, which
	// is the whole point of the type. This is essentially a compile-time
	// assertion + a behavioural smoke check.
	m := map[efrlKey]string{}
	k1 := efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(time.Second)}
	k2 := efrlKey{maxDelay: dur(time.Minute), baseDelay: dur(time.Second)} // distinct instance, same value
	m[k1] = "hit"
	if got, ok := m[k2]; !ok || got != "hit" {
		t.Errorf("expected lookup with value-equal key to hit; got %q ok=%v", got, ok)
	}
}
