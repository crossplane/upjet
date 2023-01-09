// Copyright 2023 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package migration

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSplitPathComponents(t *testing.T) {
	tests := map[string]struct {
		want []string
	}{
		`m['a.b.c']`: {
			want: []string{`m['a.b.c']`},
		},
		`m["a.b.c"]`: {
			want: []string{`m["a.b.c"]`},
		},
		`m[a.b.c]`: {
			want: []string{`m[a.b.c]`},
		},
		`m[a.b.c.d.e]`: {
			want: []string{`m[a.b.c.d.e]`},
		},
		`m['a.b.c.d.e']`: {
			want: []string{`m['a.b.c.d.e']`},
		},
		`m['a.b']`: {
			want: []string{`m['a.b']`},
		},
		`m['a']`: {
			want: []string{`m['a']`},
		},
		`m['a'].b`: {
			want: []string{`m['a']`, `b`},
		},
		`a`: {
			want: []string{`a`},
		},
		`a.b`: {
			want: []string{`a`, `b`},
		},
		`a.b.c`: {
			want: []string{`a`, `b`, `c`},
		},
		`a.b.c.m['a.b.c']`: {
			want: []string{`a`, `b`, `c`, `m['a.b.c']`},
		},
		`a.b.m['a.b.c'].c`: {
			want: []string{`a`, `b`, `m['a.b.c']`, `c`},
		},
		`m['a.b.c'].a.b.c`: {
			want: []string{`m['a.b.c']`, `a`, `b`, `c`},
		},
		`m[a.b.c].a.b.c`: {
			want: []string{`m[a.b.c]`, `a`, `b`, `c`},
		},
		`m[0]`: {
			want: []string{`m[0]`},
		},
		`a.b.c.m[0]`: {
			want: []string{`a`, `b`, `c`, `m[0]`},
		},
		`m[0].a.b.c`: {
			want: []string{`m[0]`, `a`, `b`, `c`},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, splitPathComponents(name)); diff != "" {
				t.Errorf("splitPathComponents(%s): -want, +got:\n%s\n", name, diff)
			}
		})
	}
}
