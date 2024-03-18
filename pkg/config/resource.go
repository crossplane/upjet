// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/pkg/config/conversion"
	"github.com/crossplane/upjet/pkg/registry"
)

// A ListType is a type of list.
type ListType string

// Types of lists.
const (
	// ListTypeAtomic means the entire list is replaced during merge. At any
	// point in time, a single manager owns the list.
	ListTypeAtomic ListType = "atomic"

	// ListTypeSet can be granularly merged, and different managers can own
	// different elements in the list. The list can include only scalar
	// elements.
	ListTypeSet ListType = "set"

	// ListTypeMap can be granularly merged, and different managers can own
	// different elements in the list. The list can include only nested types
	// (i.e. objects).
	ListTypeMap ListType = "map"
)

// A MapType is a type of map.
type MapType string

// Types of maps.
const (
	// MapTypeAtomic means that the map can only be entirely replaced by a
	// single manager.
	MapTypeAtomic MapType = "atomic"

	// MapTypeGranular means that the map supports separate managers updating
	// individual fields.
	MapTypeGranular MapType = "granular"
)

// A StructType is a type of struct.
type StructType string

// Struct types.
const (
	// StructTypeAtomic means that the struct can only be entirely replaced by a
	// single manager.
	StructTypeAtomic StructType = "atomic"

	// StructTypeGranular means that the struct supports separate managers
	// updating individual fields.
	StructTypeGranular StructType = "granular"
)

// SetIdentifierArgumentsFn sets the name of the resource in Terraform attributes map,
// i.e. Main HCL file.
type SetIdentifierArgumentsFn func(base map[string]any, externalName string)

// NopSetIdentifierArgument does nothing. It's useful for cases where the external
// name is calculated by provider and doesn't have any effect on spec fields.
var NopSetIdentifierArgument SetIdentifierArgumentsFn = func(_ map[string]any, _ string) {}

// GetIDFn returns the ID to be used in TF State file, i.e. "id" field in
// terraform.tfstate.
type GetIDFn func(ctx context.Context, externalName string, parameters map[string]any, terraformProviderConfig map[string]any) (string, error)

// ExternalNameAsID returns the name to be used as ID in TF State file.
var ExternalNameAsID GetIDFn = func(_ context.Context, externalName string, _ map[string]any, _ map[string]any) (string, error) {
	return externalName, nil
}

// GetExternalNameFn returns the external name extracted from the TF State.
type GetExternalNameFn func(tfstate map[string]any) (string, error)

// IDAsExternalName returns the TF State ID as external name.
var IDAsExternalName GetExternalNameFn = func(tfstate map[string]any) (string, error) {
	if id, ok := tfstate["id"].(string); ok && id != "" {
		return id, nil
	}
	return "", errors.New("cannot find id in tfstate")
}

// AdditionalConnectionDetailsFn functions adds custom keys to connection details
// secret using input terraform attributes
type AdditionalConnectionDetailsFn func(attr map[string]any) (map[string][]byte, error)

// NopAdditionalConnectionDetails does nothing, when no additional connection
// details configuration function provided.
var NopAdditionalConnectionDetails AdditionalConnectionDetailsFn = func(_ map[string]any) (map[string][]byte, error) {
	return nil, nil
}

// ExternalName contains all information that is necessary for naming operations,
// such as removal of those fields from spec schema and calling Configure function
// to fill attributes with information given in external name.
type ExternalName struct {
	// SetIdentifierArgumentFn sets the name of the resource in Terraform argument
	// map. In many cases, there is a field called "name" in the HCL schema, however,
	// there are cases like RDS DB Cluster where the name field in HCL is called
	// "cluster_identifier". This function is the place that you can take external
	// name and assign it to that specific key for that resource type.
	SetIdentifierArgumentFn SetIdentifierArgumentsFn

	// GetExternalNameFn returns the external name extracted from TF State. In most cases,
	// "id" field contains all the information you need. You'll need to extract
	// the format that is decided for external name annotation to use.
	// For example the following is an Azure resource ID:
	// /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/mygroup1
	// The function should return "mygroup1" so that it can be used to set external
	// name if it was not set already.
	GetExternalNameFn GetExternalNameFn

	// GetIDFn returns the string that will be used as "id" key in TF state. In
	// many cases, external name format is the same as "id" but when it is not
	// we may need information from other places to construct it. For example,
	// the following is an Azure resource ID:
	// /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/mygroup1
	// The function here should use information from supplied arguments to
	// construct this ID, i.e. "mygroup1" from external name, subscription ID
	// from terraformProviderConfig, and others from parameters map if needed.
	GetIDFn GetIDFn

	// OmittedFields are the ones you'd like to be removed from the schema since
	// they are specified via external name. For example, if you set
	// "cluster_identifier" in SetIdentifierArgumentFn, then you need to omit
	// that field.
	// You can omit only the top level fields.
	// No field is omitted by default.
	OmittedFields []string

	// DisableNameInitializer allows you to specify whether the name initializer
	// that sets external name to metadata.name if none specified should be disabled.
	// It needs to be disabled for resources whose external identifier is randomly
	// assigned by the provider, like AWS VPC where it gets vpc-21kn123 identifier
	// and not let you name it.
	DisableNameInitializer bool

	// IdentifierFields are the fields that are used to construct external
	// resource identifier. We need to know these fields no matter what the
	// management policy is including the Observe Only, different from other
	// (required) fields.
	IdentifierFields []string

	// RequiredFields are the fields that are marked as required, although
	// it is not required in the TF schema.
	RequiredFields []string
}

// References represents reference resolver configurations for the fields of a
// given resource. Key should be the field path of the field to be referenced.
type References map[string]Reference

// Reference represents the Crossplane options used to generate
// reference resolvers for fields
type Reference struct {
	// Type is the type name of the CRD if it is in the same package or
	// <package-path>.<type-name> if it is in a different package.
	Type string
	// TerraformName is the name of the Terraform resource
	// which will be referenced. The supplied resource name is
	// converted to a type name of the corresponding CRD using
	// the configured TerraformTypeMapper.
	TerraformName string
	// Extractor is the function to be used to extract value from the
	// referenced type. Defaults to getting external name.
	// Optional
	Extractor string
	// RefFieldName is the field name for the Reference field. Defaults to
	// <field-name>Ref or <field-name>Refs.
	// Optional
	RefFieldName string
	// SelectorFieldName is the field name for the Selector field. Defaults to
	// <field-name>Selector.
	// Optional
	SelectorFieldName string
}

// Sensitive represents configurations to handle sensitive information
type Sensitive struct {
	// AdditionalConnectionDetailsFn is the path for function adding additional
	// connection details keys
	AdditionalConnectionDetailsFn AdditionalConnectionDetailsFn

	// fieldPaths keeps the mapping of sensitive fields in Terraform schema with
	// terraform field path as key and xp field path as value.
	fieldPaths map[string]string
}

// LateInitializer represents configurations that control
// late-initialization behaviour
type LateInitializer struct {
	// IgnoredFields are the field paths to be skipped during
	// late-initialization. Similar to other configurations, these paths are
	// Terraform field paths concatenated with dots. For example, if we want to
	// ignore "ebs" block in "aws_launch_template", we should add
	// "block_device_mappings.ebs".
	IgnoredFields []string

	// ignoredCanonicalFieldPaths are the Canonical field paths to be skipped
	// during late-initialization. This is filled using the `IgnoredFields`
	// field which keeps Terraform paths by converting them to Canonical paths.
	ignoredCanonicalFieldPaths []string
}

// GetIgnoredCanonicalFields returns the ignoredCanonicalFields
func (l *LateInitializer) GetIgnoredCanonicalFields() []string {
	return l.ignoredCanonicalFieldPaths
}

// AddIgnoredCanonicalFields sets ignored canonical fields
func (l *LateInitializer) AddIgnoredCanonicalFields(cf string) {
	if l.ignoredCanonicalFieldPaths == nil {
		l.ignoredCanonicalFieldPaths = make([]string, 0)
	}
	l.ignoredCanonicalFieldPaths = append(l.ignoredCanonicalFieldPaths, cf)
}

// GetFieldPaths returns the fieldPaths map for Sensitive
func (s *Sensitive) GetFieldPaths() map[string]string {
	return s.fieldPaths
}

// AddFieldPath adds the given tf path and xp path to the fieldPaths map.
func (s *Sensitive) AddFieldPath(tf, xp string) {
	if s.fieldPaths == nil {
		s.fieldPaths = make(map[string]string)
	}
	s.fieldPaths[tf] = xp
}

// OperationTimeouts allows configuring resource operation timeouts:
// https://www.terraform.io/language/resources/syntax#operation-timeouts
// Please note that, not all resources support configuring timeouts.
type OperationTimeouts struct {
	Read   time.Duration
	Create time.Duration
	Update time.Duration
	Delete time.Duration
}

// NewInitializerFn returns the Initializer with a client.
type NewInitializerFn func(client client.Client) managed.Initializer

// TagInitializer returns a tagger to use default tag initializer.
var TagInitializer NewInitializerFn = func(client client.Client) managed.Initializer {
	return NewTagger(client, "tags")
}

// Tagger implements the Initialize function to set external tags
type Tagger struct {
	kube      client.Client
	fieldName string
}

// NewTagger returns a Tagger object.
func NewTagger(kube client.Client, fieldName string) *Tagger {
	return &Tagger{kube: kube, fieldName: fieldName}
}

// Initialize is a custom initializer for setting external tags
func (t *Tagger) Initialize(ctx context.Context, mg xpresource.Managed) error {
	if sets.New[xpv1.ManagementAction](mg.GetManagementPolicies()...).Equal(sets.New[xpv1.ManagementAction](xpv1.ManagementActionObserve)) {
		// We don't want to add tags to the spec.forProvider if the resource is
		// only being Observed.
		return nil
	}
	paved, err := fieldpath.PaveObject(mg)
	if err != nil {
		return err
	}
	pavedByte, err := setExternalTagsWithPaved(xpresource.GetExternalTags(mg), paved, t.fieldName)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(pavedByte, mg); err != nil {
		return err
	}
	if err := t.kube.Update(ctx, mg); err != nil {
		return err
	}
	return nil
}

func setExternalTagsWithPaved(externalTags map[string]string, paved *fieldpath.Paved, fieldName string) ([]byte, error) {
	tags := map[string]*string{
		xpresource.ExternalResourceTagKeyKind:     ptr.To(externalTags[xpresource.ExternalResourceTagKeyKind]),
		xpresource.ExternalResourceTagKeyName:     ptr.To(externalTags[xpresource.ExternalResourceTagKeyName]),
		xpresource.ExternalResourceTagKeyProvider: ptr.To(externalTags[xpresource.ExternalResourceTagKeyProvider]),
	}

	if err := paved.SetValue(fmt.Sprintf("spec.forProvider.%s", fieldName), tags); err != nil {
		return nil, err
	}
	pavedByte, err := paved.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return pavedByte, nil
}

type InjectedKey struct {
	Key          string
	DefaultValue string
}

// ListMapKeys is the list map keys when the server-side apply merge strategy
// islistType=map.
type ListMapKeys struct {
	// InjectedKey can be used to inject the specified index key
	// into the generated CRD schema for the list object when
	// the SSA merge strategy for the parent list is `map`.
	// If a non-zero `InjectedKey` is specified, then a field of type string with
	// the specified name is injected into the Terraform schema and used as
	// a list map key together with any other existing keys specified in `Keys`.
	InjectedKey InjectedKey
	// Keys is the set of list map keys to be used while SSA merges list items.
	// If InjectedKey is non-zero, then it's automatically put into Keys and
	// you must not specify the InjectedKey in Keys explicitly.
	Keys []string
}

// ListMergeStrategy configures the corresponding field as list
// and configures its server-side apply merge strategy.
type ListMergeStrategy struct {
	// ListMapKeys is the list map keys when the SSA merge strategy is
	// `listType=map`.  The keys specified here must be a set of scalar Terraform
	// argument names to be used as the list map keys for the object list.
	ListMapKeys ListMapKeys
	// MergeStrategy is the SSA merge strategy for an object list. Valid values
	// are: `atomic`, `set` and `map`
	MergeStrategy ListType
}

// MergeStrategy configures the server-side apply merge strategy for the
// corresponding field. One and only one of the pointer members can be set
// and the specified merge strategy configuration must match the field's
// type, e.g., you cannot set MapMergeStrategy for a field of type list.
type MergeStrategy struct {
	ListMergeStrategy   ListMergeStrategy
	MapMergeStrategy    MapType
	StructMergeStrategy StructType
}

// ServerSideApplyMergeStrategies configures the server-side apply merge strategy
// for the field at the specified path as the map key. The key is
// a Terraform configuration argument path such as a.b.c, without any
// index notation (i.e., array/map components do not need indices).
// It's an error to set a configuration option which does not match
// the object type at the specified path or to leave the corresponding
// configuration entry empty. For example, if the field at path a.b.c is
// a list, then ListMergeStrategy must be set and it should be the only
// configuration entry set.
type ServerSideApplyMergeStrategies map[string]MergeStrategy

// Resource is the set of information that you can override at different steps
// of the code generation pipeline.
type Resource struct {
	// Name is the name of the resource type in Terraform,
	// e.g. aws_rds_cluster.
	Name string

	// TerraformResource is the Terraform representation of the
	// Terraform Plugin SDKv2 based resource.
	TerraformResource *schema.Resource

	// TerraformPluginFrameworkResource is the Terraform representation
	// of the TF Plugin Framework based resource
	TerraformPluginFrameworkResource fwresource.Resource

	// ShortGroup is the short name of the API group of this CRD. The full
	// CRD API group is calculated by adding the group suffix of the provider.
	// For example, ShortGroup could be `ec2` where group suffix of the
	// provider is `aws.crossplane.io` and in that case, the full group would
	// be `ec2.aws.crossplane.io`
	ShortGroup string

	// Version is the version CRD will have.
	Version string

	// Kind is the kind of the CRD.
	Kind string

	// UseAsync should be enabled for resource whose creation and/or deletion
	// takes more than 1 minute to complete such as Kubernetes clusters or
	// databases.
	UseAsync bool

	// InitializerFns specifies the initializer functions to be used
	// for this Resource.
	InitializerFns []NewInitializerFn

	// OperationTimeouts allows configuring resource operation timeouts.
	OperationTimeouts OperationTimeouts

	// ExternalName allows you to specify a custom ExternalName.
	ExternalName ExternalName

	// References keeps the configuration to build cross resource references
	References References

	// Sensitive keeps the configuration to handle sensitive information
	Sensitive Sensitive

	// LateInitializer configuration to control late-initialization behaviour
	LateInitializer LateInitializer

	// MetaResource is the metadata associated with the resource scraped from
	// the Terraform registry.
	MetaResource *registry.Resource

	// Path is the resource path for the API server endpoint. It defaults to
	// the plural name of the generated CRD. Overriding this sets both the
	// path and the plural name for the generated CRD.
	Path string

	// SchemaElementOptions is a map from the schema element paths to
	// SchemaElementOption for configuring options for schema elements.
	SchemaElementOptions SchemaElementOptions

	// TerraformConfigurationInjector allows a managed resource to inject
	// configuration values in the Terraform configuration map obtained by
	// deserializing its `spec.forProvider` value. Managed resources can
	// use this resource configuration option to inject Terraform
	// configuration parameters into their deserialized configuration maps,
	// if the deserialization skips certain fields.
	TerraformConfigurationInjector ConfigurationInjector

	// TerraformCustomDiff allows a resource.Terraformed to customize how its
	// Terraform InstanceDiff is computed during reconciliation.
	TerraformCustomDiff CustomDiff

	// ServerSideApplyMergeStrategies configures the server-side apply merge
	// strategy for the fields at the given map keys. The map key is
	// a Terraform configuration argument path such as a.b.c, without any
	// index notation (i.e., array/map components do not need indices).
	ServerSideApplyMergeStrategies ServerSideApplyMergeStrategies

	Conversions []conversion.Conversion

	// useTerraformPluginSDKClient indicates that a plugin SDK external client should
	// be generated instead of the Terraform CLI-forking client.
	useTerraformPluginSDKClient bool

	// useTerraformPluginFrameworkClient indicates that a Terraform
	// Plugin Framework external client should be generated instead of
	// the Terraform Plugin SDKv2 client.
	useTerraformPluginFrameworkClient bool

	// OverrideFieldNames allows to manually override the relevant field name to
	// avoid possible Go struct name conflicts that may occur after Multiversion
	// CRDs support. During field generation, there may be fields with the same
	// struct name calculated in the same group. For example, let X and Y
	// resources in the same API group have a field named Tag. This field is an
	// object type and the name calculated for the struct to be generated is
	// TagParameters (for spec) for both resources. To avoid this conflict, upjet
	// looks at all previously created structs in the package during generation
	// and if there is a conflict, it puts the Kind name of the related resource
	// in front of the next one: YTagParameters.
	// With Multiversion CRDs support, the above conflict scenario cannot be
	// solved in the generator when the old API group is preserved and not
	// regenerated, because the generator does not know the object names in the
	// old version. For example, a new API version is generated for resource X. In
	// this case, no generation is done for the old version of X and when Y is
	// generated, the generator is not aware of the TagParameters in X and
	// generates TagParameters instead of YTagParameters. Thus, two object types
	// with the same name are generated in the same package. This can be overcome
	// by using this configuration API.
	// The key of the map indicates the name of the field that is generated and
	// causes the conflict, while the value indicates the name used to avoid the
	// conflict. By convention, also used in upjet, the field name is preceded by
	// the value of the generated Kind, for example:
	// "TagParameters": "ClusterTagParameters"
	OverrideFieldNames map[string]string
}

// ShouldUseTerraformPluginSDKClient returns whether to generate an SDKv2-based
// external client for this Resource.
func (r *Resource) ShouldUseTerraformPluginSDKClient() bool {
	return r.useTerraformPluginSDKClient
}

// ShouldUseTerraformPluginFrameworkClient returns whether to generate a
// Terraform Plugin Framework-based external client for this Resource.
func (r *Resource) ShouldUseTerraformPluginFrameworkClient() bool {
	return r.useTerraformPluginFrameworkClient
}

// CustomDiff customizes the computed Terraform InstanceDiff. This can be used
// in cases where, for example, changes in a certain argument should just be
// dismissed. The new InstanceDiff is returned along with any errors.
type CustomDiff func(diff *terraform.InstanceDiff, state *terraform.InstanceState, config *terraform.ResourceConfig) (*terraform.InstanceDiff, error)

// ConfigurationInjector is a function that injects Terraform configuration
// values from the specified managed resource into the specified configuration
// map. jsonMap is the map obtained by converting the `spec.forProvider` using
// the JSON tags and tfMap is obtained by using the TF tags.
type ConfigurationInjector func(jsonMap map[string]any, tfMap map[string]any) error

// SchemaElementOptions represents schema element options for the
// schema elements of a Resource.
type SchemaElementOptions map[string]*SchemaElementOption

// SetAddToObservation sets the AddToObservation for the specified key.
func (m SchemaElementOptions) SetAddToObservation(el string) {
	if m[el] == nil {
		m[el] = &SchemaElementOption{}
	}
	m[el].AddToObservation = true
}

// AddToObservation returns true if the schema element at the specified path
// should be added to the CRD type's Observation type.
func (m SchemaElementOptions) AddToObservation(el string) bool {
	return m[el] != nil && m[el].AddToObservation
}

// SetEmbeddedObject sets the EmbeddedObject for the specified key.
func (m SchemaElementOptions) SetEmbeddedObject(el string) {
	if m[el] == nil {
		m[el] = &SchemaElementOption{}
	}
	m[el].EmbeddedObject = true
}

// EmbeddedObject returns true if the schema element at the specified path
// should be generated as an embedded object.
func (m SchemaElementOptions) EmbeddedObject(el string) bool {
	return m[el] != nil && m[el].EmbeddedObject
}

// SchemaElementOption represents configuration options on a schema element.
type SchemaElementOption struct {
	// AddToObservation is set to true if the field represented by
	// a schema element is to be added to the generated CRD type's
	// Observation type.
	AddToObservation bool
	// EmbeddedObject  is set to true if the field represented by
	// a schema element is to be embedded into its parent instead of being
	// generated as a single element list.
	EmbeddedObject bool
}
