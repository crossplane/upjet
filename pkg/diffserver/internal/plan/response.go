// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"context"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
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

// planConfig returns the resource configuration to plan the given resource
// with.
//
// A resource's Terraform conversions turn the CRD's embedded objects into the
// singleton lists Terraform expects, and back again when the observation is
// read. Which paths those are is a property of the Terraform schema alone -
// TFListConversionPaths(), populated once from the (version-invariant)
// Terraform schema regardless of which CRD version is being generated - so it
// is available for any served version, whether or not the reconciler's own
// TerraformConversions happens to include the conversion.
//
// Whether a given served version's CRD type actually needs that conversion
// is a different question: it depends on whether that version's Go type
// still declares the field as a list (the shape the versions predating the
// embedding used, and already the shape Terraform expects, so converting it
// again would wrap a list into a list of lists and unwrap one into an object
// on the way out) or as an embedded object (cfg.Version's shape, and that of
// every other version sharing it). cfg.SingletonListVersions is how a
// provider records which served versions are the former; every version not
// listed there is assumed to share cfg.Version's embedded shape.
//
// A reconciler never has to ask this question, because the API server
// converts every object to the reconciled version before a controller sees
// it, and whatever TerraformConversions a provider registered already
// matches that one version. The diff server is the first caller to take the
// API version from a request rather than always operating on the reconciled
// one, so it is the first that needs an answer for every served version: the
// conversion is dropped for a version listed in SingletonListVersions even
// if the reconciler's configuration carries it, and constructed from
// TFListConversionPaths() for an unlisted (embedded) version even if the
// reconciler's configuration does not - which happens whenever the
// reconciler itself runs on a legacy-shaped version and so was never given
// the conversion to begin with.
func planConfig(cfg *config.Resource, mg xpresource.Managed) *config.Resource {
	singleton := config.NewTFSingletonConversion()
	has := false
	for _, c := range cfg.TerraformConversions {
		// The conversions are empty structs, so comparing against a fresh one
		// selects it by type without naming the type, which the package does
		// not export.
		if c == singleton {
			has = true
			break
		}
	}
	legacy := false
	v := mg.GetObjectKind().GroupVersionKind().Version
	for _, lv := range cfg.SingletonListVersions {
		if lv == v {
			legacy = true
			break
		}
	}
	needed := !legacy && len(cfg.TFListConversionPaths()) > 0

	if has == needed {
		return cfg
	}

	kept := make([]config.TerraformConversion, 0, len(cfg.TerraformConversions)+1)
	for _, c := range cfg.TerraformConversions {
		// Every other conversion, such as the one for dynamically typed
		// attributes, is unrelated to the CRD's shape and is kept
		// unconditionally.
		if c != singleton {
			kept = append(kept, c)
		}
	}
	if needed {
		kept = append(kept, singleton)
	}
	// A shallow copy is enough: only this slice is replaced, and the shared
	// configuration the provider's controllers use is left untouched.
	c := *cfg
	c.TerraformConversions = kept
	return &c
}

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
	resolved, unresolved, probed, err := secretParameters(ctx, kube, tr)
	if err != nil {
		return nil, nil, err
	}
	paved := fieldpath.Pave(declared)
	probedPaved := fieldpath.Pave(probed)
	for _, p := range resolved {
		// The marker the probe recorded is set here rather than a flat
		// empty string: a whole-Secret reference resolves to a map or a
		// list, one marker per entry, and only that shape - not a scalar -
		// lets a path beneath p, such as a single entry of that map, still
		// walk all the way through declared. Only the presence of each path
		// is ever consulted, never the value, so the secret itself has no
		// reason to be here either way.
		v, err := probedPaved.GetValue(p)
		if err != nil {
			return nil, nil, errors.Wrap(err, errMarkSecretParameter)
		}
		if err := paved.SetValue(p, v); err != nil {
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
	case []any:
		// A list of key selectors becomes a list of values, one per selector.
		// It counts as resolved only when every one of them was found, because
		// a selector whose key is missing records an empty value rather than
		// failing, and a parameter built from a partly read list is not one
		// the plan can stand behind.
		if len(t) == 0 {
			return false
		}
		for _, e := range t {
			if s, ok := e.(string); !ok || s != secretResolvedMarker {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// secretParameters reports on the parameters whose values come from a Secret
// the desired resource references: which Terraform attributes they are, and
// which of them the supplied object store could not resolve. probed is the
// marker map the resolution was checked against, which the caller also needs
// to record a resolved reference's presence in the shape a diff can walk
// through, for a reference that names a whole Secret.
//
// Both matter to a plan. A reference the caller supplied the Secret for is a
// value the user chose, so the change it causes belongs to the desired state
// rather than to the provider. A reference the caller did not supply the
// Secret for is worse than unknown: GetSensitiveParameters tolerates a missing
// Secret, so the attribute simply never arrives, and a plan that says nothing
// about it reads as "this field does not change" when it may well change.
func secretParameters(ctx context.Context, kube kclient.Client, tr resource.Terraformed) (resolved, unresolved []string, probed map[string]any, err error) {
	mapping := tr.GetConnectionDetailsMapping()
	if len(mapping) == 0 {
		return nil, nil, nil, nil
	}

	referenced, err := resource.SensitiveParameterPaths(tr, mapping)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, errSensitiveParameterPaths)
	}
	if len(referenced) == 0 {
		return nil, nil, nil, nil
	}

	// This runs the resource's own resolution so that wildcards, whole-Secret
	// references and the spec/initProvider precedence behave exactly as they
	// do in a reconcile, but against a probe, so what lands is a record of
	// which references resolved rather than the secrets themselves.
	into := map[string]any{}
	if err := resource.GetSensitiveParameters(ctx, &secretProbe{kube: kube}, tr, into, mapping); err != nil {
		return nil, nil, nil, errors.Wrap(err, errResolveSensitiveParameters)
	}

	paved := fieldpath.Pave(into)
	for _, p := range referenced {
		if !isResolvedSecret(paved, p) {
			unresolved = append(unresolved, p)
			continue
		}
		resolved = append(resolved, p)
	}
	return resolved, unresolved, into, nil
}

// absentValue returns a field value that carries no concrete value, with the
// reason it does not.
func absentValue(a diffv1alpha1.Absence) *diffv1alpha1.FieldValue {
	return &diffv1alpha1.FieldValue{Kind: &diffv1alpha1.FieldValue_Absence{Absence: a}}
}

// flatmapPath renders a Terraform attribute path the way a diff spells it,
// with every index and map key as a segment of its own. The paths a resource's
// sensitive parameters are reported at use the field path syntax instead, so
// the two have to be brought to one form before they can be compared:
// "action[0].client_secret" and "action.0.client_secret" are the same
// attribute.
func flatmapPath(p string) string {
	return strings.NewReplacer("[", ".", "]", "").Replace(p)
}

// isUnresolvedParameter reports whether the attribute at the given path is one
// whose Secret the request did not supply.
//
// A reference to a whole Secret names the parameter its entries land under,
// and the diff reports those entries individually, so everything beneath an
// unresolved path is unresolved too.
func isUnresolvedParameter(path string, unresolved []string) bool {
	if len(unresolved) == 0 {
		return false
	}
	p := flatmapPath(path)
	for _, u := range unresolved {
		f := flatmapPath(u)
		if p == f || strings.HasPrefix(p, f+".") {
			return true
		}
	}
	return false
}

// unresolvedChange reports a parameter whose Secret the request did not
// supply.
//
// exists reports whether the external resource already exists. Both sides are
// absent, for different reasons: the desired value could not be evaluated at
// all, and the current one - when there is one to withhold at all - is
// treated as sensitive by association with the Secret it would otherwise come
// from. The field is the user's, because they declared the reference, so a
// client that hides provider-originated changes still shows this one.
func unresolvedChange(tfPath string, cfg *config.Resource, exists bool) *diffv1alpha1.FieldChange {
	c := &diffv1alpha1.FieldChange{
		Field:   crdParametersPath + "." + crdFieldPath(tfPath, cfg),
		Planned: absentValue(diffv1alpha1.Absence_ABSENCE_UNRESOLVED),
		Origin:  diffv1alpha1.Origin_ORIGIN_DESIRED_STATE,
	}
	if exists {
		c.Actual = absentValue(diffv1alpha1.Absence_ABSENCE_SENSITIVE)
	}
	return c
}

// crdFieldPath renders a Terraform attribute path the way the CRD spells it,
// lower camel casing each field name segment. An index is left alone, unless
// it addresses a singleton list the CRD models as an embedded object, in
// which case it is dropped - the same convention frameworkFieldPath and
// fieldPath follow for a resolved change, which this has no schema in hand to
// walk alongside and so works off the path's own shape instead. Without this,
// the same attribute would be spelled two different ways depending on
// whether its Secret happened to resolve.
func crdFieldPath(tfPath string, cfg *config.Resource) string {
	segments := strings.Split(tfPath, ".")
	var seenFields []string
	for i, s := range segments {
		field, index := s, ""
		if b := strings.Index(s, "["); b >= 0 {
			field, index = s[:b], s[b:]
		}
		seenFields = append(seenFields, field)
		if index != "" && cfg.SchemaElementOptions.EmbeddedObject(strings.Join(seenFields, ".")) {
			index = ""
		}
		segments[i] = name.NewFromSnake(field).LowerCamelComputed + index
	}
	return strings.Join(segments, ".")
}
