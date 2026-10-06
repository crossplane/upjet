// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

// Copied from
// https://github.com/crossplane/crossplane/blob/b56f55c021a195ceb301e2ff6a937db014887e43/internal/xfn/utils.go

package internal

import (
	"encoding/json"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/unstructured"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	kunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// AsStruct converts the supplied object to a protocol buffer Struct well-known
// type.
func AsStruct(o runtime.Object) (*structpb.Struct, error) {
	// If the supplied object is *Unstructured we don't need to round-trip.
	if u, ok := o.(*kunstructured.Unstructured); ok {
		s, err := structpb.NewStruct(u.Object)
		return s, errors.Wrap(err, "cannot create protobuf Struct")
	}

	// If the supplied object wraps *Unstructured we don't need to round-trip.
	if w, ok := o.(unstructured.Wrapper); ok {
		s, err := structpb.NewStruct(w.GetUnstructured().Object)
		return s, errors.Wrap(err, "cannot create protobuf Struct")
	}

	// Fall back to a JSON round-trip.
	b, err := json.Marshal(o)
	if err != nil {
		return nil, errors.Wrap(err, "cannot marshal object to JSON")
	}

	s := &structpb.Struct{}

	return s, errors.Wrap(s.UnmarshalJSON(b), "cannot unmarshal object from JSON")
}

// FromStruct populates the supplied object with content loaded from the Struct.
func FromStruct(o runtime.Object, s *structpb.Struct) error {
	// If the supplied object is *Unstructured we don't need to round-trip.
	if u, ok := o.(*kunstructured.Unstructured); ok {
		u.Object = s.AsMap()
		return nil
	}

	// If the supplied object wraps *Unstructured we don't need to round-trip.
	if w, ok := o.(unstructured.Wrapper); ok {
		w.GetUnstructured().Object = s.AsMap()
		return nil
	}

	// Fall back to a JSON round-trip.
	b, err := protojson.Marshal(s)
	if err != nil {
		return errors.Wrap(err, "cannot marshal protobuf Struct to JSON")
	}

	return errors.Wrap(json.Unmarshal(b, o), "cannot unmarshal JSON to object")
}
