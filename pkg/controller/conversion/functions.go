// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/crossplane/upjet/v2/pkg/config/conversion"
	"github.com/crossplane/upjet/v2/pkg/resource"
)

const (
	errFmtPrioritizedManagedConversion = "cannot apply the PrioritizedManagedConversion for the %q object"
	errFmtPavedConversion              = "cannot apply the PavedConversion for the %q object"
	errFmtManagedConversion            = "cannot apply the ManagedConversion for the %q object"
	errFmtGetGVK                       = "cannot get the GVK for the %s object of type %T"
)

// RoundTrip round-trips from `src` to `dst` via an unstructured map[string]any
// representation of the `src` object and applies the registered webhook
// conversion functions of this registry.
func (r *registry) RoundTrip(dst, src resource.Terraformed) error { //nolint:gocyclo // considered breaking this according to the converters and I did not like it
	if dst.GetObjectKind().GroupVersionKind().Version == "" {
		gvk, err := apiutil.GVKForObject(dst, r.scheme)
		if err != nil && !runtime.IsNotRegisteredError(err) {
			return errors.Wrapf(err, errFmtGetGVK, "destination", dst)
		}
		if err == nil {
			dst.GetObjectKind().SetGroupVersionKind(gvk)
		}
	}
	if src.GetObjectKind().GroupVersionKind().Version == "" {
		gvk, err := apiutil.GVKForObject(src, r.scheme)
		if err != nil && !runtime.IsNotRegisteredError(err) {
			return errors.Wrapf(err, errFmtGetGVK, "source", src)
		}
		if err == nil {
			src.GetObjectKind().SetGroupVersionKind(gvk)
		}
	}

	// first PrioritizedManagedConversions are run in their registration order
	for _, c := range r.GetConversions(dst) {
		if pc, ok := c.(conversion.PrioritizedManagedConversion); ok {
			if _, err := pc.ConvertManaged(src, dst); err != nil {
				return errors.Wrapf(err, errFmtPrioritizedManagedConversion, dst.GetTerraformResourceType())
			}
		}
	}

	srcMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(src)
	if err != nil {
		return errors.Wrap(err, "cannot convert the conversion source object into the map[string]any representation")
	}
	// now we will try to run the registered webhook conversions
	dstMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dst)
	if err != nil {
		return errors.Wrap(err, "cannot convert the conversion destination object into the map[string]any representation")
	}
	srcPaved := fieldpath.Pave(srcMap)
	dstPaved := fieldpath.Pave(dstMap)
	// then run the PavedConversions
	for _, c := range r.GetConversions(dst) {
		if pc, ok := c.(conversion.PavedConversion); ok {
			if _, err := pc.ConvertPaved(srcPaved, dstPaved); err != nil {
				return errors.Wrapf(err, errFmtPavedConversion, dst.GetTerraformResourceType())
			}
		}
	}
	// convert the map[string]any representation of the conversion target back to
	// the original type.
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(dstMap, dst); err != nil {
		return errors.Wrap(err, "cannot convert the map[string]any representation of the conversion target back to the object itself")
	}

	// finally at the third stage, run the ManagedConverters
	for _, c := range r.GetConversions(dst) {
		if tc, ok := c.(conversion.ManagedConversion); ok {
			if _, ok := tc.(conversion.PrioritizedManagedConversion); ok {
				continue // then already run in the first stage
			}
			if _, err := tc.ConvertManaged(src, dst); err != nil {
				return errors.Wrapf(err, errFmtManagedConversion, dst.GetTerraformResourceType())
			}
		}
	}

	return nil
}

// RoundTrip round-trips from `src` to `dst` via an unstructured map[string]any
// representation of the `src` object and applies the registered webhook
// conversion functions.
func RoundTrip(dst, src resource.Terraformed) error {
	return instance.RoundTrip(dst, src)
}
