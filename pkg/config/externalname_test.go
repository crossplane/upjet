/*
Copyright 2021 Upbound Inc.
*/

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetExternalNameFromTemplated(t *testing.T) {
	type args struct {
		tmpl string
		val  string
	}
	type want struct {
		name string
		err  error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"OnlyExternalName": {
			reason: "Should work with bare external name.",
			args: args{
				tmpl: "{{ .externalName }}",
				val:  "myname",
			},
			want: want{
				name: "myname",
			},
		},
		"ExternalNameWithPrefix": {
			reason: "Should work with prefixed external names.",
			args: args{
				tmpl: "/some:other/prefix:{{ .externalName }}",
				val:  "/some:other/prefix:myname",
			},
			want: want{
				name: "myname",
			},
		},
		"ExternalNameWithSuffix": {
			reason: "Should work with suffixed external name.",
			args: args{
				tmpl: "{{ .externalName }}/olala:{{ .another }}/ola",
				val:  "myname/olala:omama/ola",
			},
			want: want{
				name: "myname",
			},
		},
		"ExternalNameInTheMiddle": {
			reason: "Should work with external name that is both prefixed and suffixed.",
			args: args{
				tmpl: "olala:{{ .externalName }}:omama:{{ .someOther }}",
				val:  "olala:myname:omama:okaka",
			},
			want: want{
				name: "myname",
			},
		},

		"ExternalNameInTheMiddleWithLessSpaceInTemplateVar": {
			reason: "Should work with external name that is both prefixed and suffixed.",
			args: args{
				tmpl: "olala:{{.externalName}}:omama:{{ .someOther }}",
				val:  "olala:myname:omama:okaka",
			},
			want: want{
				name: "myname",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			n, err := GetExternalNameFromTemplated(tc.args.tmpl, tc.args.val)
			if diff := cmp.Diff(tc.want.err, err); diff != "" {
				t.Errorf("\n%s\nGetExternalNameFromTemplated(...): -want, +got:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.name, n); diff != "" {
				t.Errorf("\n%s\nGetExternalNameFromTemplated(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
