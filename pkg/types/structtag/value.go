// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package structtag

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
)

const (
	errFmtInvalidJSONTagName = "invalid JSON struct tag name: %q (must match %s)"
)

// reJSONName matches valid Kubernetes CRD field names.
var reJSONName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Omit defines the omission policy for a struct tag.
type Omit string

const (
	// OmitAlways causes the struct field to be always omitted
	// during serialization operations.
	OmitAlways Omit = "-"
	// OmitEmpty causes the struct field to be omitted iff it has an empty value
	// during serialization operations.
	OmitEmpty Omit = "omitempty"
	// NotOmitted prevents the omission of the struct field during
	// serialization operations.
	NotOmitted Omit = ""
)

// Key represents the known struct tag keys.
type Key string

const (
	// KeyJSON is the JSON tag key, "json".
	KeyJSON Key = "json"
	// KeyTF is the Terraform tag key, "tf".
	KeyTF Key = "tf"
)

type Value struct {
	// key of the struct tag, e.g., json, tf.
	key Key
	// name is the serialized name of the struct field.
	name string
	// omit is the omission policy for the tag.
	omit Omit
	// inline indicates whether the field should be flattened.
	inline bool
}

// Option is a functional option for configuring a Value.
type Option func(*Value)

// WithName returns an Option that sets the serialized name.
func WithName(name string) Option {
	return func(v *Value) {
		v.name = name
	}
}

// WithOmit returns an Option that sets the omission policy.
func WithOmit(omit Omit) Option {
	return func(v *Value) {
		v.omit = omit
	}
}

// WithInline returns an Option that sets the inline flag.
func WithInline(inline bool) Option {
	return func(v *Value) {
		v.inline = inline
	}
}

// NewJSON returns a new JSON struct tag with the given options.
func NewJSON(opts ...Option) *Value {
	return build(KeyJSON, opts...)
}

// NewTF returns a new Terraform struct tag with the given options.
func NewTF(opts ...Option) *Value {
	return build(KeyTF, opts...)
}

// build creates a new Value with the given key and optional configuration.
// Panics if an invalid name is provided via WithName option.
func build(key Key, opts ...Option) *Value {
	v := &Value{
		key: key,
	}
	for _, opt := range opts {
		opt(v)
	}

	// validate name if set for JSON tags.
	if key == KeyJSON && len(v.name) > 0 && !reJSONName.MatchString(v.name) {
		panic(fmt.Sprintf(errFmtInvalidJSONTagName, v.name, reJSONName.String()))
	}
	// make sure name and inline are not both set.
	if v.inline && v.name != "" {
		panic(fmt.Sprintf("invalid struct tag: cannot set the name %q with inline", v.name))
	}
	// make sure omit and inline are not both set.
	if v.inline && v.omit != NotOmitted {
		panic(fmt.Sprintf("invalid struct tag: cannot set inline with omit option %q", v.omit))
	}
	return v
}

// parse parses a struct tag value string and returns a Value.
// The value string format follows common struct tag conventions:
//   - "" (empty) means use the default field name
//   - "-" means the field is always omitted (OmitAlways)
//   - "name" sets the serialized field name
//   - "name,omitempty" sets the name and OmitEmpty
//   - ",omitempty" sets OmitEmpty with no explicit name
//   - ",inline" sets the inline flag (used in Kubernetes for embedded structs)
//
// Returns an error if the given tag value cannot be parsed successfully.
func parse(key Key, value string) (*Value, error) { //nolint:gocyclo // easier to follow as a unit
	v := &Value{
		key: key,
	}

	if value == "" {
		return v, nil
	}

	parts := strings.Split(value, ",")
	// first part is the name (or "-" for always omitted).
	name := strings.TrimSpace(parts[0])
	if name == string(OmitAlways) {
		if len(parts) > 1 {
			return nil, errors.Errorf("invalid struct tag format: %q (cannot combine %q with other options)", value, OmitAlways)
		}
		// no name expected for "-" (OmitAlways).
		v.omit = OmitAlways
		return v, nil
	}

	if len(name) > 0 {
		if key == KeyJSON && !reJSONName.MatchString(name) {
			return nil, errors.Errorf(errFmtInvalidJSONTagName, name, reJSONName.String())
		}
		v.name = name
	}

	for i := 1; i < len(parts); i++ {
		option := strings.TrimSpace(parts[i])
		switch option {
		case string(OmitEmpty):
			v.omit = OmitEmpty
		case "inline":
			v.inline = true
		default:
			// ignore unknown options for forward compatibility.
		}
	}

	// inline cannot be combined with a name.
	if v.inline && v.name != "" {
		return nil, errors.Errorf("invalid struct tag format: %q (cannot combine name with inline)", value)
	}

	// inline cannot be combined with an omit.
	if v.inline && v.omit != NotOmitted {
		return nil, errors.Errorf("invalid struct tag format: %q (cannot combine inline with omitempty)", value)
	}

	return v, nil
}

// ParseJSON parses a JSON struct tag value string.
func ParseJSON(value string) (*Value, error) {
	return parse(KeyJSON, value)
}

// ParseTF parses a Terraform struct tag value string.
func ParseTF(value string) (*Value, error) {
	return parse(KeyTF, value)
}

// MustParseTF parses a Terraform struct tag value string.
// Panics if an error is encountered.
func MustParseTF(value string) *Value {
	v, err := ParseTF(value)
	if err != nil {
		panic(err.Error())
	}
	return v
}

// OverrideFrom returns a new Value of v with its attributes overridden from
// the Value o. If o is nil, it returns a deep copy of v. If v is nil,
// it returns nil. Tag key is never overridden. name is overridden if o.name
// is not empty. omit and inline are always overridden.
func (v *Value) OverrideFrom(o *Value) *Value {
	if v == nil {
		return nil
	}

	result := *v
	if o == nil {
		return &result
	}

	if len(o.name) > 0 {
		result.name = o.name
	}
	result.omit = o.omit
	result.inline = o.inline
	return &result
}

// NoOmit returns a not-omitted copy of the Value.
func (v *Value) NoOmit() *Value {
	r := *v
	r.omit = NotOmitted
	return &r
}

// Key returns the key of the struct tag.
func (v *Value) Key() Key {
	return v.key
}

// Name returns the serialized name of the struct field.
func (v *Value) Name() string {
	return v.name
}

// Omit returns the omission policy for the struct tag.
func (v *Value) Omit() Omit {
	return v.omit
}

// AlwaysOmitted returns true if the struct field is always omitted ("-").
func (v *Value) AlwaysOmitted() bool {
	return v.omit == OmitAlways
}

// Inline returns whether the field should be inlined/flattened.
func (v *Value) Inline() bool {
	return v.inline
}

// SetOmit sets the omit policy of the struct tag.
func (v *Value) SetOmit(o Omit) {
	v.omit = o
}

// StringWithoutKey returns the tag value without the key.
// Examples: `name`, `name,omitempty`, `,omitempty`, `,inline`, `-`
func (v *Value) StringWithoutKey() string {
	if v.omit == OmitAlways {
		return string(OmitAlways)
	}

	result := v.name
	// add omitempty option if set.
	if v.omit == OmitEmpty {
		result += "," + string(OmitEmpty)
	}

	// add inline option if set.
	if v.inline {
		result += ",inline"
	}

	return result
}

// String returns the complete struct tag with key and value.
// Examples: `json:"name"`, `tf:"name,omitempty"`, `json:",inline"`
func (v *Value) String() string {
	return fmt.Sprintf(`%s:"%s"`, string(v.key), v.StringWithoutKey())
}
