package config

import (
	"sync"

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

	mx       sync.RWMutex
	Resource map[string]Resource
}

// SetForResource sets configuration for a given resource.
func (p *Provider) SetForResource(resource string, cfg Resource) {
	defer p.mx.Unlock()
	p.mx.Lock()
	p.Resource[resource] = cfg
}

// GetForResource gets the configuration for a given resource.
func (p *Provider) GetForResource(resource string) Resource {
	defer p.mx.RUnlock()
	p.mx.RLock()
	return p.Resource[resource]
}
