/*
Copyright 2021 Upbound Inc.
*/

package json

import (
	jsoniter "github.com/json-iterator/go"
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
	IndexKey any    `json:"index_key,omitempty"`
	Status   string `json:"status,omitempty"`
	Deposed  string `json:"deposed,omitempty"`

	SchemaVersion           uint64              `json:"schema_version"`
	AttributesRaw           jsoniter.RawMessage `json:"attributes,omitempty"`
	AttributesFlat          map[string]string   `json:"attributes_flat,omitempty"`
	AttributeSensitivePaths jsoniter.RawMessage `json:"sensitive_attributes,omitempty"`

	PrivateRaw []byte `json:"private,omitempty"`

	Dependencies []string `json:"dependencies,omitempty"`

	CreateBeforeDestroy bool `json:"create_before_destroy,omitempty"`
}

// GetAttributes returns attributes of the Terraform managed resource (i.e. first instance of first resource)
func (st *StateV4) GetAttributes() jsoniter.RawMessage {
	if st == nil || len(st.Resources) == 0 || len(st.Resources[0].Instances) == 0 {
		return nil
	}
	return st.Resources[0].Instances[0].AttributesRaw
}

// GetSensitiveAttributes returns sensitive attributes of the Terraform managed resource (i.e. first instance of first resource)
func (st *StateV4) GetSensitiveAttributes() jsoniter.RawMessage {
	if st == nil || len(st.Resources) == 0 || len(st.Resources[0].Instances) == 0 {
		return nil
	}
	return st.Resources[0].Instances[0].AttributeSensitivePaths
}

// GetPrivateRaw returns private attribute of the Terraform managed resource
// that is used as metadata by the Terraform provider
func (st *StateV4) GetPrivateRaw() []byte {
	if st == nil || len(st.Resources) == 0 || len(st.Resources[0].Instances) == 0 {
		return nil
	}
	return st.Resources[0].Instances[0].PrivateRaw
}
