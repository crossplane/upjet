/*
Copyright 2022 Upbound Inc.
*/

package registry

import (
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/upbound/upjet/pkg/resource/json"
)

const (
	// RandRFC1123Subdomain represents a template variable to be substituted
	// by the test runner at runtime with a random RFC1123 subdomain string.
	RandRFC1123Subdomain = "${Rand.RFC1123Subdomain}"
)

// Dependencies are the example manifests for the dependency resources.
// Key is formatted as <Terraform resource>.<example name>
type Dependencies map[string]string

// ResourceExample represents the scraped example HCL configuration
// for a Terraform resource
type ResourceExample struct {
	Name         string            `yaml:"name"`
	Manifest     string            `yaml:"manifest"`
	References   map[string]string `yaml:"references,omitempty"`
	Dependencies Dependencies      `yaml:"dependencies,omitempty"`
	Paved        fieldpath.Paved   `yaml:"-"`
}

// Resource represents the scraped metadata for a Terraform resource
type Resource struct {
	// SubCategory is the category name under which this Resource resides in
	// Terraform registry docs. Example:"Key Vault" for Azure Vault resources.
	// In Terraform docs, resources are grouped (categorized) using this field.
	SubCategory string `yaml:"subCategory"`
	// Description is a short description for the resource as it appears in
	// Terraform registry. Example: "Manages a Key Vault Key." for the
	// azurerm_key_vault_key resource.
	// This field is suitable for use in generating CRD Kind documentation.
	Description string `yaml:"description,omitempty"`
	// Name is the Terraform name of the resource. Example: azurerm_key_vault_key
	Name string `yaml:"name"`
	// Title is the title name of the resource that appears in
	// the Terraform registry doc page for a Terraform resource.
	Title string `yaml:"title"`
	// Examples are the example HCL configuration blocks for the resource
	// that appear in the resource's registry page. They are in the same
	// order as they appear on the registry page.
	Examples []ResourceExample `yaml:"examples,omitempty"`
	// ArgumentDocs maps resource attributes to their documentation in the
	// resource's registry page.
	ArgumentDocs map[string]string `yaml:"argumentDocs"`
	// ImportStatements are the example Terraform import statements as they
	// appear in the resource's registry page.
	// Example: terraform import azurerm_key_vault.example /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/mygroup1/providers/Microsoft.KeyVault/vaults/vault1
	ImportStatements []string `yaml:"importStatements"`
	// ExternalName configured for this resource. This allows the
	// external name used in the generated example manifests to be
	// overridden for a specific resource via configuration.
	ExternalName string `yaml:"-"`
}

// ProviderMetadata metadata for a Terraform native provider
type ProviderMetadata struct {
	Name      string               `yaml:"name"`
	Resources map[string]*Resource `yaml:"resources"`
}

// NewProviderMetadataFromFile loads metadata from the specified YAML-formatted document
func NewProviderMetadataFromFile(providerMetadata []byte) (*ProviderMetadata, error) {
	metadata := &ProviderMetadata{}
	if err := yaml.Unmarshal(providerMetadata, metadata); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal provider metadata")
	}
	for name, rm := range metadata.Resources {
		for j, re := range rm.Examples {
			if err := re.Paved.UnmarshalJSON([]byte(re.Manifest)); err != nil {
				return nil, errors.Wrapf(err, "cannot pave example manifest JSON: %s", re.Manifest)
			}
			rm.Examples[j] = re
			if rm.Examples[j].Dependencies == nil {
				rm.Examples[j].Dependencies = make(map[string]string)
			}
		}
		metadata.Resources[name] = rm
	}
	return metadata, nil
}

// SetPathValue sets the field at the specified path to the given value
// in the example manifest.
func (re *ResourceExample) SetPathValue(fieldPath string, val any) error {
	return errors.Wrapf(re.Paved.SetValue(fieldPath, val), "cannot set example manifest path %q to value: %#v", fieldPath, val)
}

// SetPathValue sets the field at the specified path to the given value
// in the example manifest of the specified dependency. Key format is:
// <Terraform resource type>.<configuration block name>, e.g.,
// aws_subnet.subnet1
func (d Dependencies) SetPathValue(dependencyKey string, fieldPath string, val any) error {
	m, ok := d[dependencyKey]
	if !ok {
		return nil
	}
	var params map[string]any
	if err := json.TFParser.Unmarshal([]byte(m), &params); err != nil {
		return errors.Wrapf(err, "cannot unmarshal dependency %q as JSON", dependencyKey)
	}
	p := fieldpath.Pave(params)
	if err := p.SetValue(fieldPath, val); err != nil {
		return errors.Wrapf(err, "cannot set example dependency %q path %q to value: %#v", dependencyKey, fieldPath, val)
	}
	buff, err := p.MarshalJSON()
	if err != nil {
		return errors.Wrapf(err, "cannot marshal dependency %q as JSON", dependencyKey)
	}
	d[dependencyKey] = string(buff)
	return nil
}
