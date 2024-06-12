// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"regexp"

	tfjson "github.com/hashicorp/terraform-json"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/registry"
	"github.com/crossplane/upjet/pkg/schema/traverser"
	conversiontfjson "github.com/crossplane/upjet/pkg/types/conversion/tfjson"
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

// BasePackages keeps lists of packages that needs to be registered as API
// and controllers. Typically, we expect to see ProviderConfig packages here.
// These APIs and controllers belong to non-generated (manually maintained)
// resources.
type BasePackages struct {
	APIVersion []string
	// Deprecated: Use ControllerMap instead.
	Controller    []string
	ControllerMap map[string]string
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
	// on, e.g. "aws.upbound.io".
	// Defaults to "<TerraformResourcePrefix>.upbound.io".
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

	// MainTemplate is the template string to be used to render the
	// provider subpackage main program. If this is set, the generated provider
	// is broken up into subpackage families partitioned across the API groups.
	// A monolithic provider is also generated to
	// ensure backwards-compatibility.
	MainTemplate string

	// skippedResourceNames is a list of Terraform resource names
	// available in the Terraform provider schema, but
	// not in the include list or in the skip list, meaning that
	// the corresponding managed resources are not generated.
	skippedResourceNames []string

	// IncludeList is a list of regex for the Terraform resources to be
	// included and reconciled via the Terraform CLI.
	// For example, to include "aws_shield_protection_group" into
	// the generated resources, one can add "aws_shield_protection_group$".
	// To include whole aws waf group, one can add "aws_waf.*" to the list.
	// Defaults to []string{".+"} which would include all resources.
	IncludeList []string

	// TerraformPluginSDKIncludeList is a list of regex for the Terraform resources
	// implemented with Terraform Plugin SDKv2 to be included and reconciled
	// in the no-fork architecture (without the Terraform CLI).
	// For example, to include "aws_shield_protection_group" into
	// the generated resources, one can add "aws_shield_protection_group$".
	// To include whole aws waf group, one can add "aws_waf.*" to the list.
	// Defaults to []string{".+"} which would include all resources.
	TerraformPluginSDKIncludeList []string

	// TerraformPluginFrameworkIncludeList is a list of regex for the Terraform
	// resources implemented with Terraform Plugin Framework  to be included and
	// reconciled in the no-fork architecture (without the Terraform CLI).
	// For example, to include "aws_shield_protection_group" into
	// the generated resources, one can add "aws_shield_protection_group$".
	// To include whole aws waf group, one can add "aws_waf.*" to the list.
	// Defaults to []string{".+"} which would include all resources.
	TerraformPluginFrameworkIncludeList []string

	// Resources is a map holding resource configurations where key is Terraform
	// resource name.
	Resources map[string]*Resource

	// TerraformProvider is the Terraform provider in Terraform Plugin SDKv2
	// compatible format
	TerraformProvider *schema.Provider

	// TerraformPluginFrameworkProvider is the Terraform provider reference
	// in Terraform Plugin Framework compatible format
	TerraformPluginFrameworkProvider fwprovider.Provider

	// refInjectors is an ordered list of `ReferenceInjector`s for
	// injecting references across this Provider's resources.
	refInjectors []ReferenceInjector

	// resourceConfigurators is a map holding resource configurators where key
	// is Terraform resource name.
	resourceConfigurators map[string]ResourceConfiguratorChain

	// schemaTraversers is a chain of schema traversers to be used with
	// this Provider configuration. Schema traversers can be used to inspect or
	// modify the Provider configuration based on the underlying Terraform
	// resource schemas.
	schemaTraversers []traverser.SchemaTraverser
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

// WithTerraformPluginSDKIncludeList configures the TerraformPluginSDKIncludeList for this Provider,
// with the given Terraform Plugin SDKv2-based resource name list
func WithTerraformPluginSDKIncludeList(l []string) ProviderOption {
	return func(p *Provider) {
		p.TerraformPluginSDKIncludeList = l
	}
}

// WithTerraformPluginFrameworkIncludeList configures the
// TerraformPluginFrameworkIncludeList for this Provider, with the given
// Terraform Plugin Framework-based resource name list
func WithTerraformPluginFrameworkIncludeList(l []string) ProviderOption {
	return func(p *Provider) {
		p.TerraformPluginFrameworkIncludeList = l
	}
}

// WithTerraformProvider configures the TerraformProvider for this Provider.
func WithTerraformProvider(tp *schema.Provider) ProviderOption {
	return func(p *Provider) {
		p.TerraformProvider = tp
	}
}

// WithTerraformPluginFrameworkProvider configures the
// TerraformPluginFrameworkProvider for this Provider.
func WithTerraformPluginFrameworkProvider(tp fwprovider.Provider) ProviderOption {
	return func(p *Provider) {
		p.TerraformPluginFrameworkProvider = tp
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

// WithMainTemplate configures the provider family main module file's path.
// This template file will be used to generate the main modules of the
// family's members.
func WithMainTemplate(template string) ProviderOption {
	return func(p *Provider) {
		p.MainTemplate = template
	}
}

// WithSchemaTraversers configures a chain of schema traversers to be used with
// this Provider configuration. Schema traversers can be used to inspect or
// modify the Provider configuration based on the underlying Terraform
// resource schemas.
func WithSchemaTraversers(traversers ...traverser.SchemaTraverser) ProviderOption {
	return func(p *Provider) {
		p.schemaTraversers = traversers
	}
}

// NewProvider builds and returns a new Provider from provider
// tfjson schema, that is generated using Terraform CLI with:
// `terraform providers schema --json`
func NewProvider(schema []byte, prefix string, modulePath string, metadata []byte, opts ...ProviderOption) *Provider { //nolint:gocyclo
	ps := tfjson.ProviderSchemas{}
	if err := ps.UnmarshalJSON(schema); err != nil {
		panic(errors.Wrap(err, "failed to unmarshal the Terraform JSON schema"))
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
	terraformPluginFrameworkResourceFunctionsMap := terraformPluginFrameworkResourceFunctionsMap(p.TerraformPluginFrameworkProvider)
	for name, terraformResource := range resourceMap {
		if len(terraformResource.Schema) == 0 {
			// There are resources with no schema, that we will address later.
			fmt.Printf("Skipping resource %s because it has no schema\n", name)
		}
		// if in both of the include lists, the new behavior prevails
		isTerraformPluginSDK := matches(name, p.TerraformPluginSDKIncludeList)
		isPluginFrameworkResource := matches(name, p.TerraformPluginFrameworkIncludeList)
		isCLIResource := matches(name, p.IncludeList)
		if (isTerraformPluginSDK && isPluginFrameworkResource) || (isTerraformPluginSDK && isCLIResource) || (isPluginFrameworkResource && isCLIResource) {
			panic(errors.Errorf(`resource %q is specified in more than one include list. It should appear in at most one of the lists "IncludeList", "TerraformPluginSDKIncludeList" or "TerraformPluginFrameworkIncludeList"`, name))
		}
		if len(terraformResource.Schema) == 0 || matches(name, p.SkipList) || (!matches(name, p.IncludeList) && !isTerraformPluginSDK && !isPluginFrameworkResource) {
			p.skippedResourceNames = append(p.skippedResourceNames, name)
			continue
		}
		if isTerraformPluginSDK {
			if p.TerraformProvider == nil || p.TerraformProvider.ResourcesMap[name] == nil {
				panic(errors.Errorf("resource %q is configured to be reconciled with Terraform Plugin SDK"+
					"but either config.Provider.TerraformProvider is not configured or the Go schema does not exist for the resource", name))
			}
			terraformResource = p.TerraformProvider.ResourcesMap[name]
			if terraformResource.Schema == nil {
				if terraformResource.SchemaFunc == nil {
					p.skippedResourceNames = append(p.skippedResourceNames, name)
					fmt.Printf("Skipping resource %s because it has no schema and no schema function\n", name)
					continue
				}
				terraformResource.Schema = terraformResource.SchemaFunc()
			}
		}

		var terraformPluginFrameworkResource fwresource.Resource
		if isPluginFrameworkResource {
			resourceFunc := terraformPluginFrameworkResourceFunctionsMap[name]
			if p.TerraformPluginFrameworkProvider == nil || resourceFunc == nil {
				panic(errors.Errorf("resource %q is configured to be reconciled with Terraform Plugin Framework"+
					"but either config.Provider.TerraformPluginFrameworkProvider is not configured or the provider doesn't have the resource.", name))
			}

			terraformPluginFrameworkResource = resourceFunc()
		}

		p.Resources[name] = DefaultResource(name, terraformResource, terraformPluginFrameworkResource, providerMetadata.Resources[name], p.DefaultResourceOptions...)
		p.Resources[name].useTerraformPluginSDKClient = isTerraformPluginSDK
		p.Resources[name].useTerraformPluginFrameworkClient = isPluginFrameworkResource
		// traverse the Terraform resource schema to initialize the upjet Resource
		// configurations
		if err := TraverseSchemas(name, p.Resources[name], p.schemaTraversers...); err != nil {
			panic(errors.Wrap(err, "failed to execute the Terraform schema traverser chain"))
		}
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

func terraformPluginFrameworkResourceFunctionsMap(provider fwprovider.Provider) map[string]func() fwresource.Resource {
	if provider == nil {
		return make(map[string]func() fwresource.Resource, 0)
	}

	ctx := context.TODO()
	resourceFunctions := provider.Resources(ctx)
	resourceFunctionsMap := make(map[string]func() fwresource.Resource, len(resourceFunctions))

	providerMetadata := fwprovider.MetadataResponse{}
	provider.Metadata(ctx, fwprovider.MetadataRequest{}, &providerMetadata)

	for _, resourceFunction := range resourceFunctions {
		resource := resourceFunction()

		resourceTypeNameReq := fwresource.MetadataRequest{
			ProviderTypeName: providerMetadata.TypeName,
		}
		resourceTypeNameResp := fwresource.MetadataResponse{}
		resource.Metadata(ctx, resourceTypeNameReq, &resourceTypeNameResp)

		resourceFunctionsMap[resourceTypeNameResp.TypeName] = resourceFunction
	}

	return resourceFunctionsMap
}
