// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package resource

import (
	"math"
	"strconv"

	"github.com/pkg/errors"
)

// maxExactFloat64Int is the largest integer a float64 represents exactly (2^53).
// Past it, JSON decoding into a float64 loses precision, so an id read back from
// the state could differ from the real one.
const maxExactFloat64Int = 1 << 53

// NormalizeStateID rewrites a numeric `id` in a decoded Terraform state to its
// string form, in place.
//
// Terraform's plugin-SDK always stores `id` as a string, but plugin-framework
// providers may declare it as a number, and Terraform writes it back into the
// state with that type. Everything downstream of the state — the generated
// Observation structs, the external-name annotation, connection details — models
// the id as a string, so a numeric one fails to decode with errors like:
//
//	v1alpha1.LibraryObservation.ID: ReadString: expects " or n, but found 5
//
// An id beyond 2^53 is rejected rather than converted: it cannot round-trip
// through a float64, and a truncated id would address the wrong remote resource.
func NormalizeStateID(tfstate map[string]any) error {
	id, ok := tfstate["id"]
	if !ok {
		return nil
	}
	f, ok := id.(float64)
	if !ok {
		// Strings need no work, and any other type is left alone so the caller
		// reports it with its own context rather than being masked here.
		return nil
	}
	if f != math.Trunc(f) || math.Abs(f) > maxExactFloat64Int {
		return errors.Errorf("id in state is not an exactly representable integer: %v", f)
	}
	tfstate["id"] = strconv.FormatFloat(f, 'f', -1, 64)
	return nil
}
