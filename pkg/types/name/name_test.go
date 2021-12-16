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
			got := NewNameFromSnake(tc.in)

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
			got := NewNameFromCamel(tc.in)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\nNewNameFromSnake(...): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestCapitalizeAcronyms(t *testing.T) {
	tests := map[string]struct {
		arg  string
		want string
	}{
		"NameWithPrefixAcronym": {
			arg:  "Sqlserver",
			want: "SQLServer",
		},
		"NameWithSuffixAcronym": {
			arg:  "Serverid",
			want: "ServerID",
		},
		"NameWithMultipleAcronyms": {
			arg:  "Sqlserverid",
			want: "SQLServerID",
		},
		"NameWithInterimAcronym": {
			arg:  "Mysqlserver",
			want: "Mysqlserver",
		},
		"NameOnlyAcronyms": {
			arg:  "Sqlid",
			want: "SQLID",
		},
		"EmptyName": {
			arg:  "",
			want: "",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := CapitalizeAcronyms(tt.arg); got != tt.want {
				t.Errorf("CapitalizeAcronyms(tt.arg) = %v, want %v", got, tt.want)
			}
		})
	}
}
