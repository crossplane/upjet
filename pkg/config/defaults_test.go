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

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestDefaultResource(t *testing.T) {
	type args struct {
		name string
		sch  *schema.Resource
		opts []ResourceOption
	}

	cases := map[string]struct {
		reason string
		args   args
		want   *Resource
	}{
		"ThreeSectionsName": {
			reason: "It should return GVK properly for names with three sections",
			args: args{
				name: "aws_ec2_instance",
			},
			want: &Resource{
				Name:         "aws_ec2_instance",
				ShortGroup:   "ec2",
				Kind:         "Instance",
				Version:      "v1alpha1",
				ExternalName: NameAsIdentifier,
				References:   map[string]Reference{},
				Sensitive:    NopSensitive,
			},
		},
		"TwoSectionsName": {
			reason: "It should return GVK properly for names with three sections",
			args: args{
				name: "aws_instance",
			},
			want: &Resource{
				Name:         "aws_instance",
				ShortGroup:   "aws",
				Kind:         "Instance",
				Version:      "v1alpha1",
				ExternalName: NameAsIdentifier,
				References:   map[string]Reference{},
				Sensitive:    NopSensitive,
			},
		},
	}

	// TODO(muvaf): Find a way to compare function pointers.
	ignoreUnexported := []cmp.Option{
		cmpopts.IgnoreFields(Sensitive{}, "fieldPaths", "AdditionalConnectionDetailsFn"),
		cmpopts.IgnoreFields(LateInitializer{}, "ignoredCanonicalFieldPaths"),
		cmpopts.IgnoreFields(ExternalName{}, "SetIdentifierArgumentFn", "GetNameFn", "GetIDFn"),
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := DefaultResource(tc.args.name, tc.args.sch, tc.args.opts...)
			if diff := cmp.Diff(tc.want, r, ignoreUnexported...); diff != "" {
				t.Errorf("\n%s\nDefaultResource(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
