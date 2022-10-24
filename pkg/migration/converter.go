// Copyright 2022 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package migration

import (
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
)

const (
	errFromUnstructured      = "failed to convert from unstructured.Unstructured to the managed resource type"
	errToUnstructured        = "failed to convert from the managed resource type to unstructured.Unstructured"
	errRawExtensionUnmarshal = "failed to unmarshal runtime.RawExtension"

	errFmtPavedDelete = "failed to delete fieldpath %q from paved"
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
	u := ToUnstructured(source)
	paved := fieldpath.Pave(u.Object)
	skipFieldPaths = append(skipFieldPaths, "apiVersion", "kind")
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
	return m
}

// ToUnstructured converts the specified managed resource to an
// unstructured.Unstructured. Before the converted object is
// returned, it's sanitized by removing certain fields
// (like status, metadata.creationTimestamp).
func ToUnstructured(mg any) unstructured.Unstructured {
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
