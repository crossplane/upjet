package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/iancoleman/strcase"
	"github.com/pkg/errors"
)

type ProviderConfig struct {
	Version           string
	ControllerPackage string
}

// Provider stores configuration for a provider to generate with terrajet.
type Provider struct {
	GroupSuffix       string
	ResourcePrefix    string
	ShortName         string
	ModulePath        string
	Config            ProviderConfig
	DefaultResourceFn DefaultResourceFn

	SkipList    []string
	IncludeList []string

	Resources map[string]*Resource
}

type DefaultResourceFn func() Resource
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

func WithProviderConfig(c ProviderConfig) ProviderOption {
	return func(p *Provider) {
		p.Config = c
	}
}

func WithDefaultResourceFn(f DefaultResourceFn) ProviderOption {
	return func(p *Provider) {
		p.DefaultResourceFn = f
	}
}

func NewProvider(schema *schema.Provider, prefix string, modulePath string, opts ...ProviderOption) Provider {
	p := Provider{
		ResourcePrefix:    fmt.Sprintf("%s_", prefix),
		ModulePath:        modulePath,
		GroupSuffix:       fmt.Sprintf(".%s.tf.crossplane.io", prefix),
		ShortName:         fmt.Sprintf("tf%s", prefix),
		DefaultResourceFn: getDefaultResource,
		Config: ProviderConfig{
			Version:           defaultAPIVersion,
			ControllerPackage: "providerconfig",
		},

		IncludeList: []string{
			// Include all Resources
			".+",
		},
		Resources: map[string]*Resource{},
	}

	for _, o := range opts {
		o(&p)
	}

	p.parseSchema(schema)

	return p
}

// OverrideResourceConfig overrides default configuration for a given resource
// with the provided configuration.
func (p *Provider) OverrideResourceConfig(resource string, o *Resource) {
	p.Resources[resource].OverrideConfig(o)
}

// parseSchema parses Terraform provider schema and builds a (default) resource
// configuration for each resource which could be overridden with custom
// configurations at later stages of the pipeline.
func (p *Provider) parseSchema(schema *schema.Provider) {
	for name, trResource := range schema.ResourcesMap {
		if len(trResource.Schema) == 0 {
			// There are resources with no schema, that we will address later.
			fmt.Printf("Skipping resource %s because it has no schema\n", name)
			continue
		}
		if matches(name, p.SkipList) || !matches(name, p.IncludeList) {
			continue
		}
		words := strings.Split(name, "_")
		// As group name we default to the second element if resource name
		// has at least 3 elements, otherwise, we took the first element as
		// default group name, examples:
		// - aws_rds_cluster => rds
		// - aws_rds_cluster_parameter_group => rds
		// - kafka_topic => kafka
		groupName := words[1]
		if len(words) < 3 {
			groupName = words[0]
		}

		resource := p.DefaultResourceFn()
		resource.TerraformResourceName = name
		resource.TerraformResource = trResource
		resource.Group = groupName
		resource.Kind = strcase.ToCamel(strings.TrimPrefix(strings.TrimPrefix(name, p.ResourcePrefix), groupName))

		p.Resources[name] = &resource
	}
}

func matches(name string, regexList []string) bool {
	for _, r := range regexList {
		ok, err := regexp.MatchString(r, name)
		if err != nil {
			panic(errors.Wrap(err, "cannot match regular expression"))
		}
		if ok {
			return true
		}
	}
	return false
}
