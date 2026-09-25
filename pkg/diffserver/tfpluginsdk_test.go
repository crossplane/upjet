// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/crossplane/upjet/v2/pkg/config"
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

// testResource is a schema covering each shape walkKey has to resolve: plain
// fields, an acronym, a map with user supplied keys, a list of primitives, a
// list of objects, a set of objects, and a singleton list the CRD models as an
// embedded object.
func testResource() *config.Resource {
	r := &config.Resource{
		TerraformResource: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"display_name":            {Type: schema.TypeString},
				"description":             {Type: schema.TypeString},
				"alpha":                   {Type: schema.TypeString},
				"zone":                    {Type: schema.TypeString},
				"region":                  {Type: schema.TypeString},
				"policy":                  {Type: schema.TypeString},
				"master_password":         {Type: schema.TypeString, Sensitive: true},
				"sign_in_audience":        {Type: schema.TypeString},
				"deletion_window_in_days": {Type: schema.TypeInt},
				"enable_key_rotation":     {Type: schema.TypeBool},
				"template_id":             {Type: schema.TypeString},
				"vpc_id":                  {Type: schema.TypeString},
				"tags":                    {Type: schema.TypeMap, Elem: &schema.Schema{Type: schema.TypeString}},
				"subnet_ids":              {Type: schema.TypeList, Elem: &schema.Schema{Type: schema.TypeString}},
				"logging_config": {Type: schema.TypeList, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"target_bucket": {Type: schema.TypeString},
					"target_prefix": {Type: schema.TypeString},
				}}},
				"ingress_rule": {Type: schema.TypeSet, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"from_port": {Type: schema.TypeInt},
				}}},
				"encryption_config": {Type: schema.TypeList, MaxItems: 1, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"kms_key_id": {Type: schema.TypeString},
				}}},
			},
		},
		SchemaElementOptions: config.SchemaElementOptions{},
	}
	r.SchemaElementOptions.SetEmbeddedObject("encryption_config")
	return r
}

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
		"ForceNewCountKeyOnly": {
			reason: "A count key is not reported as a field change, but it must still force a replacement rather than being dropped along with the attribute.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"subnet_ids.#": {Old: "1", New: "2", RequiresNew: true},
			}},
			exists: true,
			want:   diffv1alpha1.Action_ACTION_REPLACE,
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
			r, err := s.planResponse(tc.d, tc.exists, tc.declared, testResource())
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
				Action: diffv1alpha1.Action_ACTION_REPLACE,
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
					{Field: "spec.forProvider.subnetIds[1]", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Planned: str("subnet-b")},
					{Field: "spec.forProvider.tags.Env", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Planned: str("prod")},
				},
			},
		},
		"ForceNewCountKeyForcesReplaceWithoutAField": {
			reason: "The replacement is reported through the action even though no reported change can be attributed to it, which is better than silently downgrading the plan to an update.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"subnet_ids.#": {Old: "1", New: "2", RequiresNew: true},
				"subnet_ids.1": {Old: "", New: "subnet-b"},
			}},
			exists:   true,
			declared: map[string]any{"subnet_ids": []any{"subnet-a", "subnet-b"}},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_REPLACE,
				Changes: []*diffv1alpha1.FieldChange{{
					Field:   "spec.forProvider.subnetIds[1]",
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
					Planned: str("subnet-b"),
				}},
			},
		},
		"ForceNewOnCreateDoesNotRequireReplace": {
			reason: "A force-new attribute on a resource that does not exist yet has nothing to replace, so the change must not claim it forces a replacement.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"region": {Old: "", New: "eu-central-1", RequiresNew: true},
			}},
			exists:   false,
			declared: map[string]any{"region": "eu-central-1"},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_CREATE,
				Changes: []*diffv1alpha1.FieldChange{{
					Field:   "spec.forProvider.region",
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
					Planned: str("eu-central-1"),
				}},
			},
		},
		"AcronymInFieldName": {
			reason: "The path must be spelled the way the generated CRD serializes the field, which lower cases the ID acronym, so that a client can resolve it against the manifest.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"template_id": {Old: "3f8a5c2e", New: "7d2b4f16"},
				"vpc_id":      {Old: "vpc-a", New: "vpc-b"},
			}},
			exists:   true,
			declared: map[string]any{"template_id": "7d2b4f16", "vpc_id": "vpc-b"},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_UPDATE,
				Changes: []*diffv1alpha1.FieldChange{
					{Field: "spec.forProvider.templateId", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("3f8a5c2e"), Planned: str("7d2b4f16")},
					{Field: "spec.forProvider.vpcId", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("vpc-a"), Planned: str("vpc-b")},
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
					{Field: "spec.forProvider.loggingConfig[0].targetBucket", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("old"), Planned: str("new")},
					{Field: "spec.forProvider.tags.Env", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("dev"), Planned: str("prod")},
				},
			},
		},
		"ChangesSorted": {
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
				Action: diffv1alpha1.Action_ACTION_REPLACE,
				Changes: []*diffv1alpha1.FieldChange{
					{Field: "spec.forProvider.alpha", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("a"), Planned: str("b")},
					{Field: "spec.forProvider.description", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("a"), Planned: str("b")},
					{Field: "spec.forProvider.region", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("a"), Planned: str("b"), RequiresReplace: true},
					{Field: "spec.forProvider.zone", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Actual: str("a"), Planned: str("b"), RequiresReplace: true},
				},
			},
		},
		"CreateReportsEveryAttributeAsAdded": {
			reason: "On a create the resource has no prior state, so every attribute is reported as an addition, and no attribute forces a replacement because there is nothing to replace.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"description": {Old: "", New: "new key"},
				"region":      {Old: "", New: "eu-central-1", RequiresNew: true},
			}},
			exists:   false,
			declared: map[string]any{"description": "new key", "region": "eu-central-1"},
			want: &diffv1alpha1.PlanResponse{
				Action: diffv1alpha1.Action_ACTION_CREATE,
				Changes: []*diffv1alpha1.FieldChange{
					{Field: "spec.forProvider.description", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Planned: str("new key")},
					{Field: "spec.forProvider.region", Origin: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE, Planned: str("eu-central-1")},
				},
			},
		},
	}

	s := &PlanService{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.planResponse(tc.d, tc.exists, tc.declared, testResource())
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
	first, err := s.planResponse(d, true, nil, testResource())
	if err != nil {
		t.Fatalf("planResponse(...): unexpected error: %v", err)
	}
	for i := 0; i < 50; i++ {
		got, err := s.planResponse(d, true, nil, testResource())
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
	r, err := s.planResponse(nil, true, nil, testResource())
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
		"NestedLeafDeclared": {
			reason: "A nested leaf the desired resource declares follows from what the user wrote, which needs the walk to descend into the block rather than stopping at its head.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"logging_config.0.target_bucket": {Old: "old", New: "new"},
			}},
			declared: map[string]any{"logging_config": []any{map[string]any{"target_bucket": "new"}}},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.loggingConfig[0].targetBucket",
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				Actual:  str("old"),
				Planned: str("new"),
			}},
		},
		"NestedLeafDefaultedInsideDeclaredBlock": {
			reason: "A leaf the provider defaulted inside a block the desired resource declares is still provider-injected, which only the schema walk can tell apart from a declared leaf.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"logging_config.0.target_prefix": {Old: "", New: "defaulted"},
			}},
			declared: map[string]any{"logging_config": []any{map[string]any{"target_bucket": "b"}}},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.loggingConfig[0].targetPrefix",
				Origin:  diffv1alpha1.Origin_ORIGIN_PROVIDER,
				Planned: str("defaulted"),
			}},
		},
		"EmbeddedObjectDropsTheIndex": {
			reason: "A singleton list the CRD models as an embedded object has no index to address, and the declared parameters are in Terraform shape so the list is still there to walk.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"encryption_config.0.kms_key_id": {Old: "old", New: "new"},
			}},
			declared: map[string]any{"encryption_config": []any{map[string]any{"kms_key_id": "new"}}},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.encryptionConfig.kmsKeyId",
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				Actual:  str("old"),
				Planned: str("new"),
			}},
		},
		"MapKeyLeftVerbatim": {
			reason: "A map key is user data, so it must not be camel cased however it is spelled.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"tags.Env_Name": {Old: "", New: "prod"},
			}},
			declared: map[string]any{"tags": map[string]any{"Env_Name": "prod"}},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.tags.Env_Name",
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				Planned: str("prod"),
			}},
		},
		"SetElementAddressedByHash": {
			reason: "A set element is addressed by Terraform's hash rather than by a position, so it cannot be located in the declared list and the declared collection is as much as can be established.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"ingress_rule.2857883304.from_port": {Old: "80", New: "443"},
			}},
			declared: map[string]any{"ingress_rule": []any{map[string]any{"from_port": "443"}}},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.ingressRule[2857883304].fromPort",
				Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				Actual:  str("80"),
				Planned: str("443"),
			}},
		},
		"UnknownAttributeCarriedThrough": {
			reason: "An attribute the schema does not account for is reported verbatim rather than guessed at, and cannot be attributed to the desired state.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"not_in_schema": {Old: "", New: "x"},
			}},
			declared: map[string]any{"display_name": "name2"},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.not_in_schema",
				Origin:  diffv1alpha1.Origin_ORIGIN_PROVIDER,
				Planned: str("x"),
			}},
		},
		"NestedFieldUnderUndeclaredBlock": {
			reason: "A leaf inside a block the desired resource does not declare is provider-injected.",
			d: &tf.InstanceDiff{Attributes: map[string]*tf.ResourceAttrDiff{
				"logging_config.0.target_bucket": {Old: "", New: "defaulted"},
			}},
			declared: map[string]any{"display_name": "name2"},
			want: []*diffv1alpha1.FieldChange{{
				Field:   "spec.forProvider.loggingConfig[0].targetBucket",
				Origin:  diffv1alpha1.Origin_ORIGIN_PROVIDER,
				Planned: str("defaulted"),
			}},
		},
	}

	s := &PlanService{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.planResponse(tc.d, true, tc.declared, testResource())
			if err != nil {
				t.Fatalf("\n%s\nplanResponse(...): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, got.GetChanges(), protocmp.Transform()); diff != "" {
				t.Errorf("\n%s\nplanResponse(...): -want changes, +got changes:\n%s", tc.reason, diff)
			}
		})
	}
}
