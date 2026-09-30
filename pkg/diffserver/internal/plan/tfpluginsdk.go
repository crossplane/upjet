// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
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
	errConvertDesiredParameters  = "cannot convert the parameters of the desired resource to their Terraform shape"

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

	dtr, ok := desired.(resource.Terraformed)
	if !ok {
		return nil, errors.Errorf(fmtErrNotTerraformed, desired.GetObjectKind().GroupVersionKind().String())
	}

	if actual == nil {
		opTracker.Tracker(dtr).ResetReconstructedTfState()
	} else {
		tr, ok := actual.(resource.Terraformed)
		if !ok {
			return nil, errors.Errorf(fmtErrNotTerraformed, actual.GetObjectKind().GroupVersionKind().String())
		}
		opTracker.Tracker(tr).ResetReconstructedTfState()
		if _, _, _, err := c.ReconstructTerraformState(ctx, tr, s.log); err != nil {
			return nil, errors.Wrap(err, errReconstructTerraformState)
		}
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

	declared, err := dtr.GetMergedParameters(true)
	if err != nil {
		return nil, errors.Wrap(err, errGetDesiredParameters)
	}
	// The flatmap keys are in Terraform shape, so the parameters must be too:
	// this turns the CRD's embedded objects back into singleton lists.
	declared, err = cfg.ApplyTFConversions(declared, config.ToTerraform)
	if err != nil {
		return nil, errors.Wrap(err, errConvertDesiredParameters)
	}
	return s.planResponse(diff, obs.ResourceExists, declared, cfg)
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
func (s *PlanService) planResponse(d *tf.InstanceDiff, exists bool, declared map[string]any, cfg *config.Resource) (*diffv1alpha1.PlanResponse, error) {
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
		c, err := fieldChange(k, a, declared, exists, cfg)
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
// into a field change. exists reports whether the external resource already
// exists, because forcing a replacement is only meaningful for one that does.
func fieldChange(key string, a *tf.ResourceAttrDiff, declared map[string]any, exists bool, cfg *config.Resource) (*diffv1alpha1.FieldChange, error) {
	c := &diffv1alpha1.FieldChange{
		Field:           fieldPath(key, cfg),
		RequiresReplace: exists && a.RequiresNew,
		Origin:          origin(key, declared, cfg),
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

// stepKind distinguishes what a resolved segment of a flatmap key selects.
type stepKind int

const (
	// stepField selects a field declared by the resource schema.
	stepField stepKind = iota
	// stepIndex selects an element of a list or a set.
	stepIndex
	// stepMapKey selects an entry of a map by a user supplied key.
	stepMapKey
	// stepUnresolved is a segment the schema could not account for. It is
	// carried through verbatim so that nothing is silently dropped.
	stepUnresolved
)

// step is one resolved segment of a Terraform flatmap key.
type step struct {
	kind stepKind
	// tf is the segment as it appears in the flatmap key.
	tf string
	// crd is how the CRD spells the segment. It is empty for an index that
	// the CRD does not have, which is the case for a singleton list the
	// resource converts into an embedded object.
	crd string
}

// walkKey resolves a Terraform flatmap key against the resource schema.
//
// Resolution is what makes the difference between a field name, which the CRD
// camel cases, and a user supplied map key or a list index, which it must not
// touch: "logging_config.0.target_bucket" and "tags.Team" are indistinguishable
// without the schema. Once a segment cannot be resolved the remainder is
// carried through verbatim rather than guessed at.
func walkKey(key string, cfg *config.Resource) []step { //nolint:gocyclo // the flatmap cases are easier to follow as one walk
	segments := strings.Split(key, ".")
	steps := make([]step, 0, len(segments))

	var schemas map[string]*schema.Schema
	if cfg.TerraformResource != nil {
		schemas = cfg.TerraformResource.Schema
	}
	// tfPath accumulates the field names seen so far, which is the key
	// SchemaElementOptions uses to record singleton list conversions.
	var tfPath []string

	for i := 0; i < len(segments); i++ {
		sch := schemas[segments[i]]
		if sch == nil {
			for _, r := range segments[i:] {
				steps = append(steps, step{kind: stepUnresolved, tf: r, crd: r})
			}
			return steps
		}
		tfPath = append(tfPath, segments[i])
		steps = append(steps, step{
			kind: stepField,
			tf:   segments[i],
			crd:  name.NewFromSnake(segments[i]).LowerCamelComputed,
		})
		if i+1 == len(segments) {
			return steps
		}

		switch sch.Type { //nolint:exhaustive // only the collection types continue the walk
		case schema.TypeMap:
			// Everything left is the map key, which may itself contain dots.
			k := strings.Join(segments[i+1:], ".")
			return append(steps, step{kind: stepMapKey, tf: k, crd: k})

		case schema.TypeList, schema.TypeSet:
			i++
			crd := segments[i]
			if cfg.SchemaElementOptions.EmbeddedObject(strings.Join(tfPath, ".")) {
				// The CRD models this singleton list as an embedded object,
				// so it has no index to address.
				crd = ""
			}
			steps = append(steps, step{kind: stepIndex, tf: segments[i], crd: crd})

			elem, ok := sch.Elem.(*schema.Resource)
			if !ok {
				// A collection of primitives ends the walk at its element.
				for _, r := range segments[i+1:] {
					steps = append(steps, step{kind: stepUnresolved, tf: r, crd: r})
				}
				return steps
			}
			schemas = elem.Schema

		default:
			for _, r := range segments[i+1:] {
				steps = append(steps, step{kind: stepUnresolved, tf: r, crd: r})
			}
			return steps
		}
	}
	return steps
}

// fieldPath maps a Terraform flatmap key onto the CRD path of the field it
// came from, e.g. "logging_config.0.target_bucket" to
// "spec.forProvider.loggingConfig[0].targetBucket".
//
// A segment is lower camel cased the way the generated CRD serializes it,
// which is Name.LowerCamelComputed rather than Name.LowerCamel: the latter
// spells acronyms the way the Go field does, so "template_id" would become
// "templateID" where the manifest has "templateId".
//
// The index of a set element is Terraform's hash of that element rather than
// a position, so the path it produces locates the field but not the element.
func fieldPath(key string, cfg *config.Resource) string {
	p := crdParametersPath
	for _, s := range walkKey(key, cfg) {
		switch {
		case s.kind == stepIndex && s.crd == "":
			// A singleton list the CRD models as an embedded object.
		case s.kind == stepIndex:
			p += "[" + s.crd + "]"
		default:
			p += "." + s.crd
		}
	}
	return p
}

// isDeclared reports whether the desired resource declares the attribute the
// flatmap key addresses, by walking the declared parameters alongside the
// schema. The parameters are in Terraform shape, so a singleton list is still
// a list here even where the CRD models it as an embedded object.
func isDeclared(key string, declared map[string]any, cfg *config.Resource) bool {
	var current any = declared
	for _, s := range walkKey(key, cfg) {
		switch s.kind {
		case stepField, stepMapKey, stepUnresolved:
			m, ok := current.(map[string]any)
			if !ok {
				return false
			}
			if current, ok = m[s.tf]; !ok {
				return false
			}
		case stepIndex:
			l, ok := current.([]any)
			if !ok {
				return false
			}
			i, err := strconv.Atoi(s.tf)
			if err != nil || i < 0 || i >= len(l) {
				// A set element is addressed by its hash rather than by a
				// position, so it cannot be located in the declared list. The
				// collection itself was declared, which is as much as can be
				// established here.
				return true
			}
			current = l[i]
		}
	}
	return true
}

// origin reports where the planned value of the attribute came from.
func origin(key string, declared map[string]any, cfg *config.Resource) diffv1alpha1.Origin {
	if isDeclared(key, declared, cfg) {
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
