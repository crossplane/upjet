// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"context"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
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
	errConnectPluginFramework       = "cannot connect for Terraform plugin Framework resource"
	errObservePluginFramework       = "cannot observe Terraform plugin Framework resource"
	errDiffPluginFramework          = "cannot compute diff for a Terraform plugin Framework resource"
	errGetFrameworkPlan             = "cannot read the Terraform plugin Framework plan"
	errFrameworkSchema              = "cannot get the Terraform plugin Framework resource schema"
	errReconstructFrameworkState    = "cannot reconstruct the Terraform state of the actual resource"
	errMarkAbsentFramework          = "cannot empty the Terraform state for a create"
	errFrameworkStateDiff           = "cannot compare the planned and the prior Terraform state"
	errUnmarshalFrameworkPriorState = "cannot unmarshal the prior Terraform state"
	errUnmarshalFrameworkPlanned    = "cannot unmarshal the planned Terraform state"
	errNoFrameworkResource          = "no Terraform plugin Framework resource is configured"
	errNoFrameworkPlannedState      = "the Terraform plugin Framework plan carries no planned state"

	fmtErrConvertFrameworkValue = "cannot convert the value at %q"
)

func (s *PlanService) diffTerraformPluginFramework(ctx context.Context, kc kclient.Client, cfg *config.Resource, desired, actual xpresource.Managed) (*diffv1alpha1.PlanResponse, error) { //nolint:gocyclo // easier to follow as a unit, and it mirrors the plugin SDKv2 path step for step
	cfg = planConfig(cfg, desired)
	opTracker := controller.NewOperationStore(s.log)
	c := controller.NewTerraformPluginFrameworkConnector(
		kc, s.setupFn, cfg, opTracker,
		controller.WithTerraformPluginFrameworkLogger(s.log),
		controller.WithTerraformPluginFrameworkObservationMode(controller.UseLocalState),
		controller.WithTerraformPluginFrameworkManagementPolicies(true),
	)
	ec, err := c.Connect(ctx, desired)
	if err != nil {
		return nil, errors.Wrap(err, errConnectPluginFramework)
	}

	dtr, ok := desired.(resource.Terraformed)
	if !ok {
		return nil, errors.Errorf(fmtErrNotTerraformed, desired.GetObjectKind().GroupVersionKind().String())
	}
	sch, err := frameworkSchema(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resourceType := sch.Type().TerraformType(ctx)

	if actual == nil {
		// Connect reconstructed a Terraform state from the desired resource,
		// and for a resource with no observation it falls back to copying the
		// parameters into it, which would read as an external resource that
		// already exists. Null it so that the plan is a create: the provider
		// reads absence off a null state, and a null prior state makes the
		// proposed state the configuration, which is what a create plans.
		if err := markAbsentFramework(opTracker.Tracker(dtr), resourceType); err != nil {
			return nil, errors.Wrap(err, errMarkAbsentFramework)
		}
	} else {
		tr, ok := actual.(resource.Terraformed)
		if !ok {
			return nil, errors.Errorf(fmtErrNotTerraformed, actual.GetObjectKind().GroupVersionKind().String())
		}
		// The tracker is keyed by UID, and every later read of it - Observe,
		// and the prior state below - asks for desired's. Reconstructing
		// under actual's own UID would leave the state in a different slot
		// whenever the two differ, which they do for a rendered desired
		// manifest that was never applied and so carries no UID at all. A
		// copy keeps actual's own observation and annotations; only the UID
		// used to select the tracker slot changes.
		trAtDesiredKey := tr.DeepCopyObject().(resource.Terraformed)
		trAtDesiredKey.SetUID(dtr.GetUID())
		// Reconstructing the state from the actual resource's observation is
		// what Connect does, so connect again with the actual resource after
		// dropping the state built from the desired one. The client it returns
		// is discarded: only the state it leaves on the tracker is wanted,
		// while the configuration side must stay the desired resource's.
		opTracker.Tracker(trAtDesiredKey).ResetReconstructedFrameworkTFState()
		if _, err := c.Connect(ctx, trAtDesiredKey); err != nil {
			return nil, errors.Wrap(err, errReconstructFrameworkState)
		}
	}

	obs, err := ec.Observe(ctx, desired)
	if err != nil {
		return nil, errors.Wrap(err, errObservePluginFramework)
	}

	planResponse, err := controller.TerraformPluginFrameworkPlanResponse(ec)
	if err != nil {
		return nil, errors.Wrap(err, errGetFrameworkPlan)
	}
	if planResponse == nil || planResponse.PlannedState == nil {
		return nil, errors.New(errNoFrameworkPlannedState)
	}
	planned, err := planResponse.PlannedState.Unmarshal(resourceType)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalFrameworkPlanned)
	}
	prior := tftypes.NewValue(resourceType, nil)
	if st := opTracker.Tracker(dtr).GetFrameworkTFState(); st != nil {
		if prior, err = st.Unmarshal(resourceType); err != nil {
			return nil, errors.Wrap(err, errUnmarshalFrameworkPriorState)
		}
	}

	// The attribute paths the diff reports are in Terraform shape, so the
	// parameters they are compared against must be too.
	declared, unresolved, err := declaredParameters(ctx, kc, dtr, cfg)
	if err != nil {
		return nil, err
	}
	return s.frameworkPlanResponse(ctx, sch, cfg, prior, planned, planResponse.RequiresReplace, obs.ResourceExists, declared, unresolved)
}

// frameworkSchema returns the resource schema of the configured Terraform
// plugin Framework resource.
func frameworkSchema(ctx context.Context, cfg *config.Resource) (rschema.Schema, error) {
	res := cfg.TerraformPluginFrameworkResource
	if res == nil {
		return rschema.Schema{}, errors.New(errNoFrameworkResource)
	}
	rsp := &fwresource.SchemaResponse{}
	res.Schema(ctx, fwresource.SchemaRequest{}, rsp)
	if rsp.Diagnostics.HasError() {
		return rschema.Schema{}, errors.Errorf("%s: %v", errFrameworkSchema, rsp.Diagnostics)
	}
	return rsp.Schema, nil
}

// markAbsentFramework replaces the tracked Terraform state with a null one, so
// that the plan computed against it is a create. It is the Framework
// counterpart of clearing a plugin SDKv2 instance state's ID.
func markAbsentFramework(t *controller.AsyncTracker, ty tftypes.Type) error {
	dv, err := tfprotov6.NewDynamicValue(ty, tftypes.NewValue(ty, nil))
	if err != nil {
		return errors.Wrap(err, "cannot construct a null dynamic value for the Terraform state")
	}
	t.SetFrameworkTFState(&dv)
	return nil
}

// frameworkPlanResponse converts the difference between the prior and the
// planned Terraform state into a plan response. exists reports whether the
// external resource already exists, which is what distinguishes a create from
// an update.
func (s *PlanService) frameworkPlanResponse(ctx context.Context, sch rschema.Schema, cfg *config.Resource, prior, planned tftypes.Value, requiresReplace []*tftypes.AttributePath, exists bool, declared map[string]any, unresolved []string) (*diffv1alpha1.PlanResponse, error) { //nolint:gocyclo // the cases a reported difference falls into are easier to follow as a unit
	r := &diffv1alpha1.PlanResponse{
		Action:     diffv1alpha1.Action_ACTION_NO_OP,
		ComputedAt: timestamppb.Now(),
	}
	diffs, err := planned.Diff(prior)
	if err != nil {
		return nil, errors.Wrap(err, errFrameworkStateDiff)
	}

	for _, d := range diffs {
		if d.Path == nil || len(d.Path.Steps()) == 0 {
			// The root of the diff is the resource itself, which Diff reports
			// alongside the attributes that actually differ. Reporting it
			// would restate the whole resource as a single change.
			continue
		}
		if hasNestedDiff(d.Path, diffs) {
			// Diff reports a collection or an object as well as the elements
			// inside it that differ. The elements say where the change is, so
			// reporting their container too would repeat the same change at
			// every level above it.
			continue
		}
		if isUnresolvedParameter(frameworkTerraformPath(d.Path), unresolved) {
			// The Secret behind this attribute was not supplied, so whatever
			// the diff says about it is an artefact of the value never having
			// arrived. It is reported below as unresolved instead.
			continue
		}
		replaces := forcesReplacement(d.Path, requiresReplace)
		if !isFrameworkUserChange(d, replaces) {
			continue
		}
		c := &diffv1alpha1.FieldChange{
			Field:           frameworkFieldPath(d.Path, cfg),
			RequiresReplace: exists && replaces,
			Origin:          frameworkOrigin(d.Path, declared),
		}
		// A sensitive attribute is redacted on both sides: the plan reports
		// that the field may have changed without disclosing either value.
		if isSensitivePath(ctx, sch, d.Path) {
			c.Actual = absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE)
			c.Planned = absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE)
		} else {
			if c.Actual, err = frameworkValue(d.Value2); err != nil {
				return nil, errors.Wrapf(err, fmtErrConvertFrameworkValue, c.GetField())
			}
			if c.Planned, err = frameworkValue(d.Value1); err != nil {
				return nil, errors.Wrapf(err, fmtErrConvertFrameworkValue, c.GetField())
			}
		}
		r.Changes = append(r.GetChanges(), c)
	}

	for _, k := range unresolved {
		r.Changes = append(r.GetChanges(), unresolvedChange(k))
	}

	changes := r.GetChanges()
	sort.Slice(changes, func(i, j int) bool { return changes[i].GetField() < changes[j].GetField() })

	var replacement bool
	for _, c := range changes {
		replacement = replacement || c.GetRequiresReplace()
	}
	switch {
	case !exists:
		r.Action = diffv1alpha1.Action_ACTION_CREATE
	case len(changes) == 0:
		r.Action = diffv1alpha1.Action_ACTION_NO_OP
	case replacement:
		r.Action = diffv1alpha1.Action_ACTION_REPLACE
	default:
		r.Action = diffv1alpha1.Action_ACTION_UPDATE
	}
	return r, nil
}

// isFrameworkUserChange reports whether the given difference between the
// planned and the prior state is a change worth reporting in a plan.
func isFrameworkUserChange(d tftypes.ValueDiff, replaces bool) bool {
	switch {
	case isAbsentValue(d.Value1) && isAbsentValue(d.Value2):
		// The attribute has no value on either side. Diff reports these when
		// one whole side is null, which is what a create looks like: every
		// attribute the user did not set differs from nothing by nothing.
		return false
	case replaces:
		// Replacing the external resource is always meaningful, even when the
		// value that forces it is only known after apply.
		return true
	case d.Value1 != nil && !d.Value1.IsKnown():
		// The planned value is settled only once the provider applies the
		// change. The provider recomputes such attributes on every plan, so
		// they do not reflect a change the user made.
		return false
	default:
		return true
	}
}

// isAbsentValue reports whether the given side of a difference carries no
// value a user could act on: no value at all, or a collection with nothing in
// it. A null collection and an empty one are the same absence of entries as
// far as a plan is concerned, and a provider that computes one where the state
// recorded the other would otherwise make every untagged resource look like it
// had a pending change. A value that is merely unknown is not absent: it is a
// value the provider has, and will settle once it applies the change.
func isAbsentValue(v *tftypes.Value) bool {
	switch {
	case v == nil:
		return true
	case !v.IsKnown():
		return false
	case v.IsNull():
		return true
	default:
		return isEmptyCollection(*v)
	}
}

// isEmptyCollection reports whether the given value is a collection holding no
// elements.
func isEmptyCollection(v tftypes.Value) bool {
	t := v.Type()
	switch {
	case t.Is(tftypes.List{}) || t.Is(tftypes.Set{}) || t.Is(tftypes.Tuple{}):
		var elems []tftypes.Value
		if err := v.As(&elems); err != nil {
			return false
		}
		return len(elems) == 0
	case t.Is(tftypes.Map{}) || t.Is(tftypes.Object{}):
		var elems map[string]tftypes.Value
		if err := v.As(&elems); err != nil {
			return false
		}
		return len(elems) == 0
	default:
		return false
	}
}

// hasNestedDiff reports whether any of the given differences is at a path
// strictly beneath p, which makes p a container whose own reporting would
// duplicate what its elements already say.
func hasNestedDiff(p *tftypes.AttributePath, diffs []tftypes.ValueDiff) bool {
	prefix := p.String() + "."
	for _, d := range diffs {
		if d.Path == nil || d.Path.Equal(p) {
			continue
		}
		if strings.HasPrefix(d.Path.String(), prefix) {
			return true
		}
	}
	return false
}

// forcesReplacement reports whether the attribute at the given path is one the
// plan says forces the external resource to be replaced. A path that forces a
// replacement also covers everything beneath it, which is how a whole block
// being replaced reaches the attributes inside it.
func forcesReplacement(p *tftypes.AttributePath, requiresReplace []*tftypes.AttributePath) bool {
	for _, rp := range requiresReplace {
		if rp == nil {
			continue
		}
		if p.Equal(rp) {
			return true
		}
		if strings.HasPrefix(p.String(), rp.String()+".") {
			return true
		}
	}
	return false
}

// frameworkFieldPath maps a Terraform attribute path onto the CRD path of the
// field it came from. Unlike the plugin SDKv2 path, which has to resolve a
// flat string key against the resource schema, the steps here already say what
// each segment selects.
func frameworkFieldPath(p *tftypes.AttributePath, cfg *config.Resource) string {
	path := crdParametersPath
	var tfPath []string
	for _, st := range p.Steps() {
		switch s := st.(type) {
		case tftypes.AttributeName:
			tfPath = append(tfPath, string(s))
			path += "." + name.NewFromSnake(string(s)).LowerCamelComputed
		case tftypes.ElementKeyString:
			path += "." + string(s)
		case tftypes.ElementKeyInt:
			if cfg.SchemaElementOptions.EmbeddedObject(strings.Join(tfPath, ".")) {
				// The CRD models this singleton list as an embedded object, so
				// it has no index to address.
				continue
			}
			path += "[" + strconv.FormatInt(int64(s), 10) + "]"
		case tftypes.ElementKeyValue:
			// A set element is addressed by its value rather than by a
			// position, so the path locates the field but not the element.
			path += "[" + tftypes.Value(s).String() + "]"
		}
	}
	return path
}

// frameworkTerraformPath renders an attribute path the way a resource's
// connection details mapping spells a Terraform attribute, so that the two can
// be compared: dotted field names with bracketed indices, as
// "logging_config[0].bucket".
func frameworkTerraformPath(p *tftypes.AttributePath) string {
	var b strings.Builder
	for _, st := range p.Steps() {
		switch s := st.(type) {
		case tftypes.AttributeName:
			if b.Len() > 0 {
				b.WriteString(".")
			}
			b.WriteString(string(s))
		case tftypes.ElementKeyInt:
			b.WriteString("[" + strconv.FormatInt(int64(s), 10) + "]")
		case tftypes.ElementKeyString:
			b.WriteString("[" + string(s) + "]")
		}
	}
	return b.String()
}

// frameworkOrigin reports where the planned value of the attribute at the
// given path came from, by walking the desired resource's declared parameters
// alongside it.
func frameworkOrigin(p *tftypes.AttributePath, declared map[string]any) diffv1alpha1.Origin { //nolint:gocyclo // a walk over the kinds of path step, easier to follow as a unit
	var current any = declared
	for _, st := range p.Steps() {
		switch s := st.(type) {
		case tftypes.AttributeName:
			m, ok := current.(map[string]any)
			if !ok {
				return diffv1alpha1.Origin_ORIGIN_PROVIDER
			}
			if current, ok = m[string(s)]; !ok {
				return diffv1alpha1.Origin_ORIGIN_PROVIDER
			}
		case tftypes.ElementKeyString:
			m, ok := current.(map[string]any)
			if !ok {
				return diffv1alpha1.Origin_ORIGIN_PROVIDER
			}
			if current, ok = m[string(s)]; !ok {
				return diffv1alpha1.Origin_ORIGIN_PROVIDER
			}
		case tftypes.ElementKeyInt:
			l, ok := current.([]any)
			if !ok || int(s) < 0 || int(s) >= len(l) {
				return diffv1alpha1.Origin_ORIGIN_PROVIDER
			}
			current = l[int(s)]
		case tftypes.ElementKeyValue:
			// A set element cannot be located by position in the declared
			// parameters. The collection itself was declared, which is as much
			// as can be established here.
			return diffv1alpha1.Origin_ORIGIN_DESIRED_STATE
		}
	}
	return diffv1alpha1.Origin_ORIGIN_DESIRED_STATE
}

// isSensitivePath reports whether the resource schema marks the attribute at
// the given path sensitive.
//
// A path that ends inside an attribute rather than at one does not resolve:
// asking for `secrets[0]` of a sensitive list returns an error rather than the
// list's own attribute. Sensitivity is declared on the attribute and covers
// everything within it, so the walk climbs to the nearest enclosing attribute
// and takes its answer. Treating the error as "not sensitive" would print an
// element of a sensitive collection in the clear.
func isSensitivePath(ctx context.Context, sch rschema.Schema, p *tftypes.AttributePath) bool {
	for steps := p.Steps(); len(steps) > 0; steps = steps[:len(steps)-1] {
		a, err := sch.AttributeAtTerraformPath(ctx, tftypes.NewAttributePathWithSteps(steps))
		if err != nil {
			continue
		}
		return a.IsSensitive()
	}
	return false
}

// frameworkValue converts one side of a difference into a field value. A nil
// or null value means the field does not exist on that side, which is reported
// as no value rather than as an absence with a reason.
func frameworkValue(v *tftypes.Value) (*diffv1alpha1.FieldValue, error) {
	if v == nil || v.IsNull() {
		return nil, nil
	}
	if !v.IsKnown() {
		return absentValue(diffv1alpha1.Absence_ABSENCE_UNKNOWN), nil
	}
	g, err := frameworkGoValue(*v)
	if err != nil {
		return nil, err
	}
	sv, err := structpb.NewValue(g)
	if err != nil {
		return nil, errors.Wrap(err, errConvertValue)
	}
	return &diffv1alpha1.FieldValue{Kind: &diffv1alpha1.FieldValue_Value{Value: sv}}, nil
}

// frameworkGoValue converts a Terraform value into the Go value that
// structpb represents it with. Unlike the plugin SDKv2 path, where the flatmap
// has already turned every value into a string, the types survive here.
func frameworkGoValue(v tftypes.Value) (any, error) { //nolint:gocyclo // a switch over the Terraform type system
	if v.IsNull() || !v.IsKnown() {
		return nil, nil
	}
	t := v.Type()
	switch {
	case t.Is(tftypes.String):
		var s string
		err := v.As(&s)
		return s, errors.Wrap(err, errConvertValue)
	case t.Is(tftypes.Bool):
		var b bool
		err := v.As(&b)
		return b, errors.Wrap(err, errConvertValue)
	case t.Is(tftypes.Number):
		n := new(big.Float)
		if err := v.As(&n); err != nil {
			return nil, errors.Wrap(err, errConvertValue)
		}
		f, _ := n.Float64()
		return f, nil
	case t.Is(tftypes.List{}) || t.Is(tftypes.Set{}) || t.Is(tftypes.Tuple{}):
		var elems []tftypes.Value
		if err := v.As(&elems); err != nil {
			return nil, errors.Wrap(err, errConvertValue)
		}
		l := make([]any, 0, len(elems))
		for _, e := range elems {
			g, err := frameworkGoValue(e)
			if err != nil {
				return nil, err
			}
			l = append(l, g)
		}
		return l, nil
	case t.Is(tftypes.Map{}) || t.Is(tftypes.Object{}):
		var elems map[string]tftypes.Value
		if err := v.As(&elems); err != nil {
			return nil, errors.Wrap(err, errConvertValue)
		}
		m := make(map[string]any, len(elems))
		for k, e := range elems {
			g, err := frameworkGoValue(e)
			if err != nil {
				return nil, err
			}
			m[k] = g
		}
		return m, nil
	default:
		// Nothing else appears in a resource schema, but a value the
		// conversion does not know is better reported as its Terraform
		// rendering than dropped.
		return v.String(), nil
	}
}
