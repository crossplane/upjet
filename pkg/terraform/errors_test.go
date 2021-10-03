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
	"os"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	cases := map[string]struct {
		in     error
		result bool
	}{
		"TruePositive": {
			in:     &errNotFound{},
			result: true,
		},
		"TrueNegative": {
			in:     os.ErrNotExist,
			result: false,
		},
		"TruePositiveNewFn": {
			in:     NewNotFound(),
			result: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result := IsNotFound(tc.in)
			if result != tc.result {
				t.Errorf("%s test failed", name)
			}
		})
	}
}
