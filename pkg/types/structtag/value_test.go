// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package structtag

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
)

func TestParseJSON(t *testing.T) {
	type args struct {
		value string
	}
	type want struct {
		v   *Value
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"Empty": {
			reason: "An empty string should parse successfully as a valid JSON tag value.",
			args: args{
				value: "",
			},
			want: want{
				v: &Value{
					key: KeyJSON,
				},
			},
		},
		"NameOnly": {
			reason: "A JSON tag with only a field name should parse successfully without any options.",
			args: args{
				value: "fieldName",
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					name: "fieldName",
				},
			},
		},
		"NameWithOmitEmpty": {
			reason: "A JSON tag with a field name and omitempty option should parse both correctly.",
			args: args{
				value: "fieldName,omitempty",
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					name: "fieldName",
					omit: OmitEmpty,
				},
			},
		},
		"OmitEmptyOnly": {
			reason: "A JSON tag with only the omitempty option (no field name) should parse successfully.",
			args: args{
				value: ",omitempty",
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					omit: OmitEmpty,
				},
			},
		},
		"OmitAlways": {
			reason: "A JSON tag with only a dash should parse as always omitted.",
			args: args{
				value: "-",
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					omit: OmitAlways,
				},
			},
		},
		"InlineOnly": {
			reason: "A JSON tag with only the inline option should parse with the inline flag set to true.",
			args: args{
				value: ",inline",
			},
			want: want{
				v: &Value{
					key:    KeyJSON,
					inline: true,
				},
			},
		},
		"WithWhitespace": {
			reason: "A JSON tag with surrounding and embedded whitespace should be trimmed and parsed correctly.",
			args: args{
				value: " fieldName , omitempty ",
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					name: "fieldName",
					omit: OmitEmpty,
				},
			},
		},
		"UnknownOption": {
			reason: "Unknown options in a JSON tag should be silently ignored while known options are parsed.",
			args: args{
				value: "fieldName,omitempty,unknownopt",
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					name: "fieldName",
					omit: OmitEmpty,
				},
			},
		},
		"ErrorOmitAlwaysWithOptions": {
			reason: "A JSON tag combining dash with other options should return an error.",
			args: args{
				value: "-,omitempty",
			},
			want: want{
				err: errors.New(`invalid struct tag format: "-,omitempty" (cannot combine "-" with other options)`),
			},
		},
		"ErrorNameWithInline": {
			reason: "A JSON tag combining a field name with the inline option should return an error.",
			args: args{
				value: "fieldName,inline",
			},
			want: want{
				err: errors.New(`invalid struct tag format: "fieldName,inline" (cannot combine name with inline)`),
			},
		},
		"ErrorOmitEmptyWithInline": {
			reason: "A JSON tag combining the inline option with omitempty should return an error.",
			args: args{
				value: ",omitempty,inline",
			},
			want: want{
				err: errors.New(`invalid struct tag format: ",omitempty,inline" (cannot combine inline with omitempty)`),
			},
		},
		"ErrorInvalidNameWithSpace": {
			reason: "A JSON tag with a field name containing spaces should return a validation error.",
			args: args{
				value: "field name",
			},
			want: want{
				err: errors.New(`invalid JSON struct tag name: "field name" (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)`),
			},
		},
		"ErrorInvalidNameStartsWithDigit": {
			reason: "A JSON tag with a field name starting with a digit should return a validation error.",
			args: args{
				value: "123field",
			},
			want: want{
				err: errors.New(`invalid JSON struct tag name: "123field" (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)`),
			},
		},
		"ErrorInvalidNameWithSpecialChar": {
			reason: "A JSON tag with a field name containing special characters should return a validation error.",
			args: args{
				value: "field@name",
			},
			want: want{
				err: errors.New(`invalid JSON struct tag name: "field@name" (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)`),
			},
		},
		"ErrorInvalidNameWithDot": {
			reason: "A JSON tag with a field name containing a dot should return a validation error.",
			args: args{
				value: "field.name",
			},
			want: want{
				err: errors.New(`invalid JSON struct tag name: "field.name" (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)`),
			},
		},
		"ErrorInvalidNameWithHyphen": {
			reason: "A JSON tag with a field name containing hyphens should return a validation error as hyphens are not allowed in Kubernetes CRD field names.",
			args: args{
				value: "field-name",
			},
			want: want{
				err: errors.New(`invalid JSON struct tag name: "field-name" (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)`),
			},
		},
		"ValidNameWithUnderscore": {
			reason: "A JSON tag with a field name containing underscores should be accepted as valid.",
			args: args{
				value: "field_name",
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					name: "field_name",
				},
			},
		},
		"ValidNameStartsWithUnderscore": {
			reason: "A JSON tag with a field name starting with an underscore should be accepted as valid.",
			args: args{
				value: "_fieldName",
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					name: "_fieldName",
				},
			},
		},
		"ValidNameWithDigits": {
			reason: "A JSON tag with a field name containing digits (but not starting with them) should be accepted as valid.",
			args: args{
				value: "field123",
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					name: "field123",
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := ParseJSON(tc.value)

			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("\n%s\nParseJSON(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if gotErr != nil {
				return
			}

			if diff := cmp.Diff(tc.want.v, got, cmp.AllowUnexported(Value{})); diff != "" {
				t.Errorf("\n%s\nParseJSON(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestParseTF(t *testing.T) {
	type args struct {
		value string
	}
	type want struct {
		v   *Value
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"Empty": {
			reason: "An empty string should parse successfully as a valid TF tag value.",
			args: args{
				value: "",
			},
			want: want{
				v: &Value{
					key: KeyTF,
				},
			},
		},
		"NameOnly": {
			reason: "A TF tag with only a field name should parse successfully without any options.",
			args: args{
				value: "tf_field_name",
			},
			want: want{
				v: &Value{
					key:  KeyTF,
					name: "tf_field_name",
				},
			},
		},
		"NameWithOmitEmpty": {
			reason: "A TF tag with a field name and omitempty option should parse both correctly.",
			args: args{
				value: "tf_field_name,omitempty",
			},
			want: want{
				v: &Value{
					key:  KeyTF,
					name: "tf_field_name",
					omit: OmitEmpty,
				},
			},
		},
		"NameWithHyphen": {
			reason: "A TF tag with a field name containing hyphens should parse successfully as TF tags are not validated.",
			args: args{
				value: "field-name",
			},
			want: want{
				v: &Value{
					key:  KeyTF,
					name: "field-name",
				},
			},
		},
		"NameWithSpecialChar": {
			reason: "A TF tag with a field name containing special characters should parse successfully as TF tags are not validated.",
			args: args{
				value: "field@name",
			},
			want: want{
				v: &Value{
					key:  KeyTF,
					name: "field@name",
				},
			},
		},
		"NameStartingWithDigit": {
			reason: "A TF tag with a field name starting with a digit should parse successfully as TF tags are not validated.",
			args: args{
				value: "123field",
			},
			want: want{
				v: &Value{
					key:  KeyTF,
					name: "123field",
				},
			},
		},
		"OmitAlways": {
			reason: "A TF tag with only a dash should parse as always omitted.",
			args: args{
				value: "-",
			},
			want: want{
				v: &Value{
					key:  KeyTF,
					omit: OmitAlways,
				},
			},
		},
		"ErrorNameWithInline": {
			reason: "A TF tag combining a field name with the inline option should return an error.",
			args: args{
				value: "fieldName,inline",
			},
			want: want{
				err: errors.New(`invalid struct tag format: "fieldName,inline" (cannot combine name with inline)`),
			},
		},
		"ErrorOmitEmptyWithInline": {
			reason: "A TF tag combining the inline option with omitempty should return an error.",
			args: args{
				value: ",omitempty,inline",
			},
			want: want{
				err: errors.New(`invalid struct tag format: ",omitempty,inline" (cannot combine inline with omitempty)`),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, gotErr := ParseTF(tc.value)

			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("\n%s\nParseTF(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if gotErr != nil {
				return
			}

			if diff := cmp.Diff(tc.want.v, got, cmp.AllowUnexported(Value{})); diff != "" {
				t.Errorf("\n%s\nParseTF(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestNewJSON(t *testing.T) {
	type args struct {
		opts []Option
	}
	type want struct {
		v *Value
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NoOptions": {
			reason: "Creating a JSON tag value with no options should return a minimal value with only the key set.",
			args: args{
				opts: nil,
			},
			want: want{
				v: &Value{
					key: KeyJSON,
				},
			},
		},
		"WithName": {
			reason: "Creating a JSON tag value with a custom name should set the name field correctly.",
			args: args{
				opts: []Option{
					WithName("customName"),
				},
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					name: "customName",
				},
			},
		},
		"WithOmitEmpty": {
			reason: "Creating a JSON tag value with the omit empty option should set the omit field to OmitEmpty.",
			args: args{
				opts: []Option{
					WithOmit(OmitEmpty),
				},
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					omit: OmitEmpty,
				},
			},
		},
		"WithInline": {
			reason: "Creating a JSON tag value with the inline option should set the inline flag to true.",
			args: args{
				opts: []Option{
					WithInline(true),
				},
			},
			want: want{
				v: &Value{
					key:    KeyJSON,
					inline: true,
				},
			},
		},
		"WithMultipleOptions": {
			reason: "Creating a JSON tag value with multiple options should correctly apply all of them.",
			args: args{
				opts: []Option{
					WithName("fieldName"),
					WithOmit(OmitEmpty),
				},
			},
			want: want{
				v: &Value{
					key:  KeyJSON,
					name: "fieldName",
					omit: OmitEmpty,
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := NewJSON(tc.opts...)

			if diff := cmp.Diff(tc.want.v, got, cmp.AllowUnexported(Value{})); diff != "" {
				t.Errorf("\n%s\nNewJSON(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestNewTF(t *testing.T) {
	type args struct {
		opts []Option
	}
	type want struct {
		v *Value
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NoOptions": {
			reason: "Creating a TF tag value with no options should return a minimal value with only the key set.",
			args: args{
				opts: nil,
			},
			want: want{
				v: &Value{
					key: KeyTF,
				},
			},
		},
		"WithName": {
			reason: "Creating a TF tag value with a custom name should set the name field correctly.",
			args: args{
				opts: []Option{
					WithName("tf_custom_name"),
				},
			},
			want: want{
				v: &Value{
					key:  KeyTF,
					name: "tf_custom_name",
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := NewTF(tc.opts...)

			if diff := cmp.Diff(tc.want.v, got, cmp.AllowUnexported(Value{})); diff != "" {
				t.Errorf("\n%s\nNewTF(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestValueGetters(t *testing.T) {
	v := &Value{
		key:    KeyJSON,
		name:   "testField",
		omit:   OmitEmpty,
		inline: true,
	}

	if got := v.Key(); got != KeyJSON {
		t.Errorf("Key() = %v, want %v", got, KeyJSON)
	}

	if got := v.Name(); got != "testField" {
		t.Errorf("Name() = %v, want %v", got, "testField")
	}

	if got := v.Omit(); got != OmitEmpty {
		t.Errorf("Omit() = %v, want %v", got, OmitEmpty)
	}

	if got := v.Inline(); got != true {
		t.Errorf("Inline() = %v, want %v", got, true)
	}
}

func TestValueAlwaysOmitted(t *testing.T) {
	cases := map[string]struct {
		reason string
		v      *Value
		want   bool
	}{
		"OmitAlways": {
			reason: "A value with OmitAlways should return true from AlwaysOmitted.",
			v: &Value{
				key:  KeyJSON,
				omit: OmitAlways,
			},
			want: true,
		},
		"OmitEmpty": {
			reason: "A value with OmitEmpty should return false from AlwaysOmitted as it is conditionally omitted.",
			v: &Value{
				key:  KeyJSON,
				omit: OmitEmpty,
			},
			want: false,
		},
		"NotOmitted": {
			reason: "A value with no omit setting should return false from AlwaysOmitted.",
			v: &Value{
				key: KeyJSON,
			},
			want: false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.v.AlwaysOmitted()

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nAlwaysOmitted(): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestValueSetOmit(t *testing.T) {
	v := &Value{
		key:  KeyJSON,
		name: "testField",
	}

	v.SetOmit(OmitEmpty)

	if v.omit != OmitEmpty {
		t.Errorf("SetOmit(OmitEmpty): omit = %v, want %v", v.omit, OmitEmpty)
	}

	v.SetOmit(OmitAlways)

	if v.omit != OmitAlways {
		t.Errorf("SetOmit(OmitAlways): omit = %v, want %v", v.omit, OmitAlways)
	}
}

func TestValueNoOmit(t *testing.T) {
	cases := map[string]struct {
		reason string
		v      *Value
		want   *Value
	}{
		"OmitAlwaysToNotOmitted": {
			reason: "NoOmit should convert a value with OmitAlways to NotOmitted.",
			v: &Value{
				key:  KeyJSON,
				name: "fieldName",
				omit: OmitAlways,
			},
			want: &Value{
				key:  KeyJSON,
				name: "fieldName",
				omit: NotOmitted,
			},
		},
		"OmitEmptyToNotOmitted": {
			reason: "NoOmit should convert a value with OmitEmpty to NotOmitted.",
			v: &Value{
				key:  KeyJSON,
				name: "fieldName",
				omit: OmitEmpty,
			},
			want: &Value{
				key:  KeyJSON,
				name: "fieldName",
				omit: NotOmitted,
			},
		},
		"PreservesOtherFields": {
			reason: "NoOmit should preserve all other fields while only changing the omit setting.",
			v: &Value{
				key:    KeyTF,
				name:   "tf_field",
				omit:   OmitEmpty,
				inline: true,
			},
			want: &Value{
				key:    KeyTF,
				name:   "tf_field",
				omit:   NotOmitted,
				inline: true,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.v.NoOmit()

			// Verify result
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(Value{})); diff != "" {
				t.Errorf("\n%s\nNoOmit(): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestValueStringWithoutKey(t *testing.T) {
	cases := map[string]struct {
		reason string
		v      *Value
		want   string
	}{
		"Empty": {
			reason: "An empty value should serialize to an empty string without the key.",
			v: &Value{
				key: KeyJSON,
			},
			want: "",
		},
		"NameOnly": {
			reason: "A value with only a name should serialize to just the name without the key.",
			v: &Value{
				key:  KeyJSON,
				name: "fieldName",
			},
			want: "fieldName",
		},
		"NameWithOmitEmpty": {
			reason: "A value with name and omitempty should serialize correctly without the key.",
			v: &Value{
				key:  KeyJSON,
				name: "fieldName",
				omit: OmitEmpty,
			},
			want: "fieldName,omitempty",
		},
		"OmitEmptyOnly": {
			reason: "A value with only omitempty should serialize with a leading comma without the key.",
			v: &Value{
				key:  KeyJSON,
				omit: OmitEmpty,
			},
			want: ",omitempty",
		},
		"OmitAlways": {
			reason: "A value with OmitAlways should serialize to a dash without the key.",
			v: &Value{
				key:  KeyJSON,
				omit: OmitAlways,
			},
			want: "-",
		},
		"InlineOnly": {
			reason: "A value with only inline should serialize with a leading comma without the key.",
			v: &Value{
				key:    KeyJSON,
				inline: true,
			},
			want: ",inline",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.v.StringWithoutKey()

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nStringWithoutKey(): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestValueString(t *testing.T) {
	cases := map[string]struct {
		reason string
		v      *Value
		want   string
	}{
		"JSONEmpty": {
			reason: "An empty JSON value should serialize to an empty quoted struct tag.",
			v: &Value{
				key: KeyJSON,
			},
			want: `json:""`,
		},
		"JSONNameOnly": {
			reason: "A JSON value with only a name should serialize with the key and name.",
			v: &Value{
				key:  KeyJSON,
				name: "fieldName",
			},
			want: `json:"fieldName"`,
		},
		"JSONNameWithOmitEmpty": {
			reason: "A JSON value with name and omitempty should serialize as a complete struct tag.",
			v: &Value{
				key:  KeyJSON,
				name: "fieldName",
				omit: OmitEmpty,
			},
			want: `json:"fieldName,omitempty"`,
		},
		"JSONOmitEmptyOnly": {
			reason: "A JSON value with only omitempty should serialize with a leading comma in the value.",
			v: &Value{
				key:  KeyJSON,
				omit: OmitEmpty,
			},
			want: `json:",omitempty"`,
		},
		"JSONOmitAlways": {
			reason: "A JSON value with OmitAlways should serialize to json:\"-\".",
			v: &Value{
				key:  KeyJSON,
				omit: OmitAlways,
			},
			want: `json:"-"`,
		},
		"JSONInline": {
			reason: "A JSON value with inline should serialize with the inline option.",
			v: &Value{
				key:    KeyJSON,
				inline: true,
			},
			want: `json:",inline"`,
		},
		"TFNameWithOmitEmpty": {
			reason: "A TF value with name and omitempty should serialize as a complete tf struct tag.",
			v: &Value{
				key:  KeyTF,
				name: "tf_field_name",
				omit: OmitEmpty,
			},
			want: `tf:"tf_field_name,omitempty"`,
		},
		"TFOmitAlways": {
			reason: "A TF value with OmitAlways should serialize to tf:\"-\".",
			v: &Value{
				key:  KeyTF,
				omit: OmitAlways,
			},
			want: `tf:"-"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.v.String()

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nString(): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestValueRoundTrip(t *testing.T) {
	cases := map[string]struct {
		reason string
		input  string
	}{
		"Empty":         {reason: "Parsing and serializing an empty tag should preserve the empty string.", input: ""},
		"Name":          {reason: "Parsing and serializing a simple name should preserve the name.", input: "fieldName"},
		"NameOmitEmpty": {reason: "Parsing and serializing a name with omitempty should preserve both.", input: "fieldName,omitempty"},
		"OmitEmptyOnly": {reason: "Parsing and serializing only omitempty should preserve the format.", input: ",omitempty"},
		"OmitAlways":    {reason: "Parsing and serializing a dash should preserve the dash.", input: "-"},
		"Inline":        {reason: "Parsing and serializing inline should preserve the inline option.", input: ",inline"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			parsed, err := ParseJSON(tc.input)
			if err != nil {
				t.Fatalf("\n%s\nParseJSON(%q) failed: %v", tc.reason, tc.input, err)
			}

			got := parsed.StringWithoutKey()

			if diff := cmp.Diff(tc.input, got); diff != "" {
				t.Errorf("\n%s\nRoundTrip: -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestMustParseTF(t *testing.T) {
	cases := map[string]struct {
		reason    string
		value     string
		want      *Value
		mustPanic bool
		panicMsg  string
	}{
		"InvalidTFTagNameWithInline": {
			reason:    "MustParseTF should panic when TF tag combines name with inline.",
			value:     "fieldName,inline",
			mustPanic: true,
			panicMsg:  `invalid struct tag format: "fieldName,inline" (cannot combine name with inline)`,
		},
		"InvalidTFTagInlineWithOmitEmpty": {
			reason:    "MustParseTF should panic when TF tag combines inline with omitempty.",
			value:     ",inline,omitempty",
			mustPanic: true,
			panicMsg:  `invalid struct tag format: ",inline,omitempty" (cannot combine inline with omitempty)`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.mustPanic {
				defer func() {
					r := recover()
					if r == nil {
						t.Fatalf("\n%s\nexpected panic but got none", tc.reason)
					}
					panicMsg, ok := r.(string)
					if !ok {
						t.Fatalf("\n%s\nexpected string panic message, got %T: %v", tc.reason, r, r)
					}
					if panicMsg != tc.panicMsg {
						t.Errorf("\n%s\npanic message: -want, +got:\n%s\n%s", tc.reason, tc.panicMsg, panicMsg)
					}
				}()
			}

			got := MustParseTF(tc.value)

			if !tc.mustPanic {
				if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(Value{})); diff != "" {
					t.Errorf("\n%s\nMustParseTF(...): -want, +got:\n%s", tc.reason, diff)
				}
			}
		})
	}
}

func TestBuildPanics(t *testing.T) {
	cases := map[string]struct {
		reason    string
		key       Key
		opts      []Option
		mustPanic bool
		panicMsg  string
	}{
		"JSONInvalidNameWithSpace": {
			reason:    "Building a JSON value with an invalid name containing spaces should panic.",
			key:       KeyJSON,
			mustPanic: true,
			opts: []Option{
				WithName("field name"),
			},
			panicMsg: `invalid JSON struct tag name: "field name" (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)`,
		},
		"JSONInvalidNameStartingWithDigit": {
			reason:    "Building a JSON value with an invalid name starting with a digit should panic.",
			key:       KeyJSON,
			mustPanic: true,
			opts: []Option{
				WithName("123field"),
			},
			panicMsg: `invalid JSON struct tag name: "123field" (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)`,
		},
		"JSONInvalidNameWithSpecialChar": {
			reason:    "Building a JSON value with an invalid name containing special characters should panic.",
			key:       KeyJSON,
			mustPanic: true,
			opts: []Option{
				WithName("field@name"),
			},
			panicMsg: `invalid JSON struct tag name: "field@name" (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)`,
		},
		"JSONInvalidNameWithHyphen": {
			reason:    "Building a JSON value with an invalid name containing hyphens should panic as hyphens are not allowed in Kubernetes CRD field names.",
			key:       KeyJSON,
			mustPanic: true,
			opts: []Option{
				WithName("field-name"),
			},
			panicMsg: `invalid JSON struct tag name: "field-name" (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)`,
		},
		"JSONNameWithInline": {
			reason:    "Building a JSON value combining a name with inline should panic due to incompatibility.",
			key:       KeyJSON,
			mustPanic: true,
			opts: []Option{
				WithName("fieldName"),
				WithInline(true),
			},
			panicMsg: `invalid struct tag: cannot set the name "fieldName" with inline`,
		},
		"JSONInlineWithOmitEmpty": {
			reason:    "Building a JSON value combining inline with OmitEmpty should panic due to incompatibility.",
			key:       KeyJSON,
			mustPanic: true,
			opts: []Option{
				WithInline(true),
				WithOmit(OmitEmpty),
			},
			panicMsg: `invalid struct tag: cannot set inline with omit option "omitempty"`,
		},
		"JSONInlineWithOmitAlways": {
			reason:    "Building a JSON value combining inline with OmitAlways should panic due to incompatibility.",
			key:       KeyJSON,
			mustPanic: true,
			opts: []Option{
				WithInline(true),
				WithOmit(OmitAlways),
			},
			panicMsg: `invalid struct tag: cannot set inline with omit option "-"`,
		},
		"TFNameStartingWithDigit": {
			reason:    "Building a TF value with a name starting with a digit should not panic as TF tags are not validated.",
			key:       KeyTF,
			mustPanic: false,
			opts: []Option{
				WithName("123field"),
			},
		},
		"TFNameWithHyphen": {
			reason:    "Building a TF value with a name containing hyphens should not panic as TF tags are not validated.",
			key:       KeyTF,
			mustPanic: false,
			opts: []Option{
				WithName("field-name"),
			},
		},
		"TFNameWithDot": {
			reason:    "Building a TF value with a name containing dots should not panic as TF tags are not validated.",
			key:       KeyTF,
			mustPanic: false,
			opts: []Option{
				WithName("field.name"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r := recover()
				if tc.mustPanic {
					if r == nil {
						t.Fatalf("\n%s\nexpected panic but got none", tc.reason)
					}
					panicMsg, ok := r.(string)
					if !ok {
						t.Fatalf("\n%s\nexpected string panic message, got %T: %v", tc.reason, r, r)
					}
					if panicMsg != tc.panicMsg {
						t.Errorf("\n%s\npanic message: -want, +got:\n%s\n%s", tc.reason, tc.panicMsg, panicMsg)
					}
				} else {
					if r != nil {
						t.Errorf("\n%s\nunexpected panic: %v", tc.reason, r)
					}
				}
			}()
			got := build(tc.key, tc.opts...)
			if !tc.mustPanic && got == nil {
				t.Errorf("\n%s\nbuild(...) returned nil", tc.reason)
			}
		})
	}
}

func TestNewJSONValidCases(t *testing.T) {
	cases := map[string]struct {
		reason string
		opts   []Option
		want   *Value
	}{
		"ValidName": {
			reason: "Building a value with a valid alphanumeric name should succeed.",
			opts: []Option{
				WithName("validName"),
			},
			want: &Value{
				key:  KeyJSON,
				name: "validName",
			},
		},
		"ValidNameWithUnderscore": {
			reason: "Building a value with a valid name containing underscores should succeed.",
			opts: []Option{
				WithName("valid_name"),
			},
			want: &Value{
				key:  KeyJSON,
				name: "valid_name",
			},
		},
		"ValidNameStartingWithUnderscore": {
			reason: "Building a value with a valid name starting with underscore should succeed.",
			opts: []Option{
				WithName("_validName"),
			},
			want: &Value{
				key:  KeyJSON,
				name: "_validName",
			},
		},
		"InlineWithoutName": {
			reason: "Building a value with inline and no name should succeed as they are compatible.",
			opts: []Option{
				WithInline(true),
			},
			want: &Value{
				key:    KeyJSON,
				inline: true,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := NewJSON(tc.opts...)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(Value{})); diff != "" {
				t.Errorf("\n%s\nNewJSON(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestValueOverrideFrom(t *testing.T) {
	cases := map[string]struct {
		reason string
		v      *Value
		o      *Value
		want   *Value
	}{
		"NilReceiver": {
			reason: "OverrideFrom should return nil when the receiver is nil.",
			v:      nil,
			o: &Value{
				key:  KeyJSON,
				name: "override",
			},
			want: nil,
		},
		"NilOverride": {
			reason: "OverrideFrom should return a deep copy of the receiver when the override is nil.",
			v: &Value{
				key:    KeyJSON,
				name:   "original",
				omit:   OmitEmpty,
				inline: true,
			},
			o: nil,
			want: &Value{
				key:    KeyJSON,
				name:   "original",
				omit:   OmitEmpty,
				inline: true,
			},
		},
		"OverrideNameOnly": {
			reason: "OverrideFrom should override the name when override has a name set.",
			v: &Value{
				key:    KeyJSON,
				name:   "original",
			},
			o: &Value{
				key:  KeyJSON,
				name: "overridden",
			},
			want: &Value{
				key:    KeyJSON,
				name:   "overridden",
			},
		},
		"OverrideOmitOnly": {
			reason: "OverrideFrom should override omit while preserving other fields when override has only omit set.",
			v: &Value{
				key:    KeyJSON,
				name:   "original",
				omit:   NotOmitted,
			},
			o: &Value{
				key:  KeyJSON,
				omit: OmitEmpty,
			},
			want: &Value{
				key:    KeyJSON,
				name:   "original",
				omit:   OmitEmpty,
			},
		},
		"OverrideInlineOnly": {
			reason: "OverrideFrom should override inline when override has inline set.",
			v: &Value{
				key:    KeyJSON,
				name:   "original",
				inline: false,
			},
			o: &Value{
				key:    KeyJSON,
				inline: true,
			},
			want: &Value{
				key:    KeyJSON,
				name:   "original",
				inline: true,
			},
		},
		"OverrideAllFields": {
			reason: "OverrideFrom should override name, omit, and inline when all are set in override.",
			v: &Value{
				key:    KeyJSON,
				name:   "original",
				omit:   NotOmitted,
				inline: false,
			},
			o: &Value{
				key:    KeyJSON,
				name:   "overridden",
				omit:   OmitEmpty,
				inline: true,
			},
			want: &Value{
				key:    KeyJSON,
				name:   "overridden",
				omit:   OmitEmpty,
				inline: true,
			},
		},
		"PreserveKeyDifferentKeys": {
			reason: "OverrideFrom should never override the key field even when override has a different key.",
			v: &Value{
				key:  KeyJSON,
				name: "original",
			},
			o: &Value{
				key:  KeyTF,
				name: "overridden",
			},
			want: &Value{
				key:    KeyJSON,
				name:   "overridden",
			},
		},
		"EmptyNameDoesNotOverride": {
			reason: "OverrideFrom should not override name when override has an empty name.",
			v: &Value{
				key:  KeyJSON,
				name: "original",
			},
			o: &Value{
				key:  KeyJSON,
				omit: OmitEmpty,
			},
			want: &Value{
				key:  KeyJSON,
				name: "original",
				omit: OmitEmpty,
			},
		},
		"OverrideOmitToNotOmitted": {
			reason: "OverrideFrom should be able to override omit to NotOmitted (zero value).",
			v: &Value{
				key:  KeyJSON,
				name: "field",
				omit: OmitEmpty,
			},
			o: &Value{
				key:  KeyJSON,
				omit: NotOmitted,
			},
			want: &Value{
				key:  KeyJSON,
				name: "field",
				omit: NotOmitted,
			},
		},
		"OverrideInlineToFalse": {
			reason: "OverrideFrom should be able to override inline to false (zero value).",
			v: &Value{
				key:    KeyJSON,
				name:   "field",
				inline: true,
			},
			o: &Value{
				key:    KeyJSON,
				inline: false,
			},
			want: &Value{
				key:    KeyJSON,
				name:   "field",
				inline: false,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.v.OverrideFrom(tc.o)

			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(Value{})); diff != "" {
				t.Errorf("\n%s\nOverrideFrom(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
