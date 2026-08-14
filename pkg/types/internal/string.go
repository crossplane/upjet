// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package internal

import (
	"encoding/json"
	"fmt"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
)

const (
	errInvalidValue = "value must be a JSON string or %T, got %s"
)

// Primitive is a type constraint that represents the supported primitive types
// for StringOrPrimitive.
type Primitive interface {
	~bool |
		~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// StringOrPrimitive is stored canonically as a string, but can decode
// from other primitive values such as ints and bools.
type StringOrPrimitive[T Primitive] string

func (b *StringOrPrimitive[T]) UnmarshalJSON(data []byte) error {
	// first try as a string value.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*b = StringOrPrimitive[T](s)
		return nil
	}

	// if not a string value, try as a value of the specified type parameter.
	var v T
	if err := json.Unmarshal(data, &v); err == nil {
		*b = StringOrPrimitive[T](fmt.Sprint(v))
		return nil
	}

	return errors.Errorf(errInvalidValue, v, string(data))
}

func (b *StringOrPrimitive[T]) MarshalJSON() ([]byte, error) {
	// Always write back as string in the new canonical format.
	return json.Marshal(*b)
}
