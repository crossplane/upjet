// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"fmt"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	xpmetav1 "github.com/crossplane/crossplane/apis/pkg/meta/v1"
	xpmetav1alpha1 "github.com/crossplane/crossplane/apis/pkg/meta/v1alpha1"
	xppkgv1 "github.com/crossplane/crossplane/apis/pkg/v1"
	xppkgv1beta1 "github.com/crossplane/crossplane/apis/pkg/v1beta1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	k8sjson "sigs.k8s.io/json"
)

const (
	errFromUnstructured            = "failed to convert from unstructured.Unstructured to the managed resource type"
	errFromUnstructuredConfMeta    = "failed to convert from unstructured.Unstructured to Crossplane Configuration metadata"
	errFromUnstructuredConfPackage = "failed to convert from unstructured.Unstructured to Crossplane Configuration package"
	errFromUnstructuredProvider    = "failed to convert from unstructured.Unstructured to Crossplane Provider package"
	errFromUnstructuredLock        = "failed to convert from unstructured.Unstructured to Crossplane package lock"
	errToUnstructured              = "failed to convert from the managed resource type to unstructured.Unstructured"
	errRawExtensionUnmarshal       = "failed to unmarshal runtime.RawExtension"

	errFmtPavedDelete = "failed to delete fieldpath %q from paved"

	metadataAnnotationPaveKey = "metadata.annotations['%s']"
)

// CopyInto copies values of fields from the migration `source` object
// into the migration `target` object and fills in the target object's
// TypeMeta using the supplied `targetGVK`. While copying fields from
// migration source to migration target, the fields at the paths
// specified with `skipFieldPaths` array are skipped. This is a utility
// that can be used in the migration resource converter implementations.
// If a certain field with the same name in both the `source` and the `target`
// objects has different types in `source` and `target`, then it must be
// included in the `skipFieldPaths` and it must manually be handled in the
// conversion function.
func CopyInto(source any, target any, targetGVK schema.GroupVersionKind, skipFieldPaths ...string) (any, error) {
	u := ToSanitizedUnstructured(source)
	paved := fieldpath.Pave(u.Object)
	skipFieldPaths = append(skipFieldPaths, "apiVersion", "kind",
		fmt.Sprintf(metadataAnnotationPaveKey, xpmeta.AnnotationKeyExternalCreatePending),
		fmt.Sprintf(metadataAnnotationPaveKey, xpmeta.AnnotationKeyExternalCreateSucceeded),
		fmt.Sprintf(metadataAnnotationPaveKey, xpmeta.AnnotationKeyExternalCreateFailed),
		fmt.Sprintf(metadataAnnotationPaveKey, corev1.LastAppliedConfigAnnotation),
	)
	for _, p := range skipFieldPaths {
		if err := paved.DeleteField(p); err != nil {
			return nil, errors.Wrapf(err, errFmtPavedDelete, p)
		}
	}
	u.SetGroupVersionKind(targetGVK)
	return target, errors.Wrap(runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, target), errFromUnstructured)
}

// sanitizeResource removes certain fields from the unstructured object.
// It turns out that certain fields, such as `metadata.creationTimestamp`
// are still serialized even if they have zero-values. This function
// removes such fields. We also unconditionally sanitize `status`
// so that the controller will populate it back.
func sanitizeResource(m map[string]any) map[string]any {
	delete(m, "status")
	if _, ok := m["metadata"]; !ok {
		return m
	}
	metadata := m["metadata"].(map[string]any)

	if v := metadata["creationTimestamp"]; v == nil {
		delete(metadata, "creationTimestamp")
	}
	if len(metadata) == 0 {
		delete(m, "metadata")
	}
	removeNilValuedKeys(m)
	return m
}

// removeNilValuedKeys removes nil values from the specified map so that
// the serialized manifest do not contain corresponding superfluous YAML
// nulls.
func removeNilValuedKeys(m map[string]interface{}) {
	for k, v := range m {
		if v == nil {
			delete(m, k)
			continue
		}
		switch c := v.(type) {
		case map[string]any:
			removeNilValuedKeys(c)
		case []any:
			for _, e := range c {
				if cm, ok := e.(map[string]interface{}); ok {
					removeNilValuedKeys(cm)
				}
			}
		}
	}
}

// ToSanitizedUnstructured converts the specified managed resource to an
// unstructured.Unstructured. Before the converted object is
// returned, it's sanitized by removing certain fields
// (like status, metadata.creationTimestamp).
func ToSanitizedUnstructured(mg any) unstructured.Unstructured {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(mg)
	if err != nil {
		panic(errors.Wrap(err, errToUnstructured))
	}
	return unstructured.Unstructured{
		Object: sanitizeResource(m),
	}
}

// FromRawExtension attempts to convert a runtime.RawExtension into
// an unstructured.Unstructured.
func FromRawExtension(r runtime.RawExtension) (unstructured.Unstructured, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(r.Raw, &m); err != nil {
		return unstructured.Unstructured{}, errors.Wrap(err, errRawExtensionUnmarshal)
	}
	return unstructured.Unstructured{
		Object: m,
	}, nil
}

// FromGroupVersionKind converts a schema.GroupVersionKind into
// a migration.GroupVersionKind.
func FromGroupVersionKind(gvk schema.GroupVersionKind) GroupVersionKind {
	return GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
}

// ToComposition converts the specified unstructured.Unstructured to
// a Crossplane Composition.
// Workaround for:
// https://github.com/kubernetes-sigs/structured-merge-diff/issues/230
func ToComposition(u unstructured.Unstructured) (*xpv1.Composition, error) {
	buff, err := json.Marshal(u.Object)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal map to JSON")
	}
	c := &xpv1.Composition{}
	return c, errors.Wrap(k8sjson.UnmarshalCaseSensitivePreserveInts(buff, c), "failed to unmarshal into a v1.Composition")
}

func addGVK(u unstructured.Unstructured, target map[string]any) map[string]any {
	if target == nil {
		target = make(map[string]any)
	}
	target["apiVersion"] = u.GetAPIVersion()
	target["kind"] = u.GetKind()
	return target
}

func addNameGVK(u unstructured.Unstructured, target map[string]any) map[string]any {
	target = addGVK(u, target)
	m := target["metadata"]
	if m == nil {
		m = make(map[string]any)
	}
	metadata := m.(map[string]any)
	metadata["name"] = u.GetName()
	if len(u.GetNamespace()) != 0 {
		metadata["namespace"] = u.GetNamespace()
	}
	target["metadata"] = m
	return target
}

func toManagedResource(c runtime.ObjectCreater, u unstructured.Unstructured) (resource.Managed, bool, error) {
	gvk := u.GroupVersionKind()
	if gvk == xpv1.CompositionGroupVersionKind {
		return nil, false, nil
	}
	obj, err := c.New(gvk)
	if err != nil {
		return nil, false, errors.Wrapf(err, errFmtNewObject, gvk)
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj); err != nil {
		return nil, false, errors.Wrap(err, errFromUnstructured)
	}
	mg, ok := obj.(resource.Managed)
	return mg, ok, nil
}

func toConfigurationPackageV1(u unstructured.Unstructured) (*xppkgv1.Configuration, error) {
	conf := &xppkgv1.Configuration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, conf); err != nil {
		return nil, errors.Wrap(err, errFromUnstructuredConfPackage)
	}
	return conf, nil
}

func toConfigurationMetadataV1(u unstructured.Unstructured) (*xpmetav1.Configuration, error) {
	conf := &xpmetav1.Configuration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, conf); err != nil {
		return nil, errors.Wrap(err, errFromUnstructuredConfMeta)
	}
	return conf, nil
}

func toConfigurationMetadataV1Alpha1(u unstructured.Unstructured) (*xpmetav1alpha1.Configuration, error) {
	conf := &xpmetav1alpha1.Configuration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, conf); err != nil {
		return nil, errors.Wrap(err, errFromUnstructuredConfMeta)
	}
	return conf, nil
}

func toConfigurationMetadata(u unstructured.Unstructured) (metav1.Object, error) {
	var conf metav1.Object
	var err error
	switch u.GroupVersionKind().Version {
	case "v1alpha1":
		conf, err = toConfigurationMetadataV1Alpha1(u)
	default:
		conf, err = toConfigurationMetadataV1(u)
	}
	return conf, err
}

func toProviderPackage(u unstructured.Unstructured) (*xppkgv1.Provider, error) {
	pkg := &xppkgv1.Provider{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, pkg); err != nil {
		return nil, errors.Wrap(err, errFromUnstructuredProvider)
	}
	return pkg, nil
}

func getCategory(u unstructured.Unstructured) Category {
	switch u.GroupVersionKind() {
	case xpv1.CompositionGroupVersionKind:
		return CategoryComposition
	default:
		return categoryUnknown
	}
}

func toPackageLock(u unstructured.Unstructured) (*xppkgv1beta1.Lock, error) {
	lock := &xppkgv1beta1.Lock{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, lock); err != nil {
		return nil, errors.Wrap(err, errFromUnstructuredLock)
	}
	return lock, nil
}

// ConvertComposedTemplatePatchesMap converts the composed templates with given conversionMap
// Key of the conversionMap points to the source field
// Value of the conversionMap points to the target field
func ConvertComposedTemplatePatchesMap(sourceTemplate xpv1.ComposedTemplate, conversionMap map[string]string) []xpv1.Patch {
	var patchesToAdd []xpv1.Patch
	for _, p := range sourceTemplate.Patches {
		switch p.Type { //nolint:exhaustive
		case xpv1.PatchTypeFromCompositeFieldPath, xpv1.PatchTypeCombineFromComposite, "":
			{
				if p.ToFieldPath != nil {
					if to, ok := conversionMap[*p.ToFieldPath]; ok {
						patchesToAdd = append(patchesToAdd, xpv1.Patch{
							Type:          p.Type,
							FromFieldPath: p.FromFieldPath,
							ToFieldPath:   &to,
							Transforms:    p.Transforms,
							Policy:        p.Policy,
							Combine:       p.Combine,
						})
					}
				}
			}
		case xpv1.PatchTypeToCompositeFieldPath, xpv1.PatchTypeCombineToComposite:
			{
				if p.FromFieldPath != nil {
					if to, ok := conversionMap[*p.FromFieldPath]; ok {
						patchesToAdd = append(patchesToAdd, xpv1.Patch{
							Type:          p.Type,
							FromFieldPath: &to,
							ToFieldPath:   p.ToFieldPath,
							Transforms:    p.Transforms,
							Policy:        p.Policy,
							Combine:       p.Combine,
						})
					}
				}
			}
		}
	}
	return patchesToAdd
}
