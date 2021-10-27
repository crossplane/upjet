/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
					return o.Type == "" && o.StartTime() == nil && o.EndTime() == nil
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
