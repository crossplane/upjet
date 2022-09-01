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
				tmpl: "{{ .external_name }}",
				val:  "myname",
			},
			want: want{
				name: "myname",
			},
		},
		"ExternalNameWithPrefix": {
			reason: "Should work with prefixed external names.",
			args: args{
				tmpl: "/some:other/prefix:{{ .external_name }}",
				val:  "/some:other/prefix:myname",
			},
			want: want{
				name: "myname",
			},
		},
		"ExternalNameWithSuffix": {
			reason: "Should work with suffixed external name.",
			args: args{
				tmpl: "{{ .external_name }}/olala:{{ .another }}/ola",
				val:  "myname/olala:omama/ola",
			},
			want: want{
				name: "myname",
			},
		},
		"ExternalNameInTheMiddle": {
			reason: "Should work with external name that is both prefixed and suffixed.",
			args: args{
				tmpl: "olala:{{ .external_name }}:omama:{{ .someOther }}",
				val:  "olala:myname:omama:okaka",
			},
			want: want{
				name: "myname",
			},
		},

		"ExternalNameInTheMiddleWithLessSpaceInTemplateVar": {
			reason: "Should work with external name that is both prefixed and suffixed.",
			args: args{
				tmpl: "olala:{{.external_name}}:omama:{{ .someOther }}",
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
		base          map[string]any
		externalName  string
	}
	type want struct {
		base map[string]any
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
				base:          map[string]any{},
				externalName:  "myname",
			},
			want: want{
				base: map[string]any{},
			},
		},
		"TopLevelSetIdentifier": {
			reason: "Should set top level identifier in arguments.",
			args: args{
				nameFieldPath: "cluster_name",
				base:          map[string]any{},
				externalName:  "myname",
			},
			want: want{
				base: map[string]any{
					"cluster_name": "myname",
				},
			},
		},
		"LeafNodeSetIdentifier": {
			reason: "Should set identifier in arguments even when it is in leaf nodes.",
			args: args{
				nameFieldPath: "cluster_settings.cluster_name",
				base:          map[string]any{},
				externalName:  "myname",
			},
			want: want{
				base: map[string]any{
					"cluster_settings": map[string]any{
						"cluster_name": "myname",
					},
				},
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			TemplatedStringAsIdentifier(tc.args.nameFieldPath, "{{ .external_name }}").SetIdentifierArgumentFn(tc.args.base, tc.args.externalName)
			if diff := cmp.Diff(tc.want.base, tc.args.base); diff != "" {
				t.Fatalf("TemplatedStringAsIdentifier.SetIdentifierArgumentFn(...): -want, +got: %s", diff)
			}
		})
	}
}

func TestTemplatedGetIDFn(t *testing.T) {
	type args struct {
		tmpl         string
		externalName string
		parameters   map[string]any
		setup        map[string]any
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
			reason: "Should work when only external_name is used.",
			args: args{
				tmpl: "olala/{{ .parameters.somethingElse }}",
				parameters: map[string]any{
					"somethingElse": "otherthing",
				},
			},
			want: want{
				id: "olala/otherthing",
			},
		},
		"OnlyExternalName": {
			reason: "Should work when only external_name is used.",
			args: args{
				tmpl:         "olala/{{ .external_name }}",
				externalName: "myname",
			},
			want: want{
				id: "olala/myname",
			},
		},
		"MultipleParameters": {
			reason: "Should work when parameters and terraformProviderConfig are used as well.",
			args: args{
				tmpl:         "olala/{{ .parameters.ola }}:{{ .external_name }}/{{ .setup.configuration.oma }}",
				externalName: "myname",
				parameters: map[string]any{
					"ola": "paramval",
				},
				setup: map[string]any{
					"configuration": map[string]any{
						"oma": "configval",
					},
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
					tc.args.setup,
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
		tfstate map[string]any
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
			reason: "Should work when no external_name is used.",
			args: args{
				tmpl: "olala/{{ .parameters.somethingElse }}",
				tfstate: map[string]any{
					"id": "olala/otherthing",
				},
			},
			want: want{
				name: "olala/otherthing",
			},
		},
		"BareExternalName": {
			reason: "Should work when only external_name is used in template.",
			args: args{
				tmpl: "{{ .external_name }}",
				tfstate: map[string]any{
					"id": "myname",
				},
			},
			want: want{
				name: "myname",
			},
		},
		"ExternalNameSpaces": {
			reason: "Should work when external_name variable has random space characters.",
			args: args{
				tmpl: "another/thing:{{  .external_name         }}/something",
				tfstate: map[string]any{
					"id": "another/thing:myname/something",
				},
			},
			want: want{
				name: "myname",
			},
		},
		"DifferentLeftRightSeparators": {
			reason: "Should work when external_name has different left and right separators.",
			args: args{
				tmpl: "another/{{ .parameters.another }}:{{ .external_name }}/somethingelse",
				tfstate: map[string]any{
					"id": "another/thing:myname/somethingelse",
				},
			},
			want: want{
				name: "myname",
			},
		},
		"NoID": {
			reason: "Should not work when ID cannot be found.",
			args: args{
				tmpl: "{{ .external_name }}",
				tfstate: map[string]any{
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
