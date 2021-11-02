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
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/iancoleman/strcase"
)

// Common ExternalName configurations.
var (
	// NameAsIdentifier uses "name" field in the arguments as the identifier of
	// the resource.
	NameAsIdentifier = ExternalName{
		SetIdentifierArgumentFn: func(base map[string]interface{}, name string) {
			base["name"] = name
		},
		GetNameFn: IDAsName,
		SetIDFn:   NameAsID,
		OmittedFields: []string{
			"name",
			"name_prefix",
		},
	}

	// IdentifierFromProvider is used in resources whose identifier is assigned by
	// the remote client, such as AWS VPC where it gets an identifier like
	// vpc-2213das instead of letting user choose a name.
	IdentifierFromProvider = ExternalName{
		SetIdentifierArgumentFn: NopSetIdentifierArgument,
		GetNameFn:               IDAsName,
		SetIDFn:                 NameAsID,
		DisableNameInitializer:  true,
	}

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
		AdditionalConnectionDetailsFn: func(_ map[string]interface{}) (map[string][]byte, error) {
			return nil, nil
		},
	}
)

// DefaultResource keeps an initial default configuration for all resources of a
// provider.
func DefaultResource(name string, terraformSchema *schema.Resource) *Resource {
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
	kind := strcase.ToCamel(strings.Join(words[2:], "_"))
	if len(words) < 3 {
		group = words[0]
		kind = strcase.ToCamel(words[1])
	}

	return &Resource{
		Name:              name,
		TerraformResource: terraformSchema,
		ShortGroup:        group,
		Kind:              kind,
		Version:           "v1alpha1",
		ExternalName:      NameAsIdentifier,
		References:        map[string]Reference{},
		Sensitive:         NopSensitive,
	}
}
