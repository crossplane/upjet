// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource"
	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

// The pieces every execution path shares when it turns a Terraform diff into a
// plan response. What is specific to how a path computes its diff lives with
// that path, in tfpluginsdk.go and tfpluginfw.go.

const (
	errConvertValue             = "cannot convert the attribute value to a protobuf value"
	errGetDesiredParameters     = "cannot get the parameters of the desired resource"
	errConvertDesiredParameters = "cannot convert the parameters of the desired resource to their Terraform shape"

	// crdParametersPath is the path, in a managed resource's manifest, under
	// which the Terraform resource's arguments appear.
	crdParametersPath = "spec.forProvider"
)

// declaredParameters returns the parameters the desired resource declares, in
// their Terraform shape.
//
// They are what the origin of a change is decided against, so they have to be
// in the same shape as the paths a Terraform diff reports: this is what turns
// the CRD's embedded objects back into the singleton lists Terraform has.
// Merging spec.initProvider in is deliberate, because a field declared there
// is still a field the user asked for.
func declaredParameters(tr resource.Terraformed, cfg *config.Resource) (map[string]any, error) {
	declared, err := tr.GetMergedParameters(true)
	if err != nil {
		return nil, errors.Wrap(err, errGetDesiredParameters)
	}
	declared, err = cfg.ApplyTFConversions(declared, config.ToTerraform)
	return declared, errors.Wrap(err, errConvertDesiredParameters)
}

// absentValue returns a field value that carries no concrete value, with the
// reason it does not.
func absentValue(a diffv1alpha1.Absence) *diffv1alpha1.FieldValue {
	return &diffv1alpha1.FieldValue{Kind: &diffv1alpha1.FieldValue_Absence{Absence: a}}
}
