/*
Copyright 2021 Upbound Inc.
*/

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
		"NameWithPrefixAcronym": {
			reason: "It should return prefix acronym in capital case",
			args: args{
				name: "aws_db_sql_server",
			},
			want: &Resource{
				Name:         "aws_db_sql_server",
				ShortGroup:   "db",
				Kind:         "SQLServer",
				Version:      "v1alpha1",
				ExternalName: NameAsIdentifier,
				References:   map[string]Reference{},
				Sensitive:    NopSensitive,
			},
		},
		"NameWithSuffixAcronym": {
			reason: "It should return suffix acronym in capital case",
			args: args{
				name: "aws_db_server_id",
			},
			want: &Resource{
				Name:         "aws_db_server_id",
				ShortGroup:   "db",
				Kind:         "ServerID",
				Version:      "v1alpha1",
				ExternalName: NameAsIdentifier,
				References:   map[string]Reference{},
				Sensitive:    NopSensitive,
			},
		},
		"NameWithMultipleAcronyms": {
			reason: "It should return both prefix & suffix acronyms in capital case",
			args: args{
				name: "aws_db_sql_server_id",
			},
			want: &Resource{
				Name:         "aws_db_sql_server_id",
				ShortGroup:   "db",
				Kind:         "SQLServerID",
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
		cmpopts.IgnoreFields(ExternalName{}, "SetIdentifierArgumentFn", "GetExternalNameFn", "GetIDFn"),
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
