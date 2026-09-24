// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestFilterInstanceDiff(t *testing.T) {
	cases := map[string]struct {
		reason    string
		d         *tf.InstanceDiff
		want      map[string]*tf.ResourceAttrDiff
		wantEmpty bool
	}{
		"NilDiff": {
			reason: "A nil diff should be tolerated.",
			d:      nil,
		},
		"NilAttributeDiff": {
			reason: "An attribute without a diff carries no information and should be dropped.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"name": nil,
			}},
			want:      map[string]*tf.ResourceAttrDiff{},
			wantEmpty: true,
		},
		"UnchangedValue": {
			reason: "An attribute whose old and new values are equal did not change.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"name": {Old: "example", New: "example"},
			}},
			want:      map[string]*tf.ResourceAttrDiff{},
			wantEmpty: true,
		},
		"ChangedValue": {
			reason: "An attribute whose old and new values differ is a meaningful change.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"name": {Old: "example", New: "example2"},
			}},
			want: map[string]*tf.ResourceAttrDiff{
				"name": {Old: "example", New: "example2"},
			},
		},
		"ComputedWithoutPriorValue": {
			reason: "An attribute that is unknown until apply is recomputed on every plan, so it is not a change the user made.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"tags_all.%": {Old: "", New: "", NewComputed: true},
			}},
			want:      map[string]*tf.ResourceAttrDiff{},
			wantEmpty: true,
		},
		"ComputedWithPriorValue": {
			reason: "A computed attribute should be dropped whether or not it had a prior value: its New is an empty placeholder, so comparing it against Old is meaningless.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"tags_all.%": {Old: "2", New: "", NewComputed: true},
			}},
			want:      map[string]*tf.ResourceAttrDiff{},
			wantEmpty: true,
		},
		"ComputedRequiresNew": {
			reason: "Replacing the external resource is meaningful even when the value that triggers it is computed.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"subnet_ids.#": {Old: "1", New: "", NewComputed: true, RequiresNew: true},
			}},
			want: map[string]*tf.ResourceAttrDiff{
				"subnet_ids.#": {Old: "1", New: "", NewComputed: true, RequiresNew: true},
			},
		},
		"RequiresNewWithUnchangedValue": {
			reason: "A force-new attribute is kept even if its values compare equal, so that a pending replacement is never hidden.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"availability_zone": {Old: "us-east-1a", New: "us-east-1a", RequiresNew: true},
			}},
			want: map[string]*tf.ResourceAttrDiff{
				"availability_zone": {Old: "us-east-1a", New: "us-east-1a", RequiresNew: true},
			},
		},
		"AttributeRemoved": {
			reason: "Removing an attribute that had a value is a meaningful change.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"description": {Old: "some description", New: "", NewRemoved: true},
			}},
			want: map[string]*tf.ResourceAttrDiff{
				"description": {Old: "some description", New: "", NewRemoved: true},
			},
		},
		"EmptyAttributeRemoved": {
			reason: "Removing an attribute that is already empty is a no-op.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"description": {Old: "", New: "", NewRemoved: true},
			}},
			want:      map[string]*tf.ResourceAttrDiff{},
			wantEmpty: true,
		},
		"OnlyComputedAttributes": {
			reason: "A diff that consists solely of recomputed attributes means nothing meaningful changed.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"id":         {Old: "", New: "", NewComputed: true},
				"tags_all.%": {Old: "", New: "", NewComputed: true},
			}},
			want:      map[string]*tf.ResourceAttrDiff{},
			wantEmpty: true,
		},
		"MeaningfulChangeAmongComputedAttributes": {
			reason: "Only the attributes the user changed should survive the recomputed ones.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"id":         {Old: "", New: "", NewComputed: true},
				"tags_all.%": {Old: "", New: "", NewComputed: true},
				"name":       {Old: "", New: "example"},
			}},
			want: map[string]*tf.ResourceAttrDiff{
				"name": {Old: "", New: "example"},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			filterInstanceDiff(tc.d)
			if tc.d == nil {
				return
			}
			if diff := cmp.Diff(tc.want, tc.d.Attributes); diff != "" {
				t.Errorf("\n%s\nfilterInstanceDiff(...): -want attributes, +got attributes:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.wantEmpty, tc.d.Empty()); diff != "" {
				t.Errorf("\n%s\nfilterInstanceDiff(...): -want Empty(), +got Empty():\n%s", tc.reason, diff)
			}
		})
	}
}
