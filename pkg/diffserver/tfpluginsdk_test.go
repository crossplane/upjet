// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

// str builds a concrete FieldValue holding a string, which is the only kind
// of concrete value a plugin SDKv2 flatmap diff can produce.
func str(s string) *diffv1alpha1.FieldValue {
	return &diffv1alpha1.FieldValue{
		Kind: &diffv1alpha1.FieldValue_Value{Value: structpb.NewStringValue(s)},
	}
}

func absent(a diffv1alpha1.Absence) *diffv1alpha1.FieldValue {
	return &diffv1alpha1.FieldValue{Kind: &diffv1alpha1.FieldValue_Absence{Absence: a}}
}

// ignoreComputedAt drops the timestamp, which is wall-clock and therefore not
// comparable against a fixture.
var ignoreComputedAt = protocmp.IgnoreFields(&diffv1alpha1.PlanResponse{}, "computed_at")

func TestPlanResponseAction(t *testing.T) {
	changed := map[string]*tf.ResourceAttrDiff{
		"description": {Old: "old", New: "new"},
	}
	forceNew := map[string]*tf.ResourceAttrDiff{
		"region": {Old: "us-east-1", New: "eu-central-1", RequiresNew: true},
	}

	cases := map[string]struct {
		reason   string
		d        *tf.InstanceDiff
		exists   bool
		declared map[string]any
		want     diffv1alpha1.Action
	}{
		"NilDiffExistingResource": {
			reason: "An existing resource with no diff at all has nothing to do.",
			d:      nil,
			exists: true,
			want:   diffv1alpha1.Action_ACTION_NO_OP,
		},
		"NilDiffAbsentResource": {
			reason: "A resource that does not exist yet is created even when there is no diff to apply.",
			d:      nil,
			exists: false,
			want:   diffv1alpha1.Action_ACTION_CREATE,
		},
		"EmptyDiff": {
			reason: "An existing resource whose filtered diff is empty is up to date.",
			d:      &tf.InstanceDiff{},
			exists: true,
			want:   diffv1alpha1.Action_ACTION_NO_OP,
		},
		"AttributeChanged": {
			reason: "An existing resource with an in-place change is updated.",
			d:      &tf.InstanceDiff{Attributes: changed},
			exists: true,
			want:   diffv1alpha1.Action_ACTION_UPDATE,
		},
		"ForceNewAttributeChanged": {
			reason: "An existing resource whose change cannot be applied in place is replaced.",
			d:      &tf.InstanceDiff{Attributes: forceNew},
			exists: true,
			want:   diffv1alpha1.Action_ACTION_REPLACE,
		},
		"ForceNewOnAbsentResource": {
			reason: "A resource that does not exist yet is created, never replaced, however its attributes are marked.",
			d:      &tf.InstanceDiff{Attributes: forceNew},
			exists: false,
			want:   diffv1alpha1.Action_ACTION_CREATE,
		},
		"OnlyCountKeysChanged": {
			reason: "A diff that reports only a collection size is not empty, so it must not be reported as a no-op even though no field change can be attributed to it.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"tags.%": {Old: "1", New: "2"},
			}},
			exists: true,
			want:   diffv1alpha1.Action_ACTION_UPDATE,
		},
	}

	s := &PlanService{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := s.planResponse(tc.d, tc.exists, tc.declared)
			if err != nil {
				t.Fatalf("\n%s\nplanResponse(...): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, r.GetAction()); diff != "" {
				t.Errorf("\n%s\nplanResponse(...): -want action, +got action:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestPlanResponseChanges(t *testing.T) {
	cases := map[string]struct {
		reason   string
		d        *tf.InstanceDiff
		exists   bool
		declared map[string]any
		want     *diffv1alpha1.PlanResponse
	}{
		"ValueChanged": {
			reason: "Both sides of an in-place change should be reported.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"deletion_window_in_days": {Old: "7", New: "30"},
			}},
			exists:   true,
			declared: map[string]any{"deletion_window_in_days": "30"},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_UPDATE,
				Changes: []*diffv1alpha1.FieldChange{{
					Field:   "spec.forProvider.deletionWindowInDays",
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
					Actual:  str("7"),
					Planned: str("30"),
				}},
			},
		},
		"AttributeAdded": {
			reason: "Flatmap cannot tell an absent attribute from an empty one, so an empty Old is reported as no current value rather than as a change from the empty string.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"description": {Old: "", New: "production signing key"},
			}},
			exists:   true,
			declared: map[string]any{"description": "production signing key"},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_UPDATE,
				Changes: []*diffv1alpha1.FieldChange{{
					Field:   "spec.forProvider.description",
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
					Planned: str("production signing key"),
				}},
			},
		},
		"AttributeRemoved": {
			reason: "A removal should report the current value and no desired value.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"description": {Old: "legacy", New: "", NewRemoved: true},
			}},
			exists:   true,
			declared: map[string]any{"description": ""},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_UPDATE,
				Changes: []*diffv1alpha1.FieldChange{{
					Field:  "spec.forProvider.description",
					Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
					Actual: str("legacy"),
				}},
			},
		},
		"ValueKnownAfterApply": {
			reason: "A computed value is not known at plan time, so the desired side reports why rather than carrying a placeholder.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"policy": {Old: "current", New: "", NewComputed: true},
			}},
			exists:   true,
			declared: map[string]any{"policy": ""},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_UPDATE,
				Changes: []*diffv1alpha1.FieldChange{{
					Field:   "spec.forProvider.policy",
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
					Actual:  str("current"),
					Planned: absent(diffv1alpha1.Absence_ABSENCE_UNKNOWN),
				}},
			},
		},
		"SensitiveValueRedacted": {
			reason: "Neither side of a sensitive attribute may appear in the plan, whatever its values are.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"master_password": {Old: "hunter2", New: "hunter3", Sensitive: true},
			}},
			exists:   true,
			declared: map[string]any{"master_password": "hunter3"},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_UPDATE,
				Changes: []*diffv1alpha1.FieldChange{{
					Field:   "spec.forProvider.masterPassword",
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
					Actual:  absent(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
					Planned: absent(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
				}},
			},
		},
		"SensitiveAndForceNew": {
			reason: "A redacted attribute should still report that it forces a replacement.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"master_password": {Old: "hunter2", New: "hunter3", Sensitive: true, RequiresNew: true},
			}},
			exists:   true,
			declared: map[string]any{"master_password": "hunter3"},
			want: &diffv1alpha1.PlanResponse{
				Action:          diffv1alpha1.Action_ACTION_REPLACE,
				RequiresReplace: true,
				ReplaceFields:   []string{"spec.forProvider.masterPassword"},
				Changes: []*diffv1alpha1.FieldChange{{
					Field:           "spec.forProvider.masterPassword",
					Origin:          diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
					Actual:          absent(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
					Planned:         absent(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
					RequiresReplace: true,
				}},
			},
		},
		"CountKeysDropped": {
			reason: "A collection's size is not a field the user wrote, so only the element change should be reported.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"tags.%":       {Old: "1", New: "2"},
				"tags.Env":     {Old: "", New: "prod"},
				"subnet_ids.#": {Old: "1", New: "2"},
				"subnet_ids.1": {Old: "", New: "subnet-b"},
			}},
			exists:   true,
			declared: map[string]any{"tags": map[string]any{"Env": "prod"}, "subnet_ids": []any{"subnet-a", "subnet-b"}},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_UPDATE,
				Changes: []*diffv1alpha1.FieldChange{
					{Field: "spec.forProvider.subnetIds.1", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Planned: str("subnet-b")},
					{Field: "spec.forProvider.tags.Env", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Planned: str("prod")},
				},
			},
		},
		"NilAttributeDropped": {
			reason: "An attribute without a diff carries no information and must not panic the conversion.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"description": nil,
				"region":      {Old: "a", New: "b"},
			}},
			exists:   true,
			declared: map[string]any{"description": "", "region": "b"},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_UPDATE,
				Changes: []*diffv1alpha1.FieldChange{
					{Field: "spec.forProvider.region", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("a"), Planned: str("b")},
				},
			},
		},
		"NestedPathLeftInTerraformForm": {
			reason: "Only the leading segment can be translated without the schema: a nested block's field name and a user-supplied map key are indistinguishable in a flatmap key, and camel casing a map key would corrupt it.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"logging_config.0.target_bucket": {Old: "old", New: "new"},
				"tags.Env":                       {Old: "dev", New: "prod"},
			}},
			exists:   true,
			declared: map[string]any{"logging_config": []any{}, "tags": map[string]any{"Env": "prod"}},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_UPDATE,
				Changes: []*diffv1alpha1.FieldChange{
					{Field: "spec.forProvider.loggingConfig.0.target_bucket", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("old"), Planned: str("new")},
					{Field: "spec.forProvider.tags.Env", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("dev"), Planned: str("prod")},
				},
			},
		},
		"ChangesAndReplaceFieldsSorted": {
			reason: "Map iteration is unordered, so a plan must be sorted to stay stable across calls.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"zone":        {Old: "a", New: "b", RequiresNew: true},
				"alpha":       {Old: "a", New: "b"},
				"region":      {Old: "a", New: "b", RequiresNew: true},
				"description": {Old: "a", New: "b"},
			}},
			exists:   true,
			declared: map[string]any{"zone": "b", "alpha": "b", "region": "b", "description": "b"},
			want: &diffv1alpha1.PlanResponse{
				Action:          diffv1alpha1.Action_ACTION_REPLACE,
				RequiresReplace: true,
				ReplaceFields: []string{
					"spec.forProvider.region",
					"spec.forProvider.zone",
				},
				Changes: []*diffv1alpha1.FieldChange{
					{Field: "spec.forProvider.alpha", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("a"), Planned: str("b")},
					{Field: "spec.forProvider.description", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("a"), Planned: str("b")},
					{Field: "spec.forProvider.region", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("a"), Planned: str("b"), RequiresReplace: true},
					{Field: "spec.forProvider.zone", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("a"), Planned: str("b"), RequiresReplace: true},
				},
			},
		},
		"CreateReportsEveryAttributeAsAdded": {
			reason: "On a create the resource has no prior state, so every attribute is reported as an addition.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"description": {Old: "", New: "new key"},
				"region":      {Old: "", New: "eu-central-1", RequiresNew: true},
			}},
			exists:   false,
			declared: map[string]any{"description": "new key", "region": "eu-central-1"},
			want: &diffv1alpha1.PlanResponse{
				Action:          diffv1alpha1.Action_ACTION_CREATE,
				RequiresReplace: true,
				ReplaceFields:   []string{"spec.forProvider.region"},
				Changes: []*diffv1alpha1.FieldChange{
					{Field: "spec.forProvider.description", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Planned: str("new key")},
					{Field: "spec.forProvider.region", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Planned: str("eu-central-1"), RequiresReplace: true},
				},
			},
		},
	}

	s := &PlanService{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.planResponse(tc.d, tc.exists, tc.declared)
			if err != nil {
				t.Fatalf("\n%s\nplanResponse(...): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, got, protocmp.Transform(), ignoreComputedAt); diff != "" {
				t.Errorf("\n%s\nplanResponse(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

// TestPlanResponseIsDeterministic guards the sort: with map iteration being
// randomized, an unsorted conversion passes a single run often enough to slip
// through.
func TestPlanResponseIsDeterministic(t *testing.T) {
	d := &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
		"zone":                    {Old: "a", New: "b", RequiresNew: true},
		"alpha":                   {Old: "a", New: "b"},
		"region":                  {Old: "a", New: "b", RequiresNew: true},
		"description":             {Old: "a", New: "b"},
		"deletion_window_in_days": {Old: "7", New: "30"},
		"enable_key_rotation":     {Old: "false", New: "true"},
	}}

	s := &PlanService{}
	first, err := s.planResponse(d, true, nil)
	if err != nil {
		t.Fatalf("planResponse(...): unexpected error: %v", err)
	}
	for i := 0; i < 50; i++ {
		got, err := s.planResponse(d, true, nil)
		if err != nil {
			t.Fatalf("planResponse(...): unexpected error on run %d: %v", i, err)
		}
		if diff := cmp.Diff(first, got, protocmp.Transform(), ignoreComputedAt); diff != "" {
			t.Fatalf("planResponse(...) is not deterministic; run %d differs:\n%s", i, diff)
		}
	}
}

func TestPlanResponseSetsComputedAt(t *testing.T) {
	s := &PlanService{}
	r, err := s.planResponse(nil, true, nil)
	if err != nil {
		t.Fatalf("planResponse(...): unexpected error: %v", err)
	}
	if r.GetComputedAt() == nil {
		t.Error("planResponse(...): want a computed_at timestamp, got none")
	}
}

func TestPlanResponseOrigin(t *testing.T) {
	cases := map[string]struct {
		reason   string
		d        *tf.InstanceDiff
		declared map[string]any
		want     []*diffv1alpha1.FieldChange
	}{
		"DeclaredInForProvider": {
			reason: "A field the desired resource declares follows from what the user wrote.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"display_name": {Old: "name1", New: "name2"},
			}},
			declared: map[string]any{"display_name": "name2"},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.displayName",
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				Actual:  str("name1"),
				Planned: str("name2"),
			}},
		},
		"DeclaredInInitProvider": {
			reason: "spec.initProvider is user-specified, and GetMergedParameters folds it into the declared parameters, so it is not provider-injected.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"display_name": {Old: "", New: "name1"},
			}},
			declared: map[string]any{"display_name": "name1"},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.displayName",
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				Planned: str("name1"),
			}},
		},
		"ProviderInjectedDefault": {
			reason: "A value the provider plans for a field the desired resource never declares, such as a schema default, is provider-injected.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"sign_in_audience": {Old: "", New: "AzureADMyOrg"},
			}},
			declared: map[string]any{"display_name": "name2"},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.signInAudience",
				Origin:  diffv1alpha1.Origin_ORIGIN_PROVIDER,
				Planned: str("AzureADMyOrg"),
			}},
		},
		"NothingDeclared": {
			reason: "With no declared parameters at all every planned value is provider-injected.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"sign_in_audience": {Old: "", New: "AzureADMyOrg"},
			}},
			declared: nil,
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.signInAudience",
				Origin:  diffv1alpha1.Origin_ORIGIN_PROVIDER,
				Planned: str("AzureADMyOrg"),
			}},
		},
		"NestedFieldUnderDeclaredBlock": {
			reason: "Only the leading segment is looked up, so a leaf the provider defaulted inside a declared block reports ORIGIN_DESIRED_STATE. Revisit this expectation if the lookup ever walks the schema.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"logging_config.0.target_bucket": {Old: "", New: "defaulted"},
			}},
			declared: map[string]any{"logging_config": []any{map[string]any{}}},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.loggingConfig.0.target_bucket",
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				Planned: str("defaulted"),
			}},
		},
		"NestedFieldUnderUndeclaredBlock": {
			reason: "A leaf inside a block the desired resource does not declare is provider-injected.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"logging_config.0.target_bucket": {Old: "", New: "defaulted"},
			}},
			declared: map[string]any{"display_name": "name2"},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.loggingConfig.0.target_bucket",
				Origin:  diffv1alpha1.Origin_ORIGIN_PROVIDER,
				Planned: str("defaulted"),
			}},
		},
	}

	s := &PlanService{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.planResponse(tc.d, true, tc.declared)
			if err != nil {
				t.Fatalf("\n%s\nplanResponse(...): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, got.GetChanges(), protocmp.Transform()); diff != "" {
				t.Errorf("\n%s\nplanResponse(...): -want changes, +got changes:\n%s", tc.reason, diff)
			}
		})
	}
}
