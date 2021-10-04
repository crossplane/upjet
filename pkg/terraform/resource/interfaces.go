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

package resource

import (
	"context"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
)

// SecretClient is the client to get sensitive data from kubernetes secrets
//go:generate go run github.com/golang/mock/mockgen -copyright_file ../../../hack/boilerplate.txt -destination ./mocks/resource.go -package mocks github.com/crossplane-contrib/terrajet/pkg/terraform/resource SecretClient
type SecretClient interface {
	GetSecretData(ctx context.Context, s v1.SecretReference) (map[string][]byte, error)
	GetSecretValue(ctx context.Context, sel v1.SecretKeySelector) ([]byte, error)
}

// Observable structs can get and set observations in the form of Terraform JSON.
type Observable interface {
	GetObservation() (map[string]interface{}, error)
	SetObservation(map[string]interface{}) error
}

// Parameterizable structs can get and set parameters of the managed resource
// using map form of Terraform JSON.
type Parameterizable interface {
	GetParameters(ctx context.Context, c SecretClient) (map[string]interface{}, error)
	SetParameters(map[string]interface{}) error
}

// MetadataProvider provides Terraform metadata for the Terraform managed resource
type MetadataProvider interface {
	GetTerraformResourceType() string
	GetTerraformResourceIdField() string
}

// LateInitializer late-initializes the managed resource from observed Terraform state
type LateInitializer interface {
	// LateInitialize this Terraformed resource using its observed tfState.
	// returns True if the there are any spec changes for the resource.
	LateInitialize(attrs []byte) (bool, error)
}

// Terraformed is a Kubernetes object representing a concrete terraform managed resource
type Terraformed interface {
	resource.Managed

	MetadataProvider
	Observable
	Parameterizable
	SensitiveDataProvider
	LateInitializer
}
