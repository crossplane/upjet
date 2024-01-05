// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
)

// RoundTrip round-trips from `src` to `dst` via an unstructured map[string]any
// representation of the `src` object.
func RoundTrip(dst, src runtime.Object) error {
	srcMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(src)
	if err != nil {
		return errors.Wrap(err, "cannot convert the conversion source object into the map[string]interface{} representation")
	}
	gvk := dst.GetObjectKind().GroupVersionKind()
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(srcMap, dst); err != nil {
		return errors.Wrap(err, "cannot convert the map[string]interface{} representation to the conversion target object")
	}
	// restore the original GVK for the conversion destination
	dst.GetObjectKind().SetGroupVersionKind(gvk)
	return nil
}
