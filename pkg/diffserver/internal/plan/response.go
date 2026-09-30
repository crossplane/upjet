// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"context"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/types/name"
	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

// The pieces every execution path shares when it turns a Terraform diff into a
// plan response. What is specific to how a path computes its diff lives with
// that path, in tfpluginsdk.go and tfpluginfw.go.

const (
	errConvertValue               = "cannot convert the attribute value to a protobuf value"
	errGetDesiredParameters       = "cannot get the parameters of the desired resource"
	errConvertDesiredParameters   = "cannot convert the parameters of the desired resource to their Terraform shape"
	errSensitiveParameterPaths    = "cannot determine which parameters of the desired resource come from Secrets"
	errResolveSensitiveParameters = "cannot resolve the Secrets the desired resource references"
	errMarkSecretParameter        = "cannot record a secret-referenced parameter as declared"

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
// It also returns the parameters whose Secret the request did not supply, so
// that the caller can report them rather than leave them out of the plan.
func declaredParameters(ctx context.Context, kube kclient.Client, tr resource.Terraformed, cfg *config.Resource) (declared map[string]any, unresolved []string, err error) {
	declared, err = tr.GetMergedParameters(true)
	if err != nil {
		return nil, nil, errors.Wrap(err, errGetDesiredParameters)
	}
	declared, err = cfg.ApplyTFConversions(declared, config.ToTerraform)
	if err != nil {
		return nil, nil, errors.Wrap(err, errConvertDesiredParameters)
	}

	// A parameter supplied through a Secret is not in the parameters above: the
	// reference field is marked `tf:"-"`, so nothing named after the Terraform
	// attribute exists until the Secret is read. Left alone, every such
	// parameter would look like one the provider made up rather than one the
	// user asked for, and a client that hides provider-originated changes
	// would hide a password the user had just changed.
	resolved, unresolved, err := secretParameters(ctx, kube, tr)
	if err != nil {
		return nil, nil, err
	}
	paved := fieldpath.Pave(declared)
	for _, p := range resolved {
		// Only the presence of the path is consulted, never the value, so the
		// secret itself has no reason to be here.
		if err := paved.SetValue(p, ""); err != nil {
			return nil, nil, errors.Wrap(err, errMarkSecretParameter)
		}
	}
	return declared, unresolved, nil
}

// secretResolvedMarker stands in for a secret's value while probing which
// references resolve. Nothing else is ever written into the map the probe
// fills - a real secret value never reaches it, because the probe does not
// return one - so the marker only has to be distinct from the empty string.
const secretResolvedMarker = "resolved"

// secretProbe looks Secrets up in the in-memory client the request's object
// store was loaded into, and answers with a marker rather than with what it
// found.
//
// The marker is what makes a resolved reference recognisable. Resolution
// tolerates a Secret that is not there, deliberately, so that a resource whose
// Secret was deleted first can still be deleted: it records an empty value and
// carries on. An empty value is therefore not evidence that a reference
// resolved, and neither is the attribute being present. Answering with a
// marker separates the two, and keeps secret material out of the probe's map
// along the way.
type secretProbe struct {
	kube kclient.Client
}

func (c *secretProbe) GetSecretData(ctx context.Context, ref *xpv2.SecretReference) (map[string][]byte, error) {
	s := &corev1.Secret{}
	if err := c.kube.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, s); err != nil {
		return nil, err
	}
	marked := make(map[string][]byte, len(s.Data))
	for k := range s.Data {
		marked[k] = []byte(secretResolvedMarker)
	}
	return marked, nil
}

func (c *secretProbe) GetSecretValue(ctx context.Context, sel xpv2.SecretKeySelector) ([]byte, error) {
	d, err := c.GetSecretData(ctx, &sel.SecretReference)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get secret data")
	}
	if _, ok := d[sel.Key]; !ok {
		// The Secret exists but does not carry this key, which leaves the
		// parameter as unevaluated as a missing Secret would.
		return nil, nil
	}
	return []byte(secretResolvedMarker), nil
}

// isResolvedSecret reports whether the probe recorded a value for the
// reference at the given path.
func isResolvedSecret(paved *fieldpath.Paved, path string) bool {
	v, err := paved.GetValue(path)
	if err != nil {
		return false
	}
	switch t := v.(type) {
	case string:
		return t == secretResolvedMarker
	case map[string]any:
		// A reference to a whole Secret becomes a map of its entries, each of
		// which the probe marked.
		return len(t) > 0
	default:
		return false
	}
}

// secretParameters reports on the parameters whose values come from a Secret
// the desired resource references: which Terraform attributes they are, and
// which of them the supplied object store could not resolve.
//
// Both matter to a plan. A reference the caller supplied the Secret for is a
// value the user chose, so the change it causes belongs to the desired state
// rather than to the provider. A reference the caller did not supply the
// Secret for is worse than unknown: GetSensitiveParameters tolerates a missing
// Secret, so the attribute simply never arrives, and a plan that says nothing
// about it reads as "this field does not change" when it may well change.
func secretParameters(ctx context.Context, kube kclient.Client, tr resource.Terraformed) (resolved, unresolved []string, err error) {
	mapping := tr.GetConnectionDetailsMapping()
	if len(mapping) == 0 {
		return nil, nil, nil
	}

	referenced, err := resource.SensitiveParameterPaths(tr, mapping)
	if err != nil {
		return nil, nil, errors.Wrap(err, errSensitiveParameterPaths)
	}
	if len(referenced) == 0 {
		return nil, nil, nil
	}

	// This runs the resource's own resolution so that wildcards, whole-Secret
	// references and the spec/initProvider precedence behave exactly as they
	// do in a reconcile, but against a probe, so what lands is a record of
	// which references resolved rather than the secrets themselves.
	into := map[string]any{}
	if err := resource.GetSensitiveParameters(ctx, &secretProbe{kube: kube}, tr, into, mapping); err != nil {
		return nil, nil, errors.Wrap(err, errResolveSensitiveParameters)
	}

	paved := fieldpath.Pave(into)
	for _, p := range referenced {
		if !isResolvedSecret(paved, p) {
			unresolved = append(unresolved, p)
			continue
		}
		resolved = append(resolved, p)
	}
	return resolved, unresolved, nil
}

// absentValue returns a field value that carries no concrete value, with the
// reason it does not.
func absentValue(a diffv1alpha1.Absence) *diffv1alpha1.FieldValue {
	return &diffv1alpha1.FieldValue{Kind: &diffv1alpha1.FieldValue_Absence{Absence: a}}
}

// unresolvedSet indexes the given Terraform attribute paths for lookup while
// walking a diff.
func unresolvedSet(paths []string) map[string]struct{} {
	if len(paths) == 0 {
		return nil
	}
	s := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		s[p] = struct{}{}
	}
	return s
}

// unresolvedChange reports a parameter whose Secret the request did not
// supply. Both sides are absent, for different reasons: the current value is
// sensitive, and the desired one could not be evaluated at all. The field is
// the user's, because they declared the reference, so a client that hides
// provider-originated changes still shows this one.
func unresolvedChange(tfPath string) *diffv1alpha1.FieldChange {
	return &diffv1alpha1.FieldChange{
		Field:   crdParametersPath + "." + crdFieldPath(tfPath),
		Actual:  absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE),
		Planned: absentValue(diffv1alpha1.Absence_ABSENCE_UNRESOLVED),
		Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
	}
}

// crdFieldPath renders a Terraform attribute path the way the CRD spells it,
// lower camel casing each field name and leaving any index alone. It is used
// for a path that has no diff to walk alongside, which is why it works off the
// path's own shape rather than the resource schema.
func crdFieldPath(tfPath string) string {
	segments := strings.Split(tfPath, ".")
	for i, s := range segments {
		field, index := s, ""
		if b := strings.Index(s, "["); b >= 0 {
			field, index = s[:b], s[b:]
		}
		segments[i] = name.NewFromSnake(field).LowerCamelComputed + index
	}
	return strings.Join(segments, ".")
}
