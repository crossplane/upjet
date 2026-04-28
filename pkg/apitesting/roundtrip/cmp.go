// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0
package roundtrip

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// normalizeMeta strips volatile metadata fields from obj so that round-trip
// comparisons focus on the API fields that are part of the provider contract.
// Fields like ResourceVersion, UID, Generation, and ManagedFields are set by
// the API server and vary across encode/decode cycles.
func normalizeMeta(obj metav1.Object) {
	obj.SetResourceVersion("")
	obj.SetGeneration(0)
	obj.SetUID("")
	obj.SetManagedFields(nil)
	obj.SetCreationTimestamp(metav1.Time{})
}

// EquateEmptyAndSingleZeroSlice returns a cmp.Option that treats an empty (or
// nil) slice as equal to a slice containing exactly one zero-value element:
//
//   - pointer slices:  []*T{}  ≡  []*T{nil}
//   - value slices:    []T{}   ≡  []T{zero}  (zero is the zero value of T)
//
// This handles conversions that produce []T{zero} where the source had []T{}.
//
// Conflict avoidance: cmpopts.EquateEmpty (already in the default options)
// registers its own Comparer for the case where both sides have Len==0.
// This option requires at least one side to have Len==1, so the two
// Comparers are mutually exclusive and go-cmp never sees an ambiguous pair.
//
// Multi-element slices and slices whose single element is non-zero fall
// through to go-cmp's normal element-by-element comparison.
//
// Use with WithComparisonOptions:
//
//	rt, _ := roundtrip.NewRoundTripTest(provider, nil,
//	    roundtrip.WithComparisonOptions(roundtrip.EquateEmptyAndSingleZeroSlice()))
func EquateEmptyAndSingleZeroSlice() cmp.Option { //nolint:gocyclo // easier to follow as a unit
	// isEffectivelyZero recursively checks whether v is "effectively zero"
	// under the same equate-empty-or-single-zero semantics:
	//   - slice/array: nil, len==0, or len==1 with an effectively-zero element
	//   - struct: every exported and unexported field is effectively zero
	//   - ptr/interface: nil or the pointed-to value is effectively zero
	//   - map: nil or len==0
	//   - everything else: reflect.Value.IsZero()
	var isEffectivelyZero func(v reflect.Value) bool
	isEffectivelyZero = func(v reflect.Value) bool {
		switch v.Kind() { //nolint:exhaustive // default covers remaining kinds
		case reflect.Slice, reflect.Array:
			return v.Len() == 0 || (v.Len() == 1 && isEffectivelyZero(v.Index(0)))
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if !isEffectivelyZero(v.Field(i)) {
					return false
				}
			}
			return true
		case reflect.Ptr, reflect.Interface:
			return v.IsNil() || isEffectivelyZero(v.Elem())
		case reflect.Map:
			return v.IsNil() || v.Len() == 0
		default:
			return v.IsZero()
		}
	}

	// isSingleZero reports whether v is a slice with exactly one element that
	// is effectively zero (nil for pointers, "" for strings, recursively zero
	// for structs containing nested slices, etc.).
	isSingleZero := func(v reflect.Value) bool {
		return v.Len() == 1 && isEffectivelyZero(v.Index(0))
	}
	isEmptyOrSingleZero := func(v reflect.Value) bool {
		return v.Len() == 0 || isSingleZero(v)
	}
	return cmp.FilterValues(
		func(x, y interface{}) bool {
			vx, vy := reflect.ValueOf(x), reflect.ValueOf(y)
			if !vx.IsValid() || !vy.IsValid() {
				return false
			}
			if vx.Kind() != reflect.Slice || vy.Kind() != reflect.Slice {
				return false
			}
			// Both sides must be empty-or-single-zero AND at least one must be
			// exactly the single-zero case (Len==1, zero element).
			//
			// Using || instead would intercept e.g. []T{non_zero} vs []T{zero},
			// preventing go-cmp from recursing into the elements where
			// cmpopts.EquateEmpty could equate sub-fields.  Requiring both
			// sides to qualify ensures we only short-circuit truly equivalent
			// pairs and let everything else fall through to element comparison.
			//
			// cmpopts.EquateEmpty covers both-Len==0; requiring Len==1 on at
			// least one side keeps the two Comparers mutually exclusive.
			return (isSingleZero(vx) || isSingleZero(vy)) &&
				isEmptyOrSingleZero(vx) && isEmptyOrSingleZero(vy)
		},
		cmp.Comparer(func(x, y interface{}) bool {
			vx, vy := reflect.ValueOf(x), reflect.ValueOf(y)
			return isEmptyOrSingleZero(vx) && isEmptyOrSingleZero(vy)
		}),
	)
}
