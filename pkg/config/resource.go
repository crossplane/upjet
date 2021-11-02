/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// SetIdentifierArgumentFn sets the name of the resource in Terraform attributes map.
type SetIdentifierArgumentFn func(base map[string]interface{}, name string)

// NopSetIdentifierArgument does nothing. It's useful for cases where the external
// name is calculated by provider and doesn't have any effect on spec fields.
var NopSetIdentifierArgument SetIdentifierArgumentFn = func(_ map[string]interface{}, _ string) {}

// SetIDFn sets the ID in TF State file.
type SetIDFn func(name string, parameters map[string]interface{}, tfstate map[string]interface{})

// NameAsID returns the name to be used as ID in TF State file.
var NameAsID SetIDFn = func(name string, _ map[string]interface{}, tfstate map[string]interface{}) {
	if tfstate == nil {
		tfstate = map[string]interface{}{}
	}
	tfstate["id"] = name
}

// GetNameFn returns the external name extracted from the TF State.
type GetNameFn func(tfstate map[string]interface{}) string

// IDAsName returns the TF State ID as external name.
var IDAsName GetNameFn = func(tfstate map[string]interface{}) string {
	return tfstate["id"].(string)
}

// AdditionalConnectionDetailsFn functions adds custom keys to connection details
// secret using input terraform attributes
type AdditionalConnectionDetailsFn func(attr map[string]interface{}) (map[string][]byte, error)

// NopAdditionalConnectionDetails does nothing, when no additional connection
// details configuration function provided.
func NopAdditionalConnectionDetails(_ map[string]interface{}) (map[string][]byte, error) {
	return nil, nil
}

// ResourceOption allows setting optional fields of a Resource object.
type ResourceOption func(*Resource)

// ExternalName contains all information that is necessary for naming operations,
// such as removal of those fields from spec schema and calling Configure function
// to fill attributes with information given in external name.
type ExternalName struct {
	// SetIdentifierArgumentFn sets the name of the resource in Terraform argument
	// map, otherwise we cannot know which field in HCL we should treat as identifier.
	SetIdentifierArgumentFn SetIdentifierArgumentFn

	// GetNameFn returns the external name extracted from TF State. In most cases,
	// "id" key of TF State should be returned.
	GetNameFn GetNameFn

	// SetIDFn sets the identification keys in TF State file. In most cases,
	// external name should be set to "id" key.
	SetIDFn SetIDFn

	// OmittedFields are the ones you'd like to be removed from the schema since
	// they are specified via external name. You can omit only the top level fields.
	// No field is omitted by default.
	OmittedFields []string

	// DisableNameInitializer allows you to specify whether the name initializer
	// that sets external name to metadata.name if none specified should be disabled.
	// It needs to be disabled for resources whose external name includes information
	// more than the actual name of the resource, like subscription ID or region
	// etc. which is unlikely to be included in metadata.name
	DisableNameInitializer bool
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

// Resource is the set of information that you can override at different steps
// of the code generation pipeline.
type Resource struct {
	// Name is the name of the resource type in Terraform,
	// e.g. aws_rds_cluster.
	Name string

	// TerraformResource is the Terraform representation of the resource.
	TerraformResource *schema.Resource

	// IDFieldName is the name of the ID field in Terraform state of the
	// resource. Its default is "id" and in almost all cases, you don't need
	// to overwrite it.
	IDFieldName string

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

	// ExternalName allows you to specify a custom ExternalName.
	ExternalName ExternalName

	// References keeps the configuration to build cross resource references
	References References

	// Sensitive keeps the configuration to handle sensitive information
	Sensitive Sensitive

	// LateInitializer configuration to control late-initialization behaviour
	LateInitializer LateInitializer
}
