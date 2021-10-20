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

// Common ExternalName configurations.
var (
	// NameAsIdentifier uses "name" field in the arguments as the identifier of
	// the resource.
	NameAsIdentifier = ExternalName{
		SetIdentifierArgumentFn: func(base map[string]interface{}, name string) {
			base["name"] = name
		},
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
		DisableNameInitializer:  true,
	}
)
