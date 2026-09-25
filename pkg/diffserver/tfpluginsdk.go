// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"context"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/types/name"
	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

const (
	errReconstructTerraformState = "cannot reconstruct Terraform resource state"
	errConnectPluginSDKv2        = "cannot connect for Terraform plugin SDKv2 resource"
	errObservePluginSDKv2        = "cannot observe Terraform plugin SDKv2 resource"
	errGetInstanceDiff           = "cannot read Terraform plugin SDKv2 resource instance diff"
	errDiffPluginSDKv2           = "cannot compute diff for a Terraform plugin SDKv2 resource"
	errConvertValue              = "cannot convert the attribute value to a protobuf value"
	errGetDesiredParameters      = "cannot get the parameters of the desired resource"

	fmtErrConvertAttribute = "cannot convert the diff of the attribute %q"

	// crdParametersPath is the path, in a managed resource's manifest, under
	// which the Terraform resource's arguments appear.
	crdParametersPath = "spec.forProvider"
)

func (s *PlanService) diffTerraformPluginSDK(ctx context.Context, kc kclient.Client, cfg *config.Resource, desired, actual xpresource.Managed) (*diffv1alpha1.PlanResponse, error) {
	opTracker := controller.NewOperationStore(s.log)
	c := controller.NewTerraformPluginSDKConnector(
		kc, s.setupFn, cfg, opTracker,
		controller.WithTerraformPluginSDKLogger(s.log),
		controller.WithObservationMode(controller.UseLocalState),
		controller.WithTerraformPluginSDKManagementPolicies(true),
	)
	ec, err := c.Connect(ctx, desired)
	if err != nil {
		return nil, errors.Wrap(err, errConnectPluginSDKv2)
	}

	tr, ok := actual.(resource.Terraformed)
	if !ok {
		return nil, errors.Errorf(fmtErrNotTerraformed, actual.GetObjectKind().GroupVersionKind().String())
	}
	opTracker.Tracker(tr).ResetReconstructedTfState()
	if _, _, _, err := c.ReconstructTerraformState(ctx, tr, s.log); err != nil {
		return nil, errors.Wrap(err, errReconstructTerraformState)
	}

	obs, err := ec.Observe(ctx, desired)
	if err != nil {
		return nil, errors.Wrap(err, errObservePluginSDKv2)
	}

	diff, err := controller.TerraformPluginSDKInstanceDiff(ec)
	if err != nil {
		return nil, errors.Wrap(err, errGetInstanceDiff)
	}
	filterInstanceDiff(diff)

	dtr, ok := desired.(resource.Terraformed)
	if !ok {
		return nil, errors.Errorf(fmtErrNotTerraformed, desired.GetObjectKind().GroupVersionKind().String())
	}
	declared, err := dtr.GetMergedParameters(true)
	if err != nil {
		return nil, errors.Wrap(err, errGetDesiredParameters)
	}
	return s.planResponse(diff, obs.ResourceExists, declared)
}

// planResponse converts a filtered Terraform instance diff into a plan
// response. exists reports whether the external resource already exists; the
// diff alone does not carry that, and it is what distinguishes a create from
// an update.
//
// The attribute diffs are Terraform's flatmap representation, so every value
// is a string. They are reported as strings here rather than being re-typed
// against the resource schema, which means a number reads as "30" and a
// boolean as "true". Clients should not infer a type from the JSON shape of
// a plugin SDKv2 plan.
func (s *PlanService) planResponse(d *tf.InstanceDiff, exists bool, declared map[string]any) (*diffv1alpha1.PlanResponse, error) {
	r := &diffv1alpha1.PlanResponse{
		Action:     diffv1alpha1.Action_ACTION_NO_OP,
		ComputedAt: timestamppb.Now(),
	}

	var requiresReplace bool

	// A nil diff carries no attributes. Ranging over the nil map below is
	// safe, and InstanceDiff.Empty reports true for a nil receiver.
	var attributes map[string]*tf.ResourceAttrDiff
	if d != nil {
		attributes = d.Attributes
	}

	for k, a := range attributes {
		if a == nil {
			continue
		}
		// Every attribute contributes to the replacement decision, including
		// the ones that are not reported below, so that a forced replacement
		// is never dropped along with the attribute that forces it.
		requiresReplace = requiresReplace || a.RequiresNew

		if isCountKey(k) {
			// Count keys, such as "tags.%" and "subnet_ids.#", carry the size
			// of a collection rather than a field the user wrote. The element
			// changes that accompany them are reported on their own.
			continue
		}
		c, err := fieldChange(k, a, declared)
		if err != nil {
			return nil, errors.Wrapf(err, fmtErrConvertAttribute, k)
		}
		r.Changes = append(r.GetChanges(), c)
	}

	// Map iteration is unordered, so sort to keep a plan stable across calls.
	// The slice is sorted in place, through the same backing array the
	// response holds.
	changes := r.GetChanges()
	sort.Slice(changes, func(i, j int) bool { return changes[i].GetField() < changes[j].GetField() })

	switch {
	case !exists:
		// The external resource is not there yet, so the plan creates it
		// whether or not the diff carries attribute changes.
		r.Action = diffv1alpha1.Action_ACTION_CREATE
	case d.Empty():
		r.Action = diffv1alpha1.Action_ACTION_NO_OP
	case requiresReplace:
		r.Action = diffv1alpha1.Action_ACTION_REPLACE
	default:
		r.Action = diffv1alpha1.Action_ACTION_UPDATE
	}
	return r, nil
}

// fieldChange converts a single attribute diff, keyed by its flatmap key,
// into a field change.
func fieldChange(key string, a *tf.ResourceAttrDiff, declared map[string]any) (*diffv1alpha1.FieldChange, error) {
	c := &diffv1alpha1.FieldChange{
		Field:           fieldPath(key),
		RequiresReplace: a.RequiresNew,
		Origin:          origin(key, declared),
	}

	// A sensitive attribute is redacted on both sides: the plan reports that
	// the field may have changed without disclosing either value.
	if a.Sensitive {
		c.Actual = absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE)
		c.Planned = absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE)
		return c, nil
	}

	// Flatmap cannot distinguish an absent attribute from one set to the
	// empty string, so an empty Old is reported as no value on the current
	// side. This is the common case: on a create every attribute has one.
	if a.Old != "" {
		v, err := concreteValue(a.Old)
		if err != nil {
			return nil, err
		}
		c.Actual = v
	}

	switch {
	case a.NewComputed:
		// The value is only settled once the provider applies the change.
		c.Planned = absentValue(diffv1alpha1.Absence_ABSENCE_UNKNOWN)
	case a.NewRemoved:
		// The change removes the attribute, so there is no desired value.
	default:
		v, err := concreteValue(a.New)
		if err != nil {
			return nil, err
		}
		c.Planned = v
	}
	return c, nil
}

// fieldPath maps a Terraform flatmap key onto the CRD path of the field it
// came from, e.g. "deletion_window_in_days" to
// "spec.forProvider.deletionWindowInDays".
//
// Only the leading segment is translated. A flatmap key's later segments are
// ambiguous without the resource schema: "logging_config.0.target_bucket" has
// a nested field name that should be camel cased, whereas "tags.Team" has a
// user-supplied map key that must be left exactly as it is. Translating both
// would corrupt map keys, so the remainder is reported in its Terraform form
// until this walks the schema.
func fieldPath(key string) string {
	head, rest, found := strings.Cut(key, ".")
	p := crdParametersPath + "." + name.NewFromSnake(head).LowerCamel
	if found {
		p += "." + rest
	}
	return p
}

// origin reports where the planned value of the attribute came from, by
// looking for its attribute in the parameters the desired resource declares.
//
// Only the leading segment is looked up, for the same reason fieldPath only
// translates that one: resolving a nested segment needs the resource schema.
// A leaf that a provider defaulted inside a block the desired resource does
// declare therefore reports ORIGIN_DESIRED_STATE.
func origin(key string, declared map[string]any) diffv1alpha1.Origin {
	head, _, _ := strings.Cut(key, ".")
	if _, ok := declared[head]; ok {
		return diffv1alpha1.Origin_ORIGIN_DESIRED_STATE
	}
	return diffv1alpha1.Origin_ORIGIN_PROVIDER
}

// isCountKey reports whether the flatmap key addresses the size of a
// collection rather than one of its elements: "%" for maps, "#" for lists
// and sets.
func isCountKey(key string) bool {
	return strings.HasSuffix(key, ".%") || strings.HasSuffix(key, ".#")
}

func concreteValue(s string) (*diffv1alpha1.FieldValue, error) {
	v, err := structpb.NewValue(s)
	if err != nil {
		return nil, errors.Wrap(err, errConvertValue)
	}
	return &diffv1alpha1.FieldValue{Kind: &diffv1alpha1.FieldValue_Value{Value: v}}, nil
}

func absentValue(a diffv1alpha1.Absence) *diffv1alpha1.FieldValue {
	return &diffv1alpha1.FieldValue{Kind: &diffv1alpha1.FieldValue_Absence{Absence: a}}
}

// filterInstanceDiff removes the attribute diffs that do not represent a
// meaningful change from d, in place, so that what remains answers the
// question "did anything meaningful change?". Once filtered, d.Empty()
// reports whether the desired resource differs from the actual one.
func filterInstanceDiff(d *tf.InstanceDiff) {
	if d == nil {
		return
	}
	for k, a := range d.Attributes {
		if !isUserChange(a) {
			delete(d.Attributes, k)
		}
	}
}

// isUserChange reports whether the given attribute diff represents a
// change worth reporting in a plan.
func isUserChange(a *tf.ResourceAttrDiff) bool {
	switch {
	case a == nil:
		// An attribute without a diff carries no information.
		return false
	case a.RequiresNew:
		// Replacing the external resource is always meaningful, even when the
		// value that triggers the replacement is computed.
		return true
	case a.NewComputed:
		// The new value is unknown until apply. The Terraform provider
		// recomputes such attributes on every plan, they do not reflect
		// a change the user made. Their New is an empty placeholder,
		// so no need to compare it with Old.
		return false
	default:
		// Everything else, including an attribute being removed, is a change
		// exactly when its old and new values differ. Removing an attribute
		// that is already empty is therefore correctly reported as no change.
		return a.Old != a.New
	}
}
