// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

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
