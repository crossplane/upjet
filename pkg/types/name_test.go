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

package types

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
				Camel:      "SomeSnake",
				LowerCamel: "someSnake",
				Snake:      "some_snake",
			},
		},
		"AcronymInBeginning": {
			in: "id_setting",
			want: Name{
				Camel:      "IDSetting",
				LowerCamel: "idSetting",
				Snake:      "id_setting",
			},
		},
		"AcronymInMiddle": {
			in: "some_api_param",
			want: Name{
				Camel:      "SomeAPIParam",
				LowerCamel: "someAPIParam",
				Snake:      "some_api_param",
			},
		},
		"AcronymInEnd": {
			in: "subnet_id",
			want: Name{
				Camel:      "SubnetID",
				LowerCamel: "subnetID",
				Snake:      "subnet_id",
			},
		},
		"OnlyAcronym": {
			in: "ip",
			want: Name{
				Camel:      "IP",
				LowerCamel: "ip",
				Snake:      "ip",
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
