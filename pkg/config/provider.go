package config

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Provider stores configuration for a provider to generate with terrajet.
type Provider struct {
	Schema         *schema.Provider
	GroupSuffix    string
	ResourcePrefix string
	ShortName      string
	ModulePath     string
	SkipList       map[string]struct{}
	IncludeList    []string

	Resource map[string]Resource
}

// SetForResource sets configuration for a given resource.
func (p *Provider) SetForResource(resource string, cfg Resource) {
	p.Resource[resource] = cfg
}

// GetForResource gets the configuration for a given resource.
func (p *Provider) GetForResource(resource string) Resource {
	return p.Resource[resource]
}
