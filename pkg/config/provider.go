package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/iancoleman/strcase"
	"github.com/pkg/errors"
)

// ResourceConfiguratorFn is a function that implements the ResourceConfigurator
// interface
type ResourceConfiguratorFn func(r *Resource)

// Configure configures a resource by calling ResourceConfiguratorFn
func (c ResourceConfiguratorFn) Configure(r *Resource) {
	c(r)
}

// ResourceConfigurator configures a Resource
type ResourceConfigurator interface {
	Configure(r *Resource)
}

// A ResourceConfiguratorChain chains multiple ResourceConfigurators.
type ResourceConfiguratorChain []ResourceConfigurator

// Configure configures a resource by calling each ResourceConfigurator in the
// chain serially.
func (cc ResourceConfiguratorChain) Configure(r *Resource) {
	for _, c := range cc {
		c.Configure(r)
	}
}

// XPProviderConfig stores configuration for the Crossplane ProviderConfig
// resource like API Version of the type and name of the controller package.
type XPProviderConfig struct {
	APIVersion        string
	ControllerPackage string
}

// DefaultResourceFn returns a default resource configuration to be used while
// building resource configurations.
type DefaultResourceFn func() Resource

// Provider holds configuration for a provider to be generated with Terrajet.
type Provider struct {
	// TerraformResourcePrefix is the prefix used in all resources of this
	// Terraform provider, e.g. "aws_". Defaults to "<prefix>_".
	TerraformResourcePrefix string
	// GroupSuffix is the suffix to append to resource groups, e.g.
	// ".aws.tf.crossplane.io". Defaults to ".<prefix>.tf.crossplane.io".
	GroupSuffix string
	// ShortName is the short name of the provider. Typically, added as a CRD
	// category, e.g. "tfaws". Default to "tf<prefix>". For more details on CRD
	// categories, see: https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#categories
	ShortName string
	// ModulePath is the go module path for the tf based provider repo, e.g.
	// "github.com/crossplane-contrib/provider-tf-aws"
	ModulePath string
	// XPProviderConfig is the configuration for the Crossplane "ProviderConfig"
	// type.
	XPProviderConfig XPProviderConfig
	// DefaultResourceFn is a function that returns resource configuration to be
	// used as default while building the resources.
	DefaultResourceFn DefaultResourceFn

	// SkipList is a list of regex for the Terraform resources to be skipped.
	// For example, to skip generation of "aws_shield_protection_group", one
	// can add "aws_shield_protection_group$". To skip whole aws waf group, one
	// can add "aws_waf.*" to the list.
	SkipList []string
	// IncludeList is a list of regex for the Terraform resources to be
	// skipped. For example, to include "aws_shield_protection_group" into
	// the generated resources, one can add "aws_shield_protection_group$".
	// To include whole aws waf group, one can add "aws_waf.*" to the list.
	// Defaults to []string{".+"} which would include all resources.
	IncludeList []string

	// Resources is a map holding resource configurations where key is Terraform
	// resource name.
	Resources map[string]*Resource

	// resourceConfigurators is a map holding resource configurators where key
	// is Terraform resource name.
	resourceConfigurators map[string]ResourceConfiguratorChain
}

// A ProviderOption configures a Provider.
type ProviderOption func(*Provider)

// WithGroupSuffix configures GroupSuffix for resources of this Provider.
func WithGroupSuffix(s string) ProviderOption {
	return func(p *Provider) {
		p.GroupSuffix = s
	}
}

// WithShortName configures ShortName for resources of this Provider.
func WithShortName(s string) ProviderOption {
	return func(p *Provider) {
		p.ShortName = s
	}
}

// WithIncludeList configures IncludeList for this Provider.
func WithIncludeList(l []string) ProviderOption {
	return func(p *Provider) {
		p.IncludeList = l
	}
}

// WithSkipList configures SkipList for this Provider.
func WithSkipList(l []string) ProviderOption {
	return func(p *Provider) {
		p.SkipList = l
	}
}

// WithXPProviderConfig configures XPProviderConfig for this Provider.
func WithXPProviderConfig(c XPProviderConfig) ProviderOption {
	return func(p *Provider) {
		p.XPProviderConfig = c
	}
}

// WithDefaultResourceFn configures DefaultResourceFn for this Provider
func WithDefaultResourceFn(f DefaultResourceFn) ProviderOption {
	return func(p *Provider) {
		p.DefaultResourceFn = f
	}
}

// NewProvider builds and returns a new Provider.
func NewProvider(resourceMap map[string]*schema.Resource, prefix string, modulePath string, opts ...ProviderOption) Provider {
	p := Provider{
		ModulePath:              modulePath,
		TerraformResourcePrefix: fmt.Sprintf("%s_", prefix),
		GroupSuffix:             fmt.Sprintf(".%s.tf.crossplane.io", prefix),
		ShortName:               fmt.Sprintf("tf%s", prefix),
		DefaultResourceFn:       getDefaultResource,
		XPProviderConfig: XPProviderConfig{
			APIVersion:        defaultAPIVersion,
			ControllerPackage: "providerconfig",
		},

		IncludeList: []string{
			// Include all Resources
			".+",
		},
		Resources: map[string]*Resource{},

		resourceConfigurators: map[string]ResourceConfiguratorChain{},
	}

	for _, o := range opts {
		o(&p)
	}

	p.parseSchema(resourceMap)

	return p
}

// AddResourceConfigurator adds resource specific configurators.
func (p *Provider) AddResourceConfigurator(resource string, c ResourceConfiguratorFn) { //nolint:interfacer
	// Note(turkenh): nolint reasoning - easier to provide a function without
	// converting to an explicit type supporting the ResourceConfigurator
	// interface. Since this function would be a frequently used one, it should
	// be a reasonable simplification.
	p.resourceConfigurators[resource] = append(p.resourceConfigurators[resource], c)
}

// ConfigureResources configures resources with provided ResourceConfigurator's
func (p *Provider) ConfigureResources() {
	for name := range p.Resources {
		p.resourceConfigurators[name].Configure(p.Resources[name])
	}
}

// parseSchema parses Terraform provider schema and builds a (default) resource
// configuration for each resource which could be overridden with custom
// configurations later.
func (p *Provider) parseSchema(resourceMap map[string]*schema.Resource) {
	for name, trResource := range resourceMap {
		if len(trResource.Schema) == 0 {
			// There are resources with no schema, that we will address later.
			fmt.Printf("Skipping resource %s because it has no schema\n", name)
			continue
		}
		if matches(name, p.SkipList) || !matches(name, p.IncludeList) {
			continue
		}
		// As group name we default to the second element if resource name
		// has at least 3 elements, otherwise, we took the first element as
		// default group name, examples:
		// - aws_rds_cluster => rds
		// - aws_rds_cluster_parameter_group => rds
		// - kafka_topic => kafka
		words := strings.Split(name, "_")
		groupName := words[1]
		if len(words) < 3 {
			groupName = words[0]
		}

		resource := p.DefaultResourceFn()
		resource.Name = name
		resource.Terraform = trResource
		resource.Group = groupName
		resource.Kind = strcase.ToCamel(strings.TrimPrefix(strings.TrimPrefix(name, p.TerraformResourcePrefix), groupName))

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
