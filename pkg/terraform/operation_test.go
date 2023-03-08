/*
Copyright 2021 Upbound Inc.
*/

package terraform

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestOperation(t *testing.T) {
	type args struct {
		calls func(o *Operation)
	}
	type want struct {
		checks func(o *Operation) bool
		result bool
	}

	cases := map[string]struct {
		args
		want
	}{
		"Running": {
			args: args{
				calls: func(o *Operation) {
					o.MarkStart("type")
				},
			},
			want: want{
				checks: func(o *Operation) bool {
					return o.IsRunning() && !o.IsEnded()
				},
				result: true,
			},
		},
		"Ended": {
			args: args{
				calls: func(o *Operation) {
					o.MarkStart("type")
					o.MarkEnd()
				},
			},
			want: want{
				checks: func(o *Operation) bool {
					return !o.IsRunning() && o.IsEnded()
				},
				result: true,
			},
		},
		"Flushed": {
			args: args{
				calls: func(o *Operation) {
					o.MarkStart("type")
					o.MarkEnd()
					o.Flush()
				},
			},
			want: want{
				checks: func(o *Operation) bool {
					return o.Type == "" && o.startTime == nil && o.endTime == nil
				},
				result: true,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o := &Operation{}
			tc.args.calls(o)
			if diff := cmp.Diff(tc.want.result, tc.checks(o)); diff != "" {
				t.Errorf("\n%s\nOperation(...): -want error, +got error:\n%s", name, diff)
			}
		})
	}
}
