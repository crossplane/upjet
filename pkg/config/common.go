// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/crossplane/upjet/pkg/config/conversion"
	"github.com/crossplane/upjet/pkg/registry"
	tjname "github.com/crossplane/upjet/pkg/types/name"
)

const (
	// PackageNameConfig is the name of the provider subpackage that contains
	// the base resources (e.g., ProviderConfig, ProviderConfigUsage,
	// StoreConfig. etc.).
	// TODO: we should be careful that there may also exist short groups with
	// these names. We can consider making these configurable by the provider
	// maintainer.
	PackageNameConfig = "config"
	// PackageNameMonolith is the name of the backwards-compatible
	// provider subpackage that contains the all the resources.
	PackageNameMonolith = "monolith"
)

// Commonly used resource configurations.
var (
	DefaultBasePackages = BasePackages{
		APIVersion: []string{
			// Default package for ProviderConfig APIs
			"v1alpha1",
			"v1beta1",
		},

		Controller: []string{
			// Default package for ProviderConfig controllers
			"providerconfig",
		},
		ControllerMap: map[string]string{
			// Default package for ProviderConfig controllers
			"providerconfig": PackageNameConfig,
		},
	}

	// NopSensitive does nothing.
	NopSensitive = Sensitive{
		AdditionalConnectionDetailsFn: NopAdditionalConnectionDetails,
	}
)

// ResourceOption allows setting optional fields of a Resource object.
type ResourceOption func(*Resource)

// DefaultResource keeps an initial default configuration for all resources of a
// provider.
func DefaultResource(name string, terraformSchema *schema.Resource, terraformPluginFrameworkResource fwresource.Resource, terraformRegistry *registry.Resource, opts ...ResourceOption) *Resource {
	words := strings.Split(name, "_")
	// As group name we default to the second element if resource name
	// has at least 3 elements, otherwise, we took the first element as
	// default group name, examples:
	// - aws_rds_cluster => rds
	// - aws_rds_cluster_parameter_group => rds
	// - kafka_topic => kafka
	group := words[1]
	// As kind, we default to camel case version of what is left after dropping
	// elements before what is selected as group:
	// - aws_rds_cluster => Cluster
	// - aws_rds_cluster_parameter_group => ClusterParameterGroup
	// - kafka_topic => Topic
	kind := tjname.NewFromSnake(strings.Join(words[2:], "_")).Camel
	if len(words) < 3 {
		group = words[0]
		kind = tjname.NewFromSnake(words[1]).Camel
	}

	r := &Resource{
		Name:                             name,
		TerraformResource:                terraformSchema,
		TerraformPluginFrameworkResource: terraformPluginFrameworkResource,
		MetaResource:                     terraformRegistry,
		ShortGroup:                       group,
		Kind:                             kind,
		Version:                          "v1alpha1",
		ExternalName:                     NameAsIdentifier,
		References:                       make(References),
		Sensitive:                        NopSensitive,
		UseAsync:                         true,
		SchemaElementOptions:             make(SchemaElementOptions),
		ServerSideApplyMergeStrategies:   make(ServerSideApplyMergeStrategies),
		Conversions:                      []conversion.Conversion{conversion.NewIdentityConversionExpandPaths(conversion.AllVersions, conversion.AllVersions, nil)},
		OverrideFieldNames:               map[string]string{},
		listConversionPaths:              make(map[string]string),
	}
	for _, f := range opts {
		f(r)
	}
	return r
}

// MoveToStatus moves given fields and their leaf fields to the status as
// a whole. It's used mostly in cases where there is a field that is
// represented as a separate CRD, hence you'd like to remove that field from
// spec.
func MoveToStatus(sch *schema.Resource, fieldpaths ...string) {
	for _, f := range fieldpaths {
		s := GetSchema(sch, f)
		if s == nil {
			return
		}
		s.Optional = false
		s.Computed = true

		// We need to move all nodes of that field to status.
		if el, ok := s.Elem.(*schema.Resource); ok {
			l := make([]string, len(el.Schema))
			i := 0
			for fi := range el.Schema {
				l[i] = fi
				i++
			}
			MoveToStatus(el, l...)
		}
	}
}

// MarkAsRequired marks the given fieldpaths as required without manipulating
// the native field schema.
func (r *Resource) MarkAsRequired(fieldpaths ...string) {
	r.requiredFields = append(r.requiredFields, fieldpaths...)
}

// MarkAsRequired marks the schema of the given fieldpath as required. It's most
// useful in cases where external name contains an optional parameter that is
// defaulted by the provider but we need it to exist or to fix plain buggy
// schemas.
// Deprecated: Use Resource.MarkAsRequired instead.
// This function will be removed in future versions.
func MarkAsRequired(sch *schema.Resource, fieldpaths ...string) {
	for _, fp := range fieldpaths {
		if s := GetSchema(sch, fp); s != nil {
			s.Computed = false
			s.Optional = false
		}
	}
}

// GetSchema returns the schema of the field whose fieldpath is given.
// Returns nil if Schema is not found at the specified path.
func GetSchema(sch *schema.Resource, fieldpath string) *schema.Schema {
	current := sch
	fields := strings.Split(fieldpath, ".")
	final := fields[len(fields)-1]
	formers := fields[:len(fields)-1]
	for _, field := range formers {
		s, ok := current.Schema[field]
		if !ok {
			return nil
		}
		if s.Elem == nil {
			return nil
		}
		res, rok := s.Elem.(*schema.Resource)
		if !rok {
			return nil
		}
		current = res
	}
	s, ok := current.Schema[final]
	if !ok {
		return nil
	}
	return s
}

// ManipulateEveryField manipulates all fields in the schema by
// input function.
func ManipulateEveryField(r *schema.Resource, op func(sch *schema.Schema)) {
	for _, s := range r.Schema {
		if s == nil {
			return
		}
		op(s)
		if el, ok := s.Elem.(*schema.Resource); ok {
			ManipulateEveryField(el, op)
		}
	}
}
