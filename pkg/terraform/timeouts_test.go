// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package terraform

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/test"
)

func TestTimeoutsAsParameter(t *testing.T) {
	type args struct {
		to timeouts
	}
	type want struct {
		out map[string]string
	}
	cases := map[string]struct {
		args
		want
	}{
		"NoTimeouts": {
			want: want{
				out: map[string]string{},
			},
		},
		"SomeTimeout": {
			args: args{
				to: timeouts{
					Read: 3 * time.Minute,
				},
			},
			want: want{
				out: map[string]string{
					"read": "3m0s",
				},
			},
		},
		"AllTimeouts": {
			args: args{
				to: timeouts{
					Create: time.Minute,
					Update: 2 * time.Minute,
					Read:   3 * time.Minute,
					Delete: 4 * time.Minute,
				},
			},
			want: want{
				out: map[string]string{
					"create": "1m0s",
					"update": "2m0s",
					"read":   "3m0s",
					"delete": "4m0s",
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.args.to.asParameter()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("\n%s\nasParameter(...): -want out, +got out:\n%s", name, diff)
			}
		})
	}
}
func TestTimeoutsAsMetadata(t *testing.T) {
	type args struct {
		to timeouts
	}
	type want struct {
		out map[string]any
	}
	cases := map[string]struct {
		args
		want
	}{
		"NoTimeouts": {
			want: want{
				out: map[string]any{},
			},
		},
		"SomeTimeout": {
			args: args{
				to: timeouts{
					Read: 3 * time.Minute,
				},
			},
			want: want{
				out: map[string]any{
					"read": int64(180000000000),
				},
			},
		},
		"AllTimeouts": {
			args: args{
				to: timeouts{
					Create: time.Minute,
					Update: 2 * time.Minute,
					Read:   3 * time.Minute,
					Delete: 4 * time.Minute,
				},
			},
			want: want{
				out: map[string]any{
					"create": int64(60000000000),
					"update": int64(120000000000),
					"read":   int64(180000000000),
					"delete": int64(240000000000),
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.args.to.asMetadata()
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("\n%s\nasParameter(...): -want out, +got out:\n%s", name, diff)
			}
		})
	}
}
func TestInsertTimeoutsMeta(t *testing.T) {
	type args struct {
		rawMeta []byte
		to      timeouts
	}
	type want struct {
		out []byte
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"NoTimeoutNoMeta": {},
		"NoMetaButTimeout": {
			args: args{
				to: timeouts{
					Read: 2 * time.Minute,
				},
			},
			want: want{
				out: []byte(`{"e2bfb730-ecaa-11e6-8f88-34363bc7c4c0":{"read":120000000000}}`),
			},
		},
		"NonNilMetaButTimeout": {
			args: args{
				rawMeta: []byte(`{}`),
				to: timeouts{
					Read: 2 * time.Minute,
				},
			},
			want: want{
				out: []byte(`{"e2bfb730-ecaa-11e6-8f88-34363bc7c4c0":{"read":120000000000}}`),
			},
		},
		"CannotParseExistingMeta": {
			args: args{
				rawMeta: []byte(`{malformed}`),
				to: timeouts{
					Read: 2 * time.Minute,
				},
			},
			want: want{
				err: errors.Wrap(errors.New(`ReadString: expects " or n, but found m, error found in #2 byte of ...|{malformed}|..., bigger context ...|{malformed}|...`), `cannot parse existing metadata`), //nolint: golint
			},
		},
		"ExistingMetaAndTimeout": {
			args: args{
				rawMeta: []byte(`{"some-key":"some-value"}`),
				to: timeouts{
					Read: 2 * time.Minute,
				},
			},
			want: want{
				out: []byte(`{"e2bfb730-ecaa-11e6-8f88-34363bc7c4c0":{"read":120000000000},"some-key":"some-value"}`),
			},
		},
		"ExistingMetaNoTimeout": {
			args: args{
				rawMeta: []byte(`{"some-key":"some-value"}`),
			},
			want: want{
				out: []byte(`{"some-key":"some-value"}`),
			},
		},
		"ExistingMetaOverridesSomeTimeout": {
			args: args{
				rawMeta: []byte(`{"e2bfb730-ecaa-11e6-8f88-34363bc7c4c0":{"create":240000000000,"read":120000000000},"some-key":"some-value"}`),
				to: timeouts{
					Read: 1 * time.Minute,
				},
			},
			want: want{
				out: []byte(`{"e2bfb730-ecaa-11e6-8f88-34363bc7c4c0":{"create":240000000000,"read":60000000000},"some-key":"some-value"}`),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := insertTimeoutsMeta(tc.args.rawMeta, tc.args.to)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ninsertTimeoutsMeta(...): -want error, +got error:\n%s", name, diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("\n%s\ninsertTimeoutsMeta(...): -want out, +got out:\n%s", name, diff)
			}
		})
	}
}
