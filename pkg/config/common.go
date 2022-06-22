/*
Copyright 2021 Upbound Inc.
*/

package config

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	tjname "github.com/upbound/upjet/pkg/types/name"
)

// Commonly used resource configurations.
var (
	DefaultBasePackages = BasePackages{
		APIVersion: []string{
			// Default package for ProviderConfig APIs
			"apis/v1alpha1",
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
func DefaultResource(name string, terraformSchema *schema.Resource, opts ...ResourceOption) *Resource {
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
		ShortGroup:        group,
		Kind:              kind,
		Version:           "v1alpha1",
		ExternalName:      NameAsIdentifier,
		References:        map[string]Reference{},
		Sensitive:         NopSensitive,
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
func MoveToStatus(sch *schema.Resource, fields ...string) {
	for _, f := range fields {
		if _, ok := sch.Schema[f]; !ok {
			panic(fmt.Sprintf("field %s does not exist in schema", f))
		}
		sch.Schema[f].Optional = false
		sch.Schema[f].Computed = true

		// We need to move all nodes of that field to status.
		if el, ok := sch.Schema[f].Elem.(*schema.Resource); ok {
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
