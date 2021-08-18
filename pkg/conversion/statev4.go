package conversion

import (
	"encoding/base64"
	"encoding/json"

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
	ValueRaw     json.RawMessage `json:"value"`
	ValueTypeRaw json.RawMessage `json:"type"`
	Sensitive    bool            `json:"sensitive,omitempty"`
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

	SchemaVersion           uint64            `json:"schema_version"`
	AttributesRaw           json.RawMessage   `json:"attributes,omitempty"`
	AttributesFlat          map[string]string `json:"attributes_flat,omitempty"`
	AttributeSensitivePaths json.RawMessage   `json:"sensitive_attributes,omitempty,"`

	PrivateRaw []byte `json:"private,omitempty"`

	Dependencies []string `json:"dependencies,omitempty"`

	CreateBeforeDestroy bool `json:"create_before_destroy,omitempty"`
}

// ParseStateV4 parses a given Terraform state as StateV4 object
func ParseStateV4(data []byte) (*StateV4, error) {
	st := &StateV4{}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, errors.Wrap(err, errCannotParseState)
	}

	return st, errors.Wrap(st.Validate(), errInvalidState)
}

// BuildStateV4 builds a StateV4 object from the given base64 encoded state and sensitive attributes
func BuildStateV4(encodedState string, attributesSensitive json.RawMessage) (*StateV4, error) {
	m, err := base64.StdEncoding.DecodeString(encodedState)
	if err != nil {
		return nil, errors.Wrap(err, errCannotDecodeMetadata)
	}

	st, err := ParseStateV4(m)
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
func (st *StateV4) GetAttributes() json.RawMessage {
	return st.Resources[0].Instances[0].AttributesRaw
}

// GetSensitiveAttributes returns sensitive attributes of the Terraform managed resource (i.e. first instance of first resource)
func (st *StateV4) GetSensitiveAttributes() json.RawMessage {
	return st.Resources[0].Instances[0].AttributeSensitivePaths
}

// GetEncodedState returns base64 encoded sanitized (i.e. sensitive attributes removed) state
func (st *StateV4) GetEncodedState() (string, error) {
	// TODO(hasan): do we need a deep copy, probably
	st.Resources[0].Instances[0].AttributeSensitivePaths = nil
	b, err := json.Marshal(st)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

// Serialize serializes StateV4 object to byte array
func (st *StateV4) Serialize() ([]byte, error) {
	return json.Marshal(st)
}
