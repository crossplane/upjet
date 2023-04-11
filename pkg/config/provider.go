/*
Copyright 2022 Upbound Inc.
*/

package config

import (
	"fmt"
	"regexp"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/registry"
	conversiontfjson "github.com/upbound/upjet/pkg/types/conversion/tfjson"
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

// Provider holds configuration for a provider to be generated with Upjet.
type Provider struct {
	// TerraformResourcePrefix is the prefix used in all resources of this
	// Terraform provider, e.g. "aws_". Defaults to "<prefix>_". This is being
	// used while setting some defaults like Kind of the resource. For example,
	// for "aws_rds_cluster", we drop "aws_" prefix and its group ("rds") to set
	// Kind of the resource as "Cluster".
	TerraformResourcePrefix string

	// RootGroup is the root group that all CRDs groups in the provider are based
	// on, e.g. "aws.jet.crossplane.io".
	// Defaults to "<TerraformResourcePrefix>.jet.crossplane.io".
	RootGroup string

	// ShortName is the short name of the provider. Typically, added as a CRD
	// category, e.g. "awsjet". Default to "<prefix>jet". For more details on CRD
	// categories, see: https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#categories
	ShortName string

	// ModulePath is the go module path for the Crossplane provider repo, e.g.
	// "github.com/upbound/provider-aws"
	ModulePath string

	// FeaturesPackage is the relative package patch for the features package to
	// configure the features behind the feature gates.
	FeaturesPackage string

	// BasePackages keeps lists of base packages that needs to be registered as
	// API and controllers. Typically, we expect to see ProviderConfig packages
	// here.
	BasePackages BasePackages

	// DefaultResourceOptions is a list of config.ResourceOption that will be
	// applied to all resources before any user-provided options are applied.
	DefaultResourceOptions []ResourceOption

	// SkipList is a list of regex for the Terraform resources to be skipped.
	// For example, to skip generation of "aws_shield_protection_group", one
	// can add "aws_shield_protection_group$". To skip whole aws waf group, one
	// can add "aws_waf.*" to the list.
	SkipList []string

	// skippedResourceNames is a list of Terraform resource names
	// available in the Terraform provider schema, but
	// not in the include list or in the skip list, meaning that
	// the corresponding managed resources are not generated.
	skippedResourceNames []string

	// IncludeList is a list of regex for the Terraform resources to be
	// included. For example, to include "aws_shield_protection_group" into
	// the generated resources, one can add "aws_shield_protection_group$".
	// To include whole aws waf group, one can add "aws_waf.*" to the list.
	// Defaults to []string{".+"} which would include all resources.
	IncludeList []string

	// Resources is a map holding resource configurations where key is Terraform
	// resource name.
	Resources map[string]*Resource

	// refInjectors is an ordered list of `ReferenceInjector`s for
	// injecting references across this Provider's resources.
	refInjectors []ReferenceInjector

	// resourceConfigurators is a map holding resource configurators where key
	// is Terraform resource name.
	resourceConfigurators map[string]ResourceConfiguratorChain
}

// ReferenceInjector injects cross-resource references across the resources
// of this Provider.
type ReferenceInjector interface {
	InjectReferences(map[string]*Resource) error
}

// A ProviderOption configures a Provider.
type ProviderOption func(*Provider)

// WithRootGroup configures RootGroup for resources of this Provider.
func WithRootGroup(s string) ProviderOption {
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

// WithDefaultResourceOptions configures DefaultResourceOptions for this
// Provider.
func WithDefaultResourceOptions(opts ...ResourceOption) ProviderOption {
	return func(p *Provider) {
		p.DefaultResourceOptions = opts
	}
}

// WithReferenceInjectors configures an ordered list of `ReferenceInjector`s
// for this Provider. The configured reference resolvers are executed in order
// to inject cross-resource references across this Provider's resources.
func WithReferenceInjectors(refInjectors []ReferenceInjector) ProviderOption {
	return func(p *Provider) {
		p.refInjectors = refInjectors
	}
}

// WithFeaturesPackage configures FeaturesPackage for this Provider.
func WithFeaturesPackage(s string) ProviderOption {
	return func(p *Provider) {
		p.FeaturesPackage = s
	}
}

// NewProvider builds and returns a new Provider from provider
// tfjson schema, that is generated using Terraform CLI with:
// `terraform providers schema --json`
func NewProvider(schema []byte, prefix string, modulePath string, metadata []byte, opts ...ProviderOption) *Provider { // nolint:gocyclo
	ps := tfjson.ProviderSchemas{}
	if err := ps.UnmarshalJSON(schema); err != nil {
		panic(err)
	}
	if len(ps.Schemas) != 1 {
		panic(fmt.Sprintf("there should exactly be 1 provider schema but there are %d", len(ps.Schemas)))
	}
	var rs map[string]*tfjson.Schema
	for _, v := range ps.Schemas {
		rs = v.ResourceSchemas
		break
	}

	resourceMap := conversiontfjson.GetV2ResourceMap(rs)
	providerMetadata, err := registry.NewProviderMetadataFromFile(metadata)
	if err != nil {
		panic(errors.Wrap(err, "cannot load provider metadata"))
	}

	p := &Provider{
		ModulePath:              modulePath,
		TerraformResourcePrefix: fmt.Sprintf("%s_", prefix),
		RootGroup:               fmt.Sprintf("%s.upbound.io", prefix),
		ShortName:               prefix,
		BasePackages:            DefaultBasePackages,
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

	p.skippedResourceNames = make([]string, 0, len(resourceMap))
	for name, terraformResource := range resourceMap {
		if len(terraformResource.Schema) == 0 {
			// There are resources with no schema, that we will address later.
			fmt.Printf("Skipping resource %s because it has no schema\n", name)
		}
		if len(terraformResource.Schema) == 0 || matches(name, p.SkipList) || !matches(name, p.IncludeList) {
			p.skippedResourceNames = append(p.skippedResourceNames, name)
			continue
		}
		p.Resources[name] = DefaultResource(name, terraformResource, providerMetadata.Resources[name], p.DefaultResourceOptions...)
	}
	for i, refInjector := range p.refInjectors {
		if err := refInjector.InjectReferences(p.Resources); err != nil {
			panic(errors.Wrapf(err, "cannot inject references using the configured ReferenceInjector at index %d", i))
		}
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

// GetSkippedResourceNames returns a list of Terraform resource names
// available in the Terraform provider schema, but
// not in the include list or in the skip list, meaning that
// the corresponding managed resources are not generated.
func (p *Provider) GetSkippedResourceNames() []string {
	return p.skippedResourceNames
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
