// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"encoding/json"
	"go/types"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestStringOrBoolUnmarshalJSON covers decoding into StringOrBool, which accepts
// either a JSON string (stored verbatim) or a JSON boolean (coerced to its
// canonical string form), and errors on anything else.
func TestStringOrBoolUnmarshalJSON(t *testing.T) {
	type want struct {
		val StringOrBool
		err bool
	}
	cases := map[string]struct {
		reason string
		data   string
		want   want
	}{
		"StringTrue": {
			reason: "A JSON string is stored verbatim, even when its content looks like a boolean.",
			data:   `"true"`,
			want:   want{val: "true"},
		},
		"StringFalse": {
			reason: "A JSON string is stored verbatim, even when its content looks like a boolean.",
			data:   `"false"`,
			want:   want{val: "false"},
		},
		"StringArbitrary": {
			reason: "Any JSON string content is stored verbatim.",
			data:   `"hello world"`,
			want:   want{val: "hello world"},
		},
		"EmptyString": {
			reason: "An empty JSON string decodes to an empty canonical value.",
			data:   `""`,
			want:   want{val: ""},
		},
		"BoolTrue": {
			reason: "A JSON boolean true is coerced to its canonical string form.",
			data:   `true`,
			want:   want{val: "true"},
		},
		"BoolFalse": {
			reason: "A JSON boolean false is coerced to its canonical string form.",
			data:   `false`,
			want:   want{val: "false"},
		},
		"Null": {
			reason: "A JSON null is a no-op for the string decode path and yields an empty canonical value.",
			data:   `null`,
			want:   want{val: ""},
		},
		"Number": {
			reason: "A JSON number is neither a string nor a boolean.",
			data:   `42`,
			want:   want{err: true},
		},
		"Float": {
			reason: "A JSON floating-point number is neither a string nor a boolean.",
			data:   `1.5`,
			want:   want{err: true},
		},
		"Array": {
			reason: "A JSON array is neither a string nor a boolean.",
			data:   `[true]`,
			want:   want{err: true},
		},
		"Object": {
			reason: "A JSON object is neither a string nor a boolean.",
			data:   `{"a":true}`,
			want:   want{err: true},
		},
		"Malformed": {
			reason: "Malformed JSON fails both the string and the boolean decode attempts.",
			data:   `{`,
			want:   want{err: true},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got StringOrBool
			err := got.UnmarshalJSON([]byte(tc.data))
			if tc.want.err {
				if err == nil {
					t.Fatalf("UnmarshalJSON(%s): expected an error, got nil\nreason: %s", tc.data, tc.reason)
				}
				// The error originates from the delegated generic decoder and
				// must identify the accepted boolean type parameter.
				if wantSub := "value must be a JSON string or bool"; !strings.Contains(err.Error(), wantSub) {
					t.Errorf("UnmarshalJSON(%s): error %q does not contain %q\nreason: %s", tc.data, err.Error(), wantSub, tc.reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalJSON(%s): unexpected error: %v\nreason: %s", tc.data, err, tc.reason)
			}
			if diff := cmp.Diff(tc.want.val, got); diff != "" {
				t.Errorf("UnmarshalJSON(%s): -want, +got:\n%s\nreason: %s", tc.data, diff, tc.reason)
			}
		})
	}
}

// TestStringOrBoolUnmarshalJSONOverwrite verifies that decoding always replaces
// any pre-existing value, including the null case which clears it.
func TestStringOrBoolUnmarshalJSONOverwrite(t *testing.T) {
	cases := map[string]struct {
		reason string
		data   string
		want   StringOrBool
	}{
		"BoolOverwrites": {
			reason: "Decoding a boolean overwrites the previous value.",
			data:   `false`,
			want:   "false",
		},
		"NullClears": {
			reason: "Decoding a JSON null clears the previous value to empty.",
			data:   `null`,
			want:   "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := StringOrBool("preset")
			if err := got.UnmarshalJSON([]byte(tc.data)); err != nil {
				t.Fatalf("UnmarshalJSON(%s): unexpected error: %v\nreason: %s", tc.data, err, tc.reason)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("UnmarshalJSON(%s): -want, +got:\n%s\nreason: %s", tc.data, diff, tc.reason)
			}
		})
	}
}

// TestStringOrBoolMarshalJSON covers encoding, which always emits the canonical
// string form regardless of the stored content.
func TestStringOrBoolMarshalJSON(t *testing.T) {
	cases := map[string]struct {
		reason string
		val    StringOrBool
		want   string
	}{
		"True": {
			reason: "The canonical boolean value is emitted as a JSON string.",
			val:    "true",
			want:   `"true"`,
		},
		"False": {
			reason: "The canonical boolean value is emitted as a JSON string.",
			val:    "false",
			want:   `"false"`,
		},
		"Empty": {
			reason: "An empty value is emitted as an empty JSON string.",
			val:    "",
			want:   `""`,
		},
		"Arbitrary": {
			reason: "Arbitrary content is emitted verbatim as a JSON string.",
			val:    "custom",
			want:   `"custom"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.val.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON(): unexpected error: %v\nreason: %s", err, tc.reason)
			}
			if diff := cmp.Diff(tc.want, string(got)); diff != "" {
				t.Errorf("MarshalJSON(): -want, +got:\n%s\nreason: %s", diff, tc.reason)
			}
		})
	}
}

// TestStringOrBoolJSONRoundTrip exercises the type through the standard encoding/json
// package via a pointer struct field, mirroring how the generated CRD types use it.
// It confirms that a JSON boolean is canonicalized to a JSON string on the way back out.
func TestStringOrBoolJSONRoundTrip(t *testing.T) {
	type holder struct {
		V *StringOrBool `json:"v"`
	}
	cases := map[string]struct {
		reason string
		in     string
		want   string
	}{
		"BoolTrueBecomesString": {
			reason: "A JSON boolean true is canonicalized to a JSON string on re-marshal.",
			in:     `{"v":true}`,
			want:   `{"v":"true"}`,
		},
		"BoolFalseBecomesString": {
			reason: "A JSON boolean false is canonicalized to a JSON string on re-marshal.",
			in:     `{"v":false}`,
			want:   `{"v":"false"}`,
		},
		"StringPreserved": {
			reason: "A JSON string round-trips unchanged.",
			in:     `{"v":"custom"}`,
			want:   `{"v":"custom"}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var h holder
			if err := json.Unmarshal([]byte(tc.in), &h); err != nil {
				t.Fatalf("json.Unmarshal(%s): unexpected error: %v\nreason: %s", tc.in, err, tc.reason)
			}
			got, err := json.Marshal(h)
			if err != nil {
				t.Fatalf("json.Marshal(): unexpected error: %v\nreason: %s", err, tc.reason)
			}
			if diff := cmp.Diff(tc.want, string(got)); diff != "" {
				t.Errorf("round-trip(%s): -want, +got:\n%s\nreason: %s", tc.in, diff, tc.reason)
			}
		})
	}
}

// TestNewStringOrBoolType verifies the go/types.Type emitted for overridden
// fields: a pointer to a named "StringOrBool" type, declared in this package,
// whose underlying type is string.
func TestNewStringOrBoolType(t *testing.T) {
	got := NewStringOrBoolType()

	if diff := cmp.Diff("*"+packagePath+".StringOrBool", got.String()); diff != "" {
		t.Errorf("NewStringOrBoolType(): string representation: -want, +got:\n%s", diff)
	}

	ptr, ok := got.(*types.Pointer)
	if !ok {
		t.Fatalf("NewStringOrBoolType(): want *types.Pointer, got %T", got)
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok {
		t.Fatalf("NewStringOrBoolType(): pointer element: want *types.Named, got %T", ptr.Elem())
	}
	if diff := cmp.Diff("StringOrBool", named.Obj().Name()); diff != "" {
		t.Errorf("NewStringOrBoolType(): type name: -want, +got:\n%s", diff)
	}
	if diff := cmp.Diff(packagePath, named.Obj().Pkg().Path()); diff != "" {
		t.Errorf("NewStringOrBoolType(): package path: -want, +got:\n%s", diff)
	}
	if diff := cmp.Diff(packageName, named.Obj().Pkg().Name()); diff != "" {
		t.Errorf("NewStringOrBoolType(): package name: -want, +got:\n%s", diff)
	}
	if diff := cmp.Diff("string", named.Underlying().String()); diff != "" {
		t.Errorf("NewStringOrBoolType(): underlying type: -want, +got:\n%s", diff)
	}
}
