// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"context"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/resource"
)

const (
	errReconstructTerraformState = "cannot reconstruct Terraform resource state"
	errConnectPluginSDKv2        = "cannot connect for Terraform plugin SDKv2 resource"
	errObservePluginSDKv2        = "cannot observe Terraform plugin SDKv2 resource"
	errGetInstanceDiff           = "cannot read Terraform plugin SDKv2 resource instance diff"
	errDiffPluginSDKv2           = "cannot compute diff for a Terraform plugin SDKv2 resource"
)

func (s *PlanService) diffTerraformPluginSDK(ctx context.Context, kc kclient.Client, cfg *config.Resource, desired, actual xpresource.Managed) error {
	opTracker := controller.NewOperationStore(s.log)
	c := controller.NewTerraformPluginSDKConnector(
		kc, s.setupFn, cfg, opTracker,
		controller.WithTerraformPluginSDKLogger(s.log),
		controller.WithObservationMode(controller.UseLocalState),
	)
	ec, err := c.Connect(ctx, desired)
	if err != nil {
		return errors.Wrap(err, errConnectPluginSDKv2)
	}

	tr, ok := actual.(resource.Terraformed)
	if !ok {
		return errors.Errorf(fmtErrNotTerraformed, actual.GetObjectKind().GroupVersionKind().String())
	}
	opTracker.Tracker(tr).ResetReconstructedTfState()
	if _, _, _, err := c.ReconstructTerraformState(ctx, tr, s.log); err != nil {
		return errors.Wrap(err, errReconstructTerraformState)
	}

	_, err = ec.Observe(ctx, desired)
	if err != nil {
		return errors.Wrap(err, errObservePluginSDKv2)
	}

	diff, err := controller.TerraformPluginSDKInstanceDiff(ec)
	if err != nil {
		return errors.Wrap(err, errGetInstanceDiff)
	}
	filterInstanceDiff(diff)
	return nil
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
