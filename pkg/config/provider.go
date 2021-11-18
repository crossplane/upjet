package config

import (
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
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

// BasePackages keeps lists of base packages that needs to be registered as API
// and controllers. Typically, we expect to see ProviderConfig packages here.
type BasePackages struct {
	APIVersion []string
	Controller []string
}

// DefaultResourceFn returns a default resource configuration to be used while
// building resource configurations.
type DefaultResourceFn func(name string, terraformResource *schema.Resource, opts ...ResourceOption) *Resource

// Provider holds configuration for a provider to be generated with Terrajet.
type Provider struct {
	// TerraformResourcePrefix is the prefix used in all resources of this
	// Terraform provider, e.g. "aws_". Defaults to "<prefix>_". This is being
	// used while setting some defaults like Kind of the resource. For example,
	// for "aws_rds_cluster", we drop "aws_" prefix and its group ("rds") to set
	// Kind of the resource as "Cluster".
	TerraformResourcePrefix string

	// RootGroup is the root group that all CRDs groups in the provider are based
	// on, e.g. "aws.tf.crossplane.io".
	// Defaults to "<TerraformResourcePrefix>.tf.crossplane.io".
	RootGroup string

	// ShortName is the short name of the provider. Typically, added as a CRD
	// category, e.g. "tfaws". Default to "tf<prefix>". For more details on CRD
	// categories, see: https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#categories
	ShortName string

	// ModulePath is the go module path for the tf based provider repo, e.g.
	// "github.com/crossplane-contrib/provider-tf-aws"
	ModulePath string

	// BasePackages keeps lists of base packages that needs to be registered as
	// API and controllers. Typically, we expect to see ProviderConfig packages
	// here.
	BasePackages BasePackages

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

// WithGroupSuffix configures RootGroup for resources of this Provider.
func WithGroupSuffix(s string) ProviderOption {
	return func(p *Provider) {
		p.RootGroup = s
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

// WithBasePackages configures BasePackages for this Provider.
func WithBasePackages(b BasePackages) ProviderOption {
	return func(p *Provider) {
		p.BasePackages = b
	}
}

// WithDefaultResourceFn configures DefaultResourceFn for this Provider
func WithDefaultResourceFn(f DefaultResourceFn) ProviderOption {
	return func(p *Provider) {
		p.DefaultResourceFn = f
	}
}

// NewProvider builds and returns a new Provider.
func NewProvider(resourceMap map[string]*schema.Resource, prefix string, modulePath string, opts ...ProviderOption) *Provider {
	p := &Provider{
		ModulePath:              modulePath,
		TerraformResourcePrefix: fmt.Sprintf("%s_", prefix),
		RootGroup:               fmt.Sprintf("%s.tf.crossplane.io", prefix),
		ShortName:               fmt.Sprintf("tf%s", prefix),
		BasePackages:            DefaultBasePackages,
		DefaultResourceFn:       DefaultResource,
		IncludeList: []string{
			// Include all Resources
			".+",
		},
		Resources:             map[string]*Resource{},
		resourceConfigurators: map[string]ResourceConfiguratorChain{},
	}

	for _, o := range opts {
		o(p)
	}

	for name, terraformResource := range resourceMap {
		if len(terraformResource.Schema) == 0 {
			// There are resources with no schema, that we will address later.
			fmt.Printf("Skipping resource %s because it has no schema\n", name)
			continue
		}
		if matches(name, p.SkipList) {
			fmt.Printf("Skipping resource %s because it is in SkipList\n", name)
			continue
		}
		if !matches(name, p.IncludeList) {
			continue
		}

		p.Resources[name] = p.DefaultResourceFn(name, terraformResource)
	}

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

// SetResourceConfigurator sets ResourceConfigurator for a resource. This will
// override all previously added ResourceConfigurators for this resource.
func (p *Provider) SetResourceConfigurator(resource string, c ResourceConfigurator) {
	p.resourceConfigurators[resource] = ResourceConfiguratorChain{c}
}

// ConfigureResources configures resources with provided ResourceConfigurator's
func (p *Provider) ConfigureResources() {
	for name, c := range p.resourceConfigurators {
		// if not skipped & included & configured via the default configurator
		if r, ok := p.Resources[name]; ok {
			c.Configure(r)
		}
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
