/*
Copyright 2021 Upbound Inc.
*/

package config

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/upbound/upjet/pkg/registry"
	tjname "github.com/upbound/upjet/pkg/types/name"
)

// Commonly used resource configurations.
var (
	DefaultBasePackages = BasePackages{
		APIVersion: []string{
			// Default package for ProviderConfig APIs
			"apis/v1alpha1",
			"apis/v1beta1",
		},
		Controller: []string{
			// Default package for ProviderConfig controllers
			"internal/controller/providerconfig",
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
func DefaultResource(name string, terraformSchema *schema.Resource, terraformRegistry *registry.Resource, opts ...ResourceOption) *Resource {
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
		Name:              name,
		TerraformResource: terraformSchema,
		MetaResource:      terraformRegistry,
		ShortGroup:        group,
		Kind:              kind,
		Version:           "v1alpha1",
		ExternalName:      NameAsIdentifier,
		References:        map[string]Reference{},
		Sensitive:         NopSensitive,
		UseAsync:          true,
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

// MarkAsRequired marks the schema of the given fieldpath as required. It's most
// useful in cases where external name contains an optional parameter that is
// defaulted by the provider but we need it to exist or to fix plain buggy
// schemas.
func MarkAsRequired(sch *schema.Resource, fieldpaths ...string) {
	for _, fieldpath := range fieldpaths {
		if s := GetSchema(sch, fieldpath); s != nil {
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
