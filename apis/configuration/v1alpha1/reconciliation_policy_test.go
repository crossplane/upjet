// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func dur(d time.Duration) *metav1.Duration {
	return &metav1.Duration{Duration: d}
}

func TestExponentialFailureRateLimiterEqual(t *testing.T) {
	type args struct {
		a *ExponentialFailureRateLimiter
		b *ExponentialFailureRateLimiter
	}
	type want struct {
		equal bool
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"BothNil": {
			reason: "Two nil receivers should be equal.",
			args: args{
				a: nil,
				b: nil,
			},
			want: want{equal: true},
		},
		"ANilBNonNil": {
			reason: "A nil value should not be equal to a non-nil empty value.",
			args: args{
				a: nil,
				b: &ExponentialFailureRateLimiter{},
			},
			want: want{equal: false},
		},
		"ANonNilBNil": {
			reason: "A non-nil empty value should not be equal to nil (symmetric).",
			args: args{
				a: &ExponentialFailureRateLimiter{},
				b: nil,
			},
			want: want{equal: false},
		},
		"BothEmpty": {
			reason: "Two empty (zero-valued) instances should be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{},
				b: &ExponentialFailureRateLimiter{},
			},
			want: want{equal: true},
		},
		"SameMaxDelayOnly": {
			reason: "Same MaxDelay with both BaseDelays nil should be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute)},
			},
			want: want{equal: true},
		},
		"SameBaseDelayOnly": {
			reason: "Same BaseDelay with both MaxDelays nil should be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{BaseDelay: dur(time.Second)},
				b: &ExponentialFailureRateLimiter{BaseDelay: dur(time.Second)},
			},
			want: want{equal: true},
		},
		"BothFieldsSame": {
			reason: "Identical MaxDelay and BaseDelay should be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute), BaseDelay: dur(time.Second)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute), BaseDelay: dur(time.Second)},
			},
			want: want{equal: true},
		},
		"SameValueDifferentPointerInstances": {
			reason: "Equality must be by underlying duration value, not pointer identity.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(5 * time.Second), BaseDelay: dur(1 * time.Second)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(5 * time.Second), BaseDelay: dur(1 * time.Second)},
			},
			want: want{equal: true},
		},
		"BothZeroDurations": {
			reason: "Explicit zero durations on both sides should be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(0), BaseDelay: dur(0)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(0), BaseDelay: dur(0)},
			},
			want: want{equal: true},
		},
		"DifferentMaxDelay": {
			reason: "Different MaxDelay values should not be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(2 * time.Minute)},
			},
			want: want{equal: false},
		},
		"DifferentBaseDelay": {
			reason: "Different BaseDelay values should not be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{BaseDelay: dur(time.Second)},
				b: &ExponentialFailureRateLimiter{BaseDelay: dur(2 * time.Second)},
			},
			want: want{equal: false},
		},
		"OnlyMaxDelayDiffers": {
			reason: "Differing MaxDelay with identical BaseDelay should not be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute), BaseDelay: dur(time.Second)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(2 * time.Minute), BaseDelay: dur(time.Second)},
			},
			want: want{equal: false},
		},
		"OnlyBaseDelayDiffers": {
			reason: "Differing BaseDelay with identical MaxDelay should not be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute), BaseDelay: dur(time.Second)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute), BaseDelay: dur(2 * time.Second)},
			},
			want: want{equal: false},
		},
		"BothFieldsDiffer": {
			reason: "Both fields differing should not be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute), BaseDelay: dur(time.Second)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(2 * time.Minute), BaseDelay: dur(2 * time.Second)},
			},
			want: want{equal: false},
		},
		"MaxDelaySetVsNil": {
			reason: "A set MaxDelay should not be equal to a nil MaxDelay.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute)},
				b: &ExponentialFailureRateLimiter{},
			},
			want: want{equal: false},
		},
		"NilVsMaxDelaySet": {
			reason: "Nil MaxDelay should not be equal to a set MaxDelay (symmetric).",
			args: args{
				a: &ExponentialFailureRateLimiter{},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute)},
			},
			want: want{equal: false},
		},
		"BaseDelaySetVsNil": {
			reason: "A set BaseDelay should not be equal to a nil BaseDelay.",
			args: args{
				a: &ExponentialFailureRateLimiter{BaseDelay: dur(time.Second)},
				b: &ExponentialFailureRateLimiter{},
			},
			want: want{equal: false},
		},
		"NilVsBaseDelaySet": {
			reason: "Nil BaseDelay should not be equal to a set BaseDelay (symmetric).",
			args: args{
				a: &ExponentialFailureRateLimiter{},
				b: &ExponentialFailureRateLimiter{BaseDelay: dur(time.Second)},
			},
			want: want{equal: false},
		},
		"ZeroDurationVsNil": {
			reason: "An explicit zero duration must be distinguishable from an unset (nil) field.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(0)},
				b: &ExponentialFailureRateLimiter{},
			},
			want: want{equal: false},
		},
		"SamePointerInstance": {
			reason: "An instance must be equal to itself.",
			args: func() args {
				rl := &ExponentialFailureRateLimiter{MaxDelay: dur(time.Minute), BaseDelay: dur(time.Second)}
				return args{a: rl, b: rl}
			}(),
			want: want{equal: true},
		},
		"NegativeDurationsEqual": {
			reason: "Negative durations with identical values should be equal.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(-time.Second)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(-time.Second)},
			},
			want: want{equal: true},
		},
		"NegativeVsPositive": {
			reason: "A negative duration should not equal its positive counterpart.",
			args: args{
				a: &ExponentialFailureRateLimiter{MaxDelay: dur(-time.Second)},
				b: &ExponentialFailureRateLimiter{MaxDelay: dur(time.Second)},
			},
			want: want{equal: false},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.args.a.Equal(tc.args.b)
			if diff := cmp.Diff(tc.want.equal, got); diff != "" {
				t.Errorf("\n%s\nExponentialFailureRateLimiter.Equal(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}