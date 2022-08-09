/*
Copyright 2022 Upbound Inc.
*/

package name

import (
	"github.com/google/go-cmp/cmp"
	"testing"
)

func TestReferenceFieldName(t *testing.T) {
	type args struct {
		n             Name
		isList        bool
		camelOverride string
	}
	type want struct {
		n Name
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NonListDefault": {
			reason: "It should work with normal case without override.",
			args: args{
				n:             NewFromSnake("some_field"),
				isList:        false,
				camelOverride: "",
			},
			want: want{
				n: NewFromSnake("some_field_ref"),
			},
		},
		"ListDefault": {
			reason: "It should work with list case without override.",
			args: args{
				n:             NewFromSnake("some_field"),
				isList:        true,
				camelOverride: "",
			},
			want: want{
				n: NewFromSnake("some_field_refs"),
			},
		},
		"ListOverridden": {
			reason: "It should work with list case even though it's overridden.",
			args: args{
				n:             NewFromSnake("some_field"),
				isList:        true,
				camelOverride: "AnotherField",
			},
			want: want{
				n: NewFromSnake("another_field"),
			},
		},
		"NonListOverridden": {
			reason: "It should work with normal case even though it's overridden.",
			args: args{
				n:             NewFromSnake("some_field"),
				isList:        false,
				camelOverride: "AnotherField",
			},
			want: want{
				n: NewFromSnake("another_field"),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := ReferenceFieldName(tc.args.n, tc.args.isList, tc.args.camelOverride)
			if diff := cmp.Diff(tc.want.n, got); diff != "" {
				t.Errorf("\nReferenceFieldName(...): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestSelectorFieldName(t *testing.T) {
	type args struct {
		n             Name
		camelOverride string
	}
	type want struct {
		n Name
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"Default": {
			reason: "It should work with normal case without override.",
			args: args{
				n:             NewFromSnake("some_field"),
				camelOverride: "",
			},
			want: want{
				n: NewFromSnake("some_field_selector"),
			},
		},
		"Overridden": {
			reason: "It should return the override if given.",
			args: args{
				n:             NewFromSnake("some_field"),
				camelOverride: "AnotherFieldSelector",
			},
			want: want{
				n: NewFromSnake("another_field_selector"),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := SelectorFieldName(tc.args.n, tc.args.camelOverride)
			if diff := cmp.Diff(tc.want.n, got); diff != "" {
				t.Errorf("\nSelectorFieldName(...): -want, +got:\n%s", diff)
			}
		})
	}
}
