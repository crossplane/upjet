// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/crossplane/upjet/v2/pkg/config"
	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

// fwSchema covers the shapes the Framework path has to render: plain fields of
// each primitive type, an acronym, a sensitive field, a map with user supplied
// keys, a list of primitives, and a nested block.
func fwSchema() rschema.Schema {
	return rschema.Schema{
		Attributes: map[string]rschema.Attribute{
			"id":              rschema.StringAttribute{Computed: true},
			"display_name":    rschema.StringAttribute{Optional: true},
			"vpc_id":          rschema.StringAttribute{Optional: true},
			"size":            rschema.Int64Attribute{Optional: true},
			"enable_rotation": rschema.BoolAttribute{Optional: true},
			"master_password": rschema.StringAttribute{Optional: true, Sensitive: true},
			"tags":            rschema.MapAttribute{Optional: true, ElementType: types.StringType},
			"subnet_ids":      rschema.ListAttribute{Optional: true, ElementType: types.StringType},
		},
		Blocks: map[string]rschema.Block{
			"encryption_config": rschema.ListNestedBlock{
				NestedObject: rschema.NestedBlockObject{
					Attributes: map[string]rschema.Attribute{
						"kms_key_id": rschema.StringAttribute{Optional: true},
					},
				},
			},
		},
	}
}

// fwResource is the upjet resource configuration that goes with fwSchema.
func fwResource() *config.Resource {
	r := &config.Resource{SchemaElementOptions: config.SchemaElementOptions{}}
	r.SchemaElementOptions.SetEmbeddedObject("encryption_config")
	return r
}

func fwObjectType(t *testing.T) tftypes.Object {
	t.Helper()
	ty, ok := fwSchema().Type().TerraformType(context.Background()).(tftypes.Object)
	if !ok {
		t.Fatal("the test schema's Terraform type is not an object")
	}
	return ty
}

// fwObject builds a resource value, filling in every attribute the supplied
// map leaves out with a null of its own type, because a Terraform object value
// has to carry a value for each of its attributes.
func fwObject(t *testing.T, vals map[string]tftypes.Value) tftypes.Value {
	t.Helper()
	ty := fwObjectType(t)
	full := make(map[string]tftypes.Value, len(ty.AttributeTypes))
	for n, at := range ty.AttributeTypes {
		if v, ok := vals[n]; ok {
			full[n] = v
			continue
		}
		full[n] = tftypes.NewValue(at, nil)
	}
	return tftypes.NewValue(ty, full)
}

func num(f float64) *diffv1alpha1.FieldValue {
	return &diffv1alpha1.FieldValue{
		Kind: &diffv1alpha1.FieldValue_Value{Value: structpb.NewNumberValue(f)},
	}
}

func boolean(b bool) *diffv1alpha1.FieldValue {
	return &diffv1alpha1.FieldValue{
		Kind: &diffv1alpha1.FieldValue_Value{Value: structpb.NewBoolValue(b)},
	}
}

func TestFrameworkFieldPath(t *testing.T) {
	cases := map[string]struct {
		reason string
		path   *tftypes.AttributePath
		want   string
	}{
		"PlainField": {
			reason: "A field name is lower camel cased the way the CRD serializes it.",
			path:   tftypes.NewAttributePath().WithAttributeName("display_name"),
			want:   "spec.forProvider.displayName",
		},
		"Acronym": {
			reason: "An acronym is spelled the way the manifest has it, not the way the Go field does.",
			path:   tftypes.NewAttributePath().WithAttributeName("vpc_id"),
			want:   "spec.forProvider.vpcId",
		},
		"MapKey": {
			reason: "A user supplied map key is carried through untouched, never camel cased.",
			path:   tftypes.NewAttributePath().WithAttributeName("tags").WithElementKeyString("Team_Name"),
			want:   "spec.forProvider.tags.Team_Name",
		},
		"ListIndex": {
			reason: "A list element keeps its position.",
			path:   tftypes.NewAttributePath().WithAttributeName("subnet_ids").WithElementKeyInt(2),
			want:   "spec.forProvider.subnetIds[2]",
		},
		"EmbeddedObject": {
			reason: "A singleton list the CRD models as an embedded object has no index to address.",
			path: tftypes.NewAttributePath().WithAttributeName("encryption_config").
				WithElementKeyInt(0).WithAttributeName("kms_key_id"),
			want: "spec.forProvider.encryptionConfig.kmsKeyId",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, frameworkFieldPath(tc.path, fwResource())); diff != "" {
				t.Errorf("\n%s\nframeworkFieldPath(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestFrameworkOrigin(t *testing.T) {
	declared := map[string]any{
		"display_name": "demo",
		"tags":         map[string]any{"Team": "platform"},
		"subnet_ids":   []any{"subnet-1", "subnet-2"},
		"encryption_config": []any{
			map[string]any{"kms_key_id": "key-1"},
		},
	}

	cases := map[string]struct {
		reason string
		path   *tftypes.AttributePath
		want   diffv1alpha1.Origin
	}{
		"DeclaredField": {
			reason: "A field the desired resource declares follows from what the user wrote.",
			path:   tftypes.NewAttributePath().WithAttributeName("display_name"),
			want:   diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
		},
		"UndeclaredField": {
			reason: "A field the desired resource does not declare was planned by the provider.",
			path:   tftypes.NewAttributePath().WithAttributeName("size"),
			want:   diffv1alpha1.Origin_ORIGIN_PROVIDER,
		},
		"DeclaredMapKey": {
			reason: "A map key the user wrote is theirs.",
			path:   tftypes.NewAttributePath().WithAttributeName("tags").WithElementKeyString("Team"),
			want:   diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
		},
		"UndeclaredMapKey": {
			reason: "A key added to a map the user declared still did not come from the user.",
			path:   tftypes.NewAttributePath().WithAttributeName("tags").WithElementKeyString("crossplane-kind"),
			want:   diffv1alpha1.Origin_ORIGIN_PROVIDER,
		},
		"DeclaredListElement": {
			reason: "An element within the declared list is the user's.",
			path:   tftypes.NewAttributePath().WithAttributeName("subnet_ids").WithElementKeyInt(1),
			want:   diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
		},
		"ListIndexBeyondDeclared": {
			reason: "An element past the end of the declared list was not written by the user.",
			path:   tftypes.NewAttributePath().WithAttributeName("subnet_ids").WithElementKeyInt(7),
			want:   diffv1alpha1.Origin_ORIGIN_PROVIDER,
		},
		"NestedDeclaredField": {
			reason: "A field inside a declared block element is the user's.",
			path: tftypes.NewAttributePath().WithAttributeName("encryption_config").
				WithElementKeyInt(0).WithAttributeName("kms_key_id"),
			want: diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, frameworkOrigin(tc.path, declared)); diff != "" {
				t.Errorf("\n%s\nframeworkOrigin(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestFrameworkValue(t *testing.T) {
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	null := tftypes.NewValue(tftypes.String, nil)
	s := tftypes.NewValue(tftypes.String, "demo")
	n := tftypes.NewValue(tftypes.Number, 30)
	b := tftypes.NewValue(tftypes.Bool, true)

	cases := map[string]struct {
		reason string
		v      *tftypes.Value
		want   *diffv1alpha1.FieldValue
	}{
		"Nil": {
			reason: "A side the difference does not carry has no value at all.",
			v:      nil,
			want:   nil,
		},
		"Null": {
			reason: "A null value means the field does not exist on this side.",
			v:      &null,
			want:   nil,
		},
		"Unknown": {
			reason: "A value settled only once the provider applies the change is its own state.",
			v:      &unknown,
			want:   absent(diffv1alpha1.Absence_ABSENCE_UNKNOWN),
		},
		"String": {
			reason: "A string travels as a string.",
			v:      &s,
			want:   str("demo"),
		},
		"Number": {
			reason: "Unlike the plugin SDKv2 path, a number keeps its type rather than being stringified.",
			v:      &n,
			want:   num(30),
		},
		"Bool": {
			reason: "A boolean keeps its type too, so false stays distinguishable from unset.",
			v:      &b,
			want:   boolean(true),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := frameworkValue(tc.v)
			if err != nil {
				t.Fatalf("\n%s\nframeworkValue(...): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("\n%s\nframeworkValue(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestFrameworkGoValueCollections(t *testing.T) {
	list := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
		tftypes.NewValue(tftypes.String, "a"),
		tftypes.NewValue(tftypes.String, "b"),
	})
	m := tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
		"Team": tftypes.NewValue(tftypes.String, "platform"),
	})

	cases := map[string]struct {
		reason string
		v      tftypes.Value
		want   any
	}{
		"List": {
			reason: "A list converts to a slice, element by element.",
			v:      list,
			want:   []any{"a", "b"},
		},
		"Map": {
			reason: "A map converts to a map, keeping its user supplied keys.",
			v:      m,
			want:   map[string]any{"Team": "platform"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := frameworkGoValue(tc.v)
			if err != nil {
				t.Fatalf("\n%s\nframeworkGoValue(...): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nframeworkGoValue(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestForcesReplacement(t *testing.T) {
	replace := []*tftypes.AttributePath{
		tftypes.NewAttributePath().WithAttributeName("vpc_id"),
		tftypes.NewAttributePath().WithAttributeName("encryption_config"),
	}

	cases := map[string]struct {
		reason string
		path   *tftypes.AttributePath
		want   bool
	}{
		"Exact": {
			reason: "A path the plan names forces a replacement.",
			path:   tftypes.NewAttributePath().WithAttributeName("vpc_id"),
			want:   true,
		},
		"Beneath": {
			reason: "A whole block forcing a replacement covers the attributes inside it.",
			path: tftypes.NewAttributePath().WithAttributeName("encryption_config").
				WithElementKeyInt(0).WithAttributeName("kms_key_id"),
			want: true,
		},
		"Unrelated": {
			reason: "A path the plan does not name does not force a replacement.",
			path:   tftypes.NewAttributePath().WithAttributeName("display_name"),
			want:   false,
		},
		"PrefixOfAName": {
			reason: "A name that merely starts with a replacing path's name is a different field.",
			path:   tftypes.NewAttributePath().WithAttributeName("vpc_id_extra"),
			want:   false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, forcesReplacement(tc.path, replace)); diff != "" {
				t.Errorf("\n%s\nforcesReplacement(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestFrameworkPlanResponseAction(t *testing.T) {
	ctx := context.Background()
	empty := fwObject(t, nil)
	named := fwObject(t, map[string]tftypes.Value{
		"display_name": tftypes.NewValue(tftypes.String, "demo"),
	})
	renamed := fwObject(t, map[string]tftypes.Value{
		"display_name": tftypes.NewValue(tftypes.String, "other"),
	})
	replaced := fwObject(t, map[string]tftypes.Value{
		"vpc_id": tftypes.NewValue(tftypes.String, "vpc-2"),
	})
	priorVPC := fwObject(t, map[string]tftypes.Value{
		"vpc_id": tftypes.NewValue(tftypes.String, "vpc-1"),
	})
	vpcReplace := []*tftypes.AttributePath{tftypes.NewAttributePath().WithAttributeName("vpc_id")}

	cases := map[string]struct {
		reason          string
		prior, planned  tftypes.Value
		requiresReplace []*tftypes.AttributePath
		exists          bool
		want            diffv1alpha1.Action
	}{
		"Absent": {
			reason:  "A resource that does not exist yet is created, whatever the plan carries.",
			prior:   tftypes.NewValue(fwObjectType(t), nil),
			planned: named,
			exists:  false,
			want:    diffv1alpha1.Action_ACTION_CREATE,
		},
		"NoChange": {
			reason:  "An existing resource whose planned state matches the prior one is up to date.",
			prior:   named,
			planned: named,
			exists:  true,
			want:    diffv1alpha1.Action_ACTION_NO_OP,
		},
		"InPlaceChange": {
			reason:  "An existing resource with a change that does not force a replacement is updated.",
			prior:   named,
			planned: renamed,
			exists:  true,
			want:    diffv1alpha1.Action_ACTION_UPDATE,
		},
		"AddedField": {
			reason:  "Setting a field that had no value is an update.",
			prior:   empty,
			planned: named,
			exists:  true,
			want:    diffv1alpha1.Action_ACTION_UPDATE,
		},
		"ForcedReplacement": {
			reason:          "An existing resource whose change cannot be applied in place is replaced.",
			prior:           priorVPC,
			planned:         replaced,
			requiresReplace: vpcReplace,
			exists:          true,
			want:            diffv1alpha1.Action_ACTION_REPLACE,
		},
		"ForcedReplacementOnAbsentResource": {
			reason:          "A resource that does not exist yet is created, never replaced.",
			prior:           tftypes.NewValue(fwObjectType(t), nil),
			planned:         replaced,
			requiresReplace: vpcReplace,
			exists:          false,
			want:            diffv1alpha1.Action_ACTION_CREATE,
		},
	}

	s := &PlanService{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := s.frameworkPlanResponse(ctx, fwSchema(), fwResource(), tc.prior, tc.planned, tc.requiresReplace, tc.exists, nil)
			if err != nil {
				t.Fatalf("\n%s\nframeworkPlanResponse(...): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, r.GetAction()); diff != "" {
				t.Errorf("\n%s\nframeworkPlanResponse(...): -want action, +got action:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestFrameworkPlanResponseChanges(t *testing.T) {
	ctx := context.Background()

	cases := map[string]struct {
		reason          string
		prior, planned  tftypes.Value
		requiresReplace []*tftypes.AttributePath
		exists          bool
		declared        map[string]any
		want            []*diffv1alpha1.FieldChange
	}{
		"TypedValues": {
			reason: "A number and a boolean keep their types on this path, because the Framework plan has not stringified them.",
			prior: fwObject(t, map[string]tftypes.Value{
				"size":            tftypes.NewValue(tftypes.Number, 10),
				"enable_rotation": tftypes.NewValue(tftypes.Bool, false),
			}),
			planned: fwObject(t, map[string]tftypes.Value{
				"size":            tftypes.NewValue(tftypes.Number, 20),
				"enable_rotation": tftypes.NewValue(tftypes.Bool, true),
			}),
			exists:   true,
			declared: map[string]any{"size": float64(20), "enable_rotation": true},
			want: []*diffv1alpha1.FieldChange{
				{
					Field:   "spec.forProvider.enableRotation",
					Actual:  boolean(false),
					Planned: boolean(true),
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				},
				{
					Field:   "spec.forProvider.size",
					Actual:  num(10),
					Planned: num(20),
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				},
			},
		},
		"SensitiveRedacted": {
			reason: "A sensitive field is reported as changed without disclosing either value.",
			prior: fwObject(t, map[string]tftypes.Value{
				"master_password": tftypes.NewValue(tftypes.String, "old"),
			}),
			planned: fwObject(t, map[string]tftypes.Value{
				"master_password": tftypes.NewValue(tftypes.String, "new"),
			}),
			exists:   true,
			declared: map[string]any{"master_password": "new"},
			want: []*diffv1alpha1.FieldChange{
				{
					Field:   "spec.forProvider.masterPassword",
					Actual:  absent(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
					Planned: absent(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				},
			},
		},
		"UnknownDropped": {
			reason: "A value the provider recomputes on every plan is not a change the user made, so it is not reported.",
			prior: fwObject(t, map[string]tftypes.Value{
				"id": tftypes.NewValue(tftypes.String, "i-1"),
			}),
			planned: fwObject(t, map[string]tftypes.Value{
				"id": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			}),
			exists: true,
			want:   nil,
		},
		"UnknownKeptWhenItForcesReplacement": {
			reason:  "A forced replacement is never dropped, even when the value that forces it is only known after apply.",
			prior:   fwObject(t, map[string]tftypes.Value{"vpc_id": tftypes.NewValue(tftypes.String, "vpc-1")}),
			planned: fwObject(t, map[string]tftypes.Value{"vpc_id": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)}),
			requiresReplace: []*tftypes.AttributePath{
				tftypes.NewAttributePath().WithAttributeName("vpc_id"),
			},
			exists: true,
			want: []*diffv1alpha1.FieldChange{
				{
					Field:           "spec.forProvider.vpcId",
					Actual:          str("vpc-1"),
					Planned:         absent(diffv1alpha1.Absence_ABSENCE_UNKNOWN),
					RequiresReplace: true,
					Origin:          diffv1alpha1.Origin_ORIGIN_PROVIDER,
				},
			},
		},
		"CreateReportsOnlyTheFieldsThatHaveValues": {
			reason: "Against a null prior state Diff reports the resource itself and every attribute, including the ones nobody set. Only the fields that actually have a value belong in the plan, and the resource as a whole never does.",
			prior:  tftypes.NewValue(fwObjectType(t), nil),
			planned: fwObject(t, map[string]tftypes.Value{
				"display_name": tftypes.NewValue(tftypes.String, "demo"),
				"size":         tftypes.NewValue(tftypes.Number, 20),
			}),
			exists:   false,
			declared: map[string]any{"display_name": "demo", "size": float64(20)},
			want: []*diffv1alpha1.FieldChange{
				{
					Field:   "spec.forProvider.displayName",
					Planned: str("demo"),
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				},
				{
					Field:   "spec.forProvider.size",
					Planned: num(20),
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				},
			},
		},
		"SensitiveUnsetOnBothSidesIsNotAChange": {
			reason: "A sensitive field nobody set must not be reported, because redaction would hide that both sides are empty and the plan would claim a password may have changed on every create.",
			prior:  tftypes.NewValue(fwObjectType(t), nil),
			planned: fwObject(t, map[string]tftypes.Value{
				"display_name": tftypes.NewValue(tftypes.String, "demo"),
			}),
			exists:   false,
			declared: map[string]any{"display_name": "demo"},
			want: []*diffv1alpha1.FieldChange{
				{
					Field:   "spec.forProvider.displayName",
					Planned: str("demo"),
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				},
			},
		},
		"OnlyTheLeafOfANestedChange": {
			reason: "Diff reports a collection as well as the element inside it that differs. The element says where the change is, so the collection must not repeat it.",
			prior: fwObject(t, map[string]tftypes.Value{
				"subnet_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
					tftypes.NewValue(tftypes.String, "subnet-1"),
				}),
			}),
			planned: fwObject(t, map[string]tftypes.Value{
				"subnet_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
					tftypes.NewValue(tftypes.String, "subnet-1"),
					tftypes.NewValue(tftypes.String, "subnet-2"),
				}),
			}),
			exists:   true,
			declared: map[string]any{"subnet_ids": []any{"subnet-1", "subnet-2"}},
			want: []*diffv1alpha1.FieldChange{
				{
					Field:   "spec.forProvider.subnetIds[1]",
					Planned: str("subnet-2"),
					Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
				},
			},
		},
		"EmptyCollectionAgainstNullIsNotAChange": {
			reason: "A provider that computes an empty collection where the recorded state has none has not changed anything. Reporting it would stop an untagged resource from ever planning as a no-op.",
			prior:  fwObject(t, nil),
			planned: fwObject(t, map[string]tftypes.Value{
				"tags":       tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{}),
				"subnet_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{}),
			}),
			exists: true,
			want:   nil,
		},
		"RemovedField": {
			reason: "Removing a field leaves the change with no planned value.",
			prior: fwObject(t, map[string]tftypes.Value{
				"display_name": tftypes.NewValue(tftypes.String, "demo"),
			}),
			planned: fwObject(t, nil),
			exists:  true,
			want: []*diffv1alpha1.FieldChange{
				{
					Field:  "spec.forProvider.displayName",
					Actual: str("demo"),
					Origin: diffv1alpha1.Origin_ORIGIN_PROVIDER,
				},
			},
		},
	}

	s := &PlanService{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := s.frameworkPlanResponse(ctx, fwSchema(), fwResource(), tc.prior, tc.planned, tc.requiresReplace, tc.exists, tc.declared)
			if err != nil {
				t.Fatalf("\n%s\nframeworkPlanResponse(...): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, r.GetChanges(), protocmp.Transform()); diff != "" {
				t.Errorf("\n%s\nframeworkPlanResponse(...): -want changes, +got changes:\n%s", tc.reason, diff)
			}
		})
	}
}

// the schema helpers must stay in step with the attr package's types, which is
// what this reference keeps honest.
var _ attr.Type = types.StringType
