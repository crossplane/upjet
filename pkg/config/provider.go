package config

import "sync"

// Store stores a global configuration for the Provider to generate with
// terrajet.
var Store = Provider{
	resource: map[string]Resource{},
}

// Provider stores configuration for a provider to generate with terrajet.
type Provider struct {
	mx       sync.RWMutex
	resource map[string]Resource
}

// SetForResource sets configuration for a given resource.
func (p *Provider) SetForResource(resource string, cfg Resource) {
	defer p.mx.Unlock()
	p.mx.Lock()
	p.resource[resource] = cfg
}

// GetForResource gets the configuration for a given resource.
func (p *Provider) GetForResource(resource string) Resource {
	defer p.mx.RUnlock()
	p.mx.RLock()
	return p.resource[resource]
}
