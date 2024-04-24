// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package terraform

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

func TestOperation(t *testing.T) {
	testErr := errors.New("test error")
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
					o.SetError(testErr)
					o.MarkEnd()
					o.Flush()
				},
			},
			want: want{
				checks: func(o *Operation) bool {
					return o.Type == "" && o.startTime == nil && o.endTime == nil && o.err == nil
				},
				result: true,
			},
		},
		"ClearedIncludingErrors": {
			args: args{
				calls: func(o *Operation) {
					o.MarkStart("type")
					o.SetError(testErr)
					o.MarkEnd()
					o.Clear(false)
				},
			},
			want: want{
				checks: func(o *Operation) bool {
					return o.Type == "" && o.startTime == nil && o.endTime == nil && o.err == nil
				},
				result: true,
			},
		},
		"ClearedPreservingErrors": {
			args: args{
				calls: func(o *Operation) {
					o.MarkStart("type")
					o.SetError(testErr)
					o.MarkEnd()
					o.Clear(true)
				},
			},
			want: want{
				checks: func(o *Operation) bool {
					return o.Type == "" && o.startTime == nil && o.endTime == nil && errors.Is(o.err, testErr)
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
