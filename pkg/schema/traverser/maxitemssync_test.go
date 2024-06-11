// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package traverser

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestMaxItemsSync(t *testing.T) {
	type args struct {
		srcSchema    TFResourceSchema
		targetSchema TFResourceSchema
	}
	type want struct {
		targetSchema TFResourceSchema
		err          error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"SyncMaxItemsConstraints": {
			reason: `maxItemsSync traverser can successfully sync the "MaxItems = 1" constraints from the source schema to the target schema.`,
			args: args{
				srcSchema: map[string]*schema.Resource{
					"test_resource": {
						Schema: map[string]*schema.Schema{
							"argument": {
								MaxItems: 1,
								Type:     schema.TypeList,
								Elem:     &schema.Resource{},
							},
						},
					},
				},
				targetSchema: map[string]*schema.Resource{
					"test_resource": {
						Schema: map[string]*schema.Schema{
							"argument": {
								MaxItems: 0,
								Type:     schema.TypeList,
								Elem:     &schema.Resource{},
							},
						},
					},
				},
			},
			want: want{
				targetSchema: map[string]*schema.Resource{
					"test_resource": {
						Schema: map[string]*schema.Schema{
							"argument": {
								MaxItems: 1,
								Type:     schema.TypeList,
								Elem:     &schema.Resource{},
							},
						},
					},
				},
			},
		},
		"NoSyncMaxItems": {
			reason: "If the MaxItems constraint is greater than 1, then maxItemsSync should not sync the constraint.",
			args: args{
				srcSchema: map[string]*schema.Resource{
					"test_resource": {
						Schema: map[string]*schema.Schema{
							"argument": {
								MaxItems: 2,
								Type:     schema.TypeList,
								Elem:     &schema.Resource{},
							},
						},
					},
				},
				targetSchema: map[string]*schema.Resource{
					"test_resource": {
						Schema: map[string]*schema.Schema{
							"argument": {
								MaxItems: 0,
								Type:     schema.TypeList,
								Elem:     &schema.Resource{},
							},
						},
					},
				},
			},
			want: want{
				targetSchema: map[string]*schema.Resource{
					"test_resource": {
						Schema: map[string]*schema.Schema{
							"argument": {
								MaxItems: 0,
								Type:     schema.TypeList,
								Elem:     &schema.Resource{},
							},
						},
					},
				},
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			copySrc := copySchema(tc.args.srcSchema)
			got := tc.args.srcSchema.Traverse(NewMaxItemsSync(tc.args.targetSchema))
			if diff := cmp.Diff(tc.want.err, got, test.EquateErrors()); diff != "" {
				t.Errorf("%s\nMaxItemsSync: -wantErr, +gotErr: \n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.err, got, test.EquateErrors()); diff != "" {
				t.Errorf("%s\nMaxItemsSync: -wantErr, +gotErr: \n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(copySrc, tc.srcSchema); diff != "" {
				t.Errorf("%s\nMaxItemsSync: -wantSourceSchema, +gotSourceSchema: \n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.targetSchema, tc.args.targetSchema); diff != "" {
				t.Errorf("%s\nMaxItemsSync: -wantTargetSchema, +gotTargetSchema: \n%s", tc.reason, diff)
			}
		})
	}
}

func copySchema(s TFResourceSchema) TFResourceSchema {
	result := make(TFResourceSchema)
	for k, v := range s {
		c := *v
		for n, s := range v.Schema {
			// not a deep copy but sufficient to check the schema constraints
			s := *s
			c.Schema[n] = &s
		}
		result[k] = &c
	}
	return result
}
