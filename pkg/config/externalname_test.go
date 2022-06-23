/*
Copyright 2021 Upbound Inc.
*/

package config

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
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

func TestTemplatedSetIdentifierArgumentFn(t *testing.T) {
	type args struct {
		nameFieldPath string
		base          map[string]interface{}
		externalName  string
	}
	type want struct {
		base map[string]interface{}
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"NoNameField": {
			reason: "Should be no-op if no name fieldpath given.",
			args: args{
				nameFieldPath: "",
				base:          map[string]interface{}{},
				externalName:  "myname",
			},
			want: want{
				base: map[string]interface{}{},
			},
		},
		"TopLevelSetIdentifier": {
			reason: "Should set top level identifier in arguments.",
			args: args{
				nameFieldPath: "cluster_name",
				base:          map[string]interface{}{},
				externalName:  "myname",
			},
			want: want{
				base: map[string]interface{}{
					"cluster_name": "myname",
				},
			},
		},
		"LeafNodeSetIdentifier": {
			reason: "Should set identifier in arguments even when it is in leaf nodes.",
			args: args{
				nameFieldPath: "cluster_settings.cluster_name",
				base:          map[string]interface{}{},
				externalName:  "myname",
			},
			want: want{
				base: map[string]interface{}{
					"cluster_settings": map[string]interface{}{
						"cluster_name": "myname",
					},
				},
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			TemplatedStringAsIdentifier(tc.args.nameFieldPath, "{{ .externalName }}").SetIdentifierArgumentFn(tc.args.base, tc.args.externalName)
			if diff := cmp.Diff(tc.want.base, tc.args.base); diff != "" {
				t.Fatalf("TemplatedStringAsIdentifier.SetIdentifierArgumentFn(...): -want, +got: %s", diff)
			}
		})
	}
}

func TestTemplatedGetIDFn(t *testing.T) {
	type args struct {
		tmpl           string
		externalName   string
		parameters     map[string]interface{}
		providerConfig map[string]interface{}
	}
	type want struct {
		id  string
		err error
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"NoExternalName": {
			reason: "Should work when only externalName is used.",
			args: args{
				tmpl: "olala/{{ .parameters.somethingElse }}",
				parameters: map[string]interface{}{
					"somethingElse": "otherthing",
				},
			},
			want: want{
				id: "olala/otherthing",
			},
		},
		"OnlyExternalName": {
			reason: "Should work when only externalName is used.",
			args: args{
				tmpl:         "olala/{{ .externalName }}",
				externalName: "myname",
			},
			want: want{
				id: "olala/myname",
			},
		},
		"MultipleParameters": {
			reason: "Should work when parameters and providerConfig are used as well.",
			args: args{
				tmpl:         "olala/{{ .parameters.ola }}:{{ .externalName }}/{{ .providerConfig.oma }}",
				externalName: "myname",
				parameters: map[string]interface{}{
					"ola": "paramval",
				},
				providerConfig: map[string]interface{}{
					"oma": "configval",
				},
			},
			want: want{
				id: "olala/paramval:myname/configval",
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			id, err := TemplatedStringAsIdentifier("", tc.args.tmpl).
				GetIDFn(context.TODO(),
					tc.args.externalName,
					tc.args.parameters,
					tc.args.providerConfig,
				)
			if diff := cmp.Diff(tc.want.err, err); diff != "" {
				t.Fatalf("TemplatedStringAsIdentifier.GetIDFn(...): -want, +got: %s", diff)
			}
			if diff := cmp.Diff(tc.want.id, id); diff != "" {
				t.Fatalf("TemplatedStringAsIdentifier.GetIDFn(...): -want, +got: %s", diff)
			}
		})
	}
}

func TestTemplatedGetExternalNameFn(t *testing.T) {
	type args struct {
		tmpl    string
		tfstate map[string]interface{}
	}
	type want struct {
		name string
		err  error
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"NoExternalName": {
			reason: "Should work when only externalName is used.",
			args: args{
				tmpl: "olala/{{ .parameters.somethingElse }}",
				tfstate: map[string]interface{}{
					"id": "olala/otherthing",
				},
			},
			want: want{
				name: "olala/otherthing",
			},
		},
		"BareExternalName": {
			reason: "Should work when only externalName is used in template.",
			args: args{
				tmpl: "{{ .externalName }}",
				tfstate: map[string]interface{}{
					"id": "myname",
				},
			},
			want: want{
				name: "myname",
			},
		},
		"NoID": {
			reason: "Should not work when ID cannot be found.",
			args: args{
				tmpl: "{{ .externalName }}",
				tfstate: map[string]interface{}{
					"another": "myname",
				},
			},
			want: want{
				err: errors.New(errIDNotFoundInTFState),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			n, err := TemplatedStringAsIdentifier("", tc.args.tmpl).
				GetExternalNameFn(tc.args.tfstate)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Fatalf("TemplatedStringAsIdentifier.GetExternalNameFn(...): -want, +got: %s", diff)
			}
			if diff := cmp.Diff(tc.want.name, n); diff != "" {
				t.Fatalf("TemplatedStringAsIdentifier.GetExternalNameFn(...): -want, +got: %s", diff)
			}
		})
	}
}
