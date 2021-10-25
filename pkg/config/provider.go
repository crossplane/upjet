package config

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Provider stores configuration for a provider to generate with terrajet.
type Provider struct {
	Schema         *schema.Provider
	GroupSuffix    string
	ResourcePrefix string
	ShortName      string
	ModulePath     string

	SkipList    []string
	IncludeList []string

	DefaultResource Resource
	resources       map[string]Resource
}

type ProviderOption func(*Provider)

func WithGroupSuffix(s string) ProviderOption {
	return func(p *Provider) {
		p.GroupSuffix = s
	}
}

func WithShortName(s string) ProviderOption {
	return func(p *Provider) {
		p.ShortName = s
	}
}

func WithIncludeList(l []string) ProviderOption {
	return func(p *Provider) {
		p.IncludeList = l
	}
}

func WithSkipList(l []string) ProviderOption {
	return func(p *Provider) {
		p.SkipList = l
	}
}

func WithDefaultResource(r Resource) ProviderOption {
	return func(p *Provider) {
		p.DefaultResource = r
	}
}

func NewProvider(schema *schema.Provider, prefix string, modulePath string, opts ...ProviderOption) Provider {
	p := Provider{
		Schema:         schema,
		ResourcePrefix: fmt.Sprintf("%s_", prefix),
		ModulePath:     modulePath,
		GroupSuffix:    fmt.Sprintf(".%s.tf.crossplane.io", prefix),
		ShortName:      fmt.Sprintf("tf%s", prefix),

		IncludeList: []string{
			// Include all resources starting with the prefix
			".+",
		},
		DefaultResource: DefaultResource,
		resources:       map[string]Resource{},
	}

	for _, o := range opts {
		o(&p)
	}

	return p
}

// GetResource gets the configuration for a given resource.
func (p *Provider) GetResource(resource string) Resource {
	r, ok := p.resources[resource]
	if ok {
		return r
	}
	return p.DefaultResource
}

// SetResource sets configuration for a given resource.
func (p *Provider) SetResource(resource string, cfg Resource) {
	p.resources[resource] = cfg
}
