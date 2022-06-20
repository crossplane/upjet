/*
Copyright 2022 Upbound Inc.
*/

package meta

import (
	"io/ioutil"
	"path/filepath"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

const (
	// RandRFC1123Subdomain represents a template variable to be substituted
	// by the test runner at runtime with a random RFC1123 subdomain string.
	RandRFC1123Subdomain = "{{ .Rand.RFC1123Subdomain }}"
)

// Dependencies are the example manifests for the dependency resources.
// Key is formatted as <Terraform resource>.<example name>
type Dependencies map[string]string

// ResourceExample represents the scraped example HCL configuration
// for a Terraform resource
type ResourceExample struct {
	Manifest     string            `yaml:"manifest"`
	References   map[string]string `yaml:"references,omitempty"`
	Dependencies Dependencies      `yaml:"dependencies,omitempty"`
	Paved        fieldpath.Paved   `yaml:"-"`
}

// Resource represents the scraped metadata for a Terraform resource
type Resource struct {
	SubCategory      string            `yaml:"subCategory"`
	Description      string            `yaml:"description,omitempty"`
	Name             string            `yaml:"name"`
	TitleName        string            `yaml:"titleName"`
	Examples         []ResourceExample `yaml:"examples,omitempty"`
	ArgumentDocs     map[string]string `yaml:"argumentDocs"`
	ImportStatements []string          `yaml:"importStatements"`
	ExternalName     string            `yaml:"-"`
}

// ProviderMetadata metadata for a Terraform native provider
type ProviderMetadata struct {
	Name      string               `yaml:"name"`
	Resources map[string]*Resource `yaml:"resources"`
}

// NewProviderMetadataFromFile loads metadata from the specified YAML-formatted file
func NewProviderMetadataFromFile(path string) (*ProviderMetadata, error) {
	buff, err := ioutil.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read metadata file %q", path)
	}

	metadata := &ProviderMetadata{}
	if err := yaml.Unmarshal(buff, metadata); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal provider metadata")
	}
	for name, rm := range metadata.Resources {
		for j, re := range rm.Examples {
			if err := re.Paved.UnmarshalJSON([]byte(re.Manifest)); err != nil {
				return nil, errors.Wrapf(err, "cannot pave example manifest JSON: %s", re.Manifest)
			}
			rm.Examples[j] = re
		}
		metadata.Resources[name] = rm
	}
	return metadata, nil
}

// SetPathValue sets the field at the specified path to the given value
// in the example manifest.
func (re *ResourceExample) SetPathValue(fieldPath string, val interface{}) error {
	return errors.Wrapf(re.Paved.SetValue(fieldPath, val), "cannot set example manifest path %q to value: %#v", fieldPath, val)
}
