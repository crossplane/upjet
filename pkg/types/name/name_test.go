/*
Copyright 2021 Upbound Inc.
*/

package name

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNewNameFromSnake(t *testing.T) {
	cases := map[string]struct {
		in   string
		want Name
	}{
		"Normal": {
			in: "some_snake",
			want: Name{
				Camel:              "SomeSnake",
				CamelComputed:      "SomeSnake",
				LowerCamel:         "someSnake",
				LowerCamelComputed: "someSnake",
				Snake:              "some_snake",
			},
		},
		"AcronymInBeginning": {
			in: "id_setting",
			want: Name{
				Camel:              "IDSetting",
				CamelComputed:      "IdSetting",
				LowerCamel:         "idSetting",
				LowerCamelComputed: "idSetting",
				Snake:              "id_setting",
			},
		},
		"AcronymInMiddle": {
			in: "some_api_param",
			want: Name{
				Camel:              "SomeAPIParam",
				CamelComputed:      "SomeApiParam",
				LowerCamel:         "someAPIParam",
				LowerCamelComputed: "someApiParam",
				Snake:              "some_api_param",
			},
		},
		"AcronymInEnd": {
			in: "subnet_id",
			want: Name{
				Camel:              "SubnetID",
				CamelComputed:      "SubnetId",
				LowerCamel:         "subnetID",
				LowerCamelComputed: "subnetId",
				Snake:              "subnet_id",
			},
		},
		"OnlyAcronym": {
			in: "ip",
			want: Name{
				Camel:              "IP",
				CamelComputed:      "Ip",
				LowerCamel:         "ip",
				LowerCamelComputed: "ip",
				Snake:              "ip",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := NewFromSnake(tc.in)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\nNewNameFromSnake(...): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestNewNameFromCamel(t *testing.T) {
	cases := map[string]struct {
		in   string
		want Name
	}{
		"Normal": {
			in: "SomeCamel",
			want: Name{
				Camel:              "SomeCamel",
				CamelComputed:      "SomeCamel",
				LowerCamel:         "someCamel",
				LowerCamelComputed: "someCamel",
				Snake:              "some_camel",
			},
		},
		"AcronymInBeginning": {
			in: "IDSetting",
			want: Name{
				Camel:              "IDSetting",
				CamelComputed:      "IdSetting",
				LowerCamel:         "idSetting",
				LowerCamelComputed: "idSetting",
				Snake:              "id_setting",
			},
		},
		"AcronymInMiddle": {
			in: "SomeAPIParam",
			want: Name{
				Camel:              "SomeAPIParam",
				CamelComputed:      "SomeApiParam",
				LowerCamel:         "someAPIParam",
				LowerCamelComputed: "someApiParam",
				Snake:              "some_api_param",
			},
		},
		"AcronymInEnd": {
			in: "SubnetID",
			want: Name{
				Camel:              "SubnetID",
				CamelComputed:      "SubnetId",
				LowerCamel:         "subnetID",
				LowerCamelComputed: "subnetId",
				Snake:              "subnet_id",
			},
		},
		"OnlyAcronym": {
			in: "IP",
			want: Name{
				Camel:              "IP",
				CamelComputed:      "Ip",
				LowerCamel:         "ip",
				LowerCamelComputed: "ip",
				Snake:              "ip",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := NewFromCamel(tc.in)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\nNewNameFromSnake(...): -want, +got:\n%s", diff)
			}
		})
	}
}
