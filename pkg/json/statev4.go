/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package json

import (
	"encoding/base64"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
)

const (
	errCannotParseState       = "cannot parse state"
	errCannotDecodeMetadata   = "cannot decode state metadata"
	errInvalidState           = "invalid state file"
	errFmtIncompatibleVersion = "state version not supported, expecting 4 found %d"

	errNotOneResource = "state file should contain exactly 1 resource"
	errNotOneInstance = "state file should contain exactly 1 instance"
)

// NewStateV4 returns a new base StateV4 object.
func NewStateV4() *StateV4 {
	return &StateV4{
		Version: 4,
		Serial:  1,
	}
}

// State file schema from https://github.com/hashicorp/terraform/blob/d9dfd451ea572219871bb9c5503a471418258e40/internal/states/statefile/version4.go

// StateV4 represents a version 4 terraform state
type StateV4 struct {
	Version          uint64                   `json:"version"`
	TerraformVersion string                   `json:"terraform_version"`
	Serial           uint64                   `json:"serial"`
	Lineage          string                   `json:"lineage"`
	RootOutputs      map[string]OutputStateV4 `json:"outputs"`
	Resources        []ResourceStateV4        `json:"resources"`
}

// OutputStateV4 represents a version 4 output state
type OutputStateV4 struct {
	ValueRaw     jsoniter.RawMessage `json:"value"`
	ValueTypeRaw jsoniter.RawMessage `json:"type"`
	Sensitive    bool                `json:"sensitive,omitempty"`
}

// ResourceStateV4 represents a version 4 resource state
type ResourceStateV4 struct {
	Module         string                  `json:"module,omitempty"`
	Mode           string                  `json:"mode"`
	Type           string                  `json:"type"`
	Name           string                  `json:"name"`
	EachMode       string                  `json:"each,omitempty"`
	ProviderConfig string                  `json:"provider"`
	Instances      []InstanceObjectStateV4 `json:"instances"`
}

// InstanceObjectStateV4 represents a version 4 instance object state
type InstanceObjectStateV4 struct {
	IndexKey interface{} `json:"index_key,omitempty"`
	Status   string      `json:"status,omitempty"`
	Deposed  string      `json:"deposed,omitempty"`

	SchemaVersion           uint64              `json:"schema_version"`
	AttributesRaw           jsoniter.RawMessage `json:"attributes,omitempty"`
	AttributesFlat          map[string]string   `json:"attributes_flat,omitempty"`
	AttributeSensitivePaths jsoniter.RawMessage `json:"sensitive_attributes,omitempty,"`

	PrivateRaw []byte `json:"private,omitempty"`

	Dependencies []string `json:"dependencies,omitempty"`

	CreateBeforeDestroy bool `json:"create_before_destroy,omitempty"`
}

// UnmarshalStateV4 parses a given Terraform state as StateV4 object
func UnmarshalStateV4(data []byte) (*StateV4, error) {
	st := &StateV4{}
	if err := JSParser.Unmarshal(data, st); err != nil {
		return nil, errors.Wrap(err, errCannotParseState)
	}

	return st, errors.Wrap(st.Validate(), errInvalidState)
}

// BuildStateV4 builds a StateV4 object from the given base64 encoded state and sensitive attributes
func BuildStateV4(encodedState string, attributesSensitive jsoniter.RawMessage) (*StateV4, error) {
	m, err := base64.StdEncoding.DecodeString(encodedState)
	if err != nil {
		return nil, errors.Wrap(err, errCannotDecodeMetadata)
	}

	st, err := UnmarshalStateV4(m)
	if err != nil {
		return nil, errors.Wrap(err, errCannotParseState)
	}

	st.Resources[0].Instances[0].AttributeSensitivePaths = attributesSensitive

	return st, nil
}

// Validate checks if the StateV4 is a valid Terraform managed resource state
func (st *StateV4) Validate() error {
	// We only recognize and support state file version 4 right now
	if st.Version != 4 {
		return errors.Errorf(errFmtIncompatibleVersion, st.Version)
	}
	// Terraform state files may contain more than 1 resources. And each resource
	// could have more than 1 instances which is controlled by the count argument:
	// https://www.terraform.io/docs/language/meta-arguments/count.html#basic-syntax
	// In our case, we expect our state file will always contain exactly 1 instance of 1 resource.
	if len(st.Resources) != 1 {
		return errors.New(errNotOneResource)
	}
	if len(st.Resources[0].Instances) != 1 {
		return errors.New(errNotOneInstance)
	}

	return nil
}

// GetAttributes returns attributes of the Terraform managed resource (i.e. first instance of first resource)
func (st *StateV4) GetAttributes() jsoniter.RawMessage {
	return st.Resources[0].Instances[0].AttributesRaw
}

// GetSensitiveAttributes returns sensitive attributes of the Terraform managed resource (i.e. first instance of first resource)
func (st *StateV4) GetSensitiveAttributes() jsoniter.RawMessage {
	return st.Resources[0].Instances[0].AttributeSensitivePaths
}

// GetPrivateRaw returns private attribute of the Terraform managed resource
// that is used as metadata by the Terraform provider
func (st *StateV4) GetPrivateRaw() []byte {
	return st.Resources[0].Instances[0].PrivateRaw
}

// GetEncodedState returns base64 encoded sanitized (i.e. sensitive attributes removed) state
func (st *StateV4) GetEncodedState() (string, error) {
	sensitive := st.Resources[0].Instances[0].AttributeSensitivePaths
	defer func() {
		st.Resources[0].Instances[0].AttributeSensitivePaths = sensitive
	}()

	st.Resources[0].Instances[0].AttributeSensitivePaths = nil
	b, err := JSParser.Marshal(st)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

// Serialize serializes StateV4 object to byte array
func (st *StateV4) Serialize() ([]byte, error) {
	return JSParser.Marshal(st)
}
