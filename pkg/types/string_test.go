// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// stringOrPrimitiveIO captures the primitive-specific inputs/outputs for a
// single StringOrPrimitive type parameter. The type-independent behaviour
// (decoding a JSON string, error handling, ...) is asserted by
// runStringOrPrimitiveSuite for every T.
type stringOrPrimitiveIO struct {
	// fromPrimitive is the JSON encoding of a native value of the primitive
	// type parameter, e.g. "true", "42" or "0.5".
	fromPrimitive string
	// canonical is the canonical string form StringOrPrimitive stores for
	// fromPrimitive, i.e. fmt.Sprint(v).
	canonical string
}

// runStringOrPrimitiveSuite exercises UnmarshalJSON and MarshalJSON for a
// single primitive type parameter T, in both directions.
func runStringOrPrimitiveSuite[T Primitive](t *testing.T, io stringOrPrimitiveIO) {
	t.Helper()

	// Unmarshalling a JSON string is type-independent: the raw string is
	// stored verbatim as the canonical value regardless of T.
	t.Run("UnmarshalJSONString", func(t *testing.T) {
		const in = `"already-a-string"`
		var got StringOrPrimitive[T]
		if err := got.UnmarshalJSON([]byte(in)); err != nil {
			t.Fatalf("UnmarshalJSON(%s): unexpected error: %v", in, err)
		}
		if diff := cmp.Diff("already-a-string", string(got)); diff != "" {
			t.Errorf("UnmarshalJSON(%s): -want, +got:\n%s", in, diff)
		}
	})

	// Unmarshalling the native primitive encoding coerces it into its
	// canonical string form.
	t.Run("UnmarshalJSONPrimitive", func(t *testing.T) {
		var got StringOrPrimitive[T]
		if err := got.UnmarshalJSON([]byte(io.fromPrimitive)); err != nil {
			t.Fatalf("UnmarshalJSON(%s): unexpected error: %v", io.fromPrimitive, err)
		}
		if diff := cmp.Diff(io.canonical, string(got)); diff != "" {
			t.Errorf("UnmarshalJSON(%s): -want, +got:\n%s", io.fromPrimitive, diff)
		}
	})

	// Marshalling always emits the canonical string form, never the original
	// primitive encoding.
	t.Run("MarshalJSON", func(t *testing.T) {
		b := StringOrPrimitive[T](io.canonical)
		got, err := b.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON(): unexpected error: %v", err)
		}
		want, err := json.Marshal(io.canonical)
		if err != nil {
			t.Fatalf("json.Marshal(%q): unexpected error: %v", io.canonical, err)
		}
		if diff := cmp.Diff(string(want), string(got)); diff != "" {
			t.Errorf("MarshalJSON(): -want, +got:\n%s", diff)
		}
	})

	// A canonical value survives a marshal -> unmarshal round-trip unchanged.
	t.Run("RoundTrip", func(t *testing.T) {
		want := StringOrPrimitive[T](io.canonical)
		data, err := want.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON(): unexpected error: %v", err)
		}
		var got StringOrPrimitive[T]
		if err := got.UnmarshalJSON(data); err != nil {
			t.Fatalf("UnmarshalJSON(%s): unexpected error: %v", data, err)
		}
		if diff := cmp.Diff(string(want), string(got)); diff != "" {
			t.Errorf("round-trip: -want, +got:\n%s", diff)
		}
	})
}

// TestStringOrPrimitive covers every primitive type in the Primitive
// constraint (plus a named ~float64 type) in both directions.
func TestStringOrPrimitive(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"Bool": func(t *testing.T) {
			runStringOrPrimitiveSuite[bool](t, stringOrPrimitiveIO{fromPrimitive: "true", canonical: "true"})
		},
		"Int": func(t *testing.T) {
			runStringOrPrimitiveSuite[int](t, stringOrPrimitiveIO{fromPrimitive: "-1", canonical: "-1"})
		},
		"Int8": func(t *testing.T) {
			runStringOrPrimitiveSuite[int8](t, stringOrPrimitiveIO{fromPrimitive: "-128", canonical: "-128"})
		},
		"Int16": func(t *testing.T) {
			runStringOrPrimitiveSuite[int16](t, stringOrPrimitiveIO{fromPrimitive: "32767", canonical: "32767"})
		},
		"Int32": func(t *testing.T) {
			runStringOrPrimitiveSuite[int32](t, stringOrPrimitiveIO{fromPrimitive: "-2147483648", canonical: "-2147483648"})
		},
		"Int64": func(t *testing.T) {
			runStringOrPrimitiveSuite[int64](t, stringOrPrimitiveIO{fromPrimitive: "9223372036854775807", canonical: "9223372036854775807"})
		},
		"Uint": func(t *testing.T) {
			runStringOrPrimitiveSuite[uint](t, stringOrPrimitiveIO{fromPrimitive: "1", canonical: "1"})
		},
		"Uint8": func(t *testing.T) {
			runStringOrPrimitiveSuite[uint8](t, stringOrPrimitiveIO{fromPrimitive: "255", canonical: "255"})
		},
		"Uint16": func(t *testing.T) {
			runStringOrPrimitiveSuite[uint16](t, stringOrPrimitiveIO{fromPrimitive: "65535", canonical: "65535"})
		},
		"Uint32": func(t *testing.T) {
			runStringOrPrimitiveSuite[uint32](t, stringOrPrimitiveIO{fromPrimitive: "4294967295", canonical: "4294967295"})
		},
		"Uint64": func(t *testing.T) {
			runStringOrPrimitiveSuite[uint64](t, stringOrPrimitiveIO{fromPrimitive: "18446744073709551615", canonical: "18446744073709551615"})
		},
	}
	for name, run := range cases {
		t.Run(name, run)
	}
}

// TestStringOrPrimitiveUnmarshalJSONEdgeCases covers UnmarshalJSON inputs whose
// handling does not depend on the primitive type parameter; int is used as a
// representative T.
func TestStringOrPrimitiveUnmarshalJSONEdgeCases(t *testing.T) {
	cases := map[string]struct {
		reason string
		data   string
		want   string
	}{
		"EmptyString": {
			reason: "An empty JSON string decodes to an empty canonical value.",
			data:   `""`,
			want:   "",
		},
		"Null": {
			reason: "A JSON null is a no-op for the string decode path and yields an empty canonical value.",
			data:   `null`,
			want:   "",
		},
		"EscapedString": {
			reason: "Escape sequences in a JSON string are decoded before being stored.",
			data:   `"a\"b\tc"`,
			want:   "a\"b\tc",
		},
		"WhitespacePreserved": {
			reason: "Surrounding whitespace within a JSON string is preserved.",
			data:   `"  spaced  "`,
			want:   "  spaced  ",
		},
		"NumericString": {
			reason: "A quoted number is treated as a string and stored verbatim.",
			data:   `"42"`,
			want:   "42",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got StringOrPrimitive[int]
			if err := got.UnmarshalJSON([]byte(tc.data)); err != nil {
				t.Fatalf("UnmarshalJSON(%s): unexpected error: %v\nreason: %s", tc.data, err, tc.reason)
			}
			if diff := cmp.Diff(tc.want, string(got)); diff != "" {
				t.Errorf("UnmarshalJSON(%s): -want, +got:\n%s\nreason: %s", tc.data, diff, tc.reason)
			}
		})
	}
}

// TestStringOrPrimitiveUnmarshalJSONErrors covers inputs that are neither a
// JSON string nor a valid value of the type parameter; int is used as a
// representative T.
func TestStringOrPrimitiveUnmarshalJSONErrors(t *testing.T) {
	cases := map[string]struct {
		reason string
		data   string
	}{
		"JSONArray": {
			reason: "A JSON array is neither a string nor an int.",
			data:   `[1,2,3]`,
		},
		"JSONObject": {
			reason: "A JSON object is neither a string nor an int.",
			data:   `{"a":1}`,
		},
		"TypeMismatch": {
			reason: "A JSON boolean cannot be decoded into an int type parameter.",
			data:   `true`,
		},
		"FloatIntoInt": {
			reason: "A JSON floating-point number cannot be decoded into an int type parameter.",
			data:   `1.5`,
		},
		"MalformedJSON": {
			reason: "Malformed JSON fails both the string and the primitive decode attempts.",
			data:   `{`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got StringOrPrimitive[int]
			err := got.UnmarshalJSON([]byte(tc.data))
			if err == nil {
				t.Fatalf("UnmarshalJSON(%s): expected an error, got nil\nreason: %s", tc.data, tc.reason)
			}
			if want := "value must be a JSON string"; !strings.Contains(err.Error(), want) {
				t.Errorf("UnmarshalJSON(%s): error %q does not contain %q\nreason: %s", tc.data, err.Error(), want, tc.reason)
			}
		})
	}
}
