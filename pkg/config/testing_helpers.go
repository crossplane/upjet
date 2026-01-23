// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/require"
)

// NewTestProvider creates a minimal provider for testing with a single test resource.
// This function is exported for use in integration tests.
func NewTestProvider(t *testing.T) *Provider {
	t.Helper()

	pc := &Provider{
		RootGroup: "example.io",
		Resources: map[string]*Resource{},
	}

	return pc
}

// newTestProvider is the package-internal version for unit tests.
func newTestProvider(t *testing.T) *Provider {
	return NewTestProvider(t)
}

// NewTestResource creates a test resource with the given name and Terraform schema.
// The resource is configured with a minimal setup suitable for testing conversions.
// This function is exported for use in integration tests.
func NewTestResource(name string, tfSchema map[string]*schema.Schema) *Resource {
	tfResource := &schema.Resource{}
	if tfSchema != nil {
		tfResource.Schema = tfSchema
	}
	return &Resource{
		Name:                              name,
		ShortGroup:                        "test",
		Kind:                              name,
		TerraformResource:                 tfResource,
		AutoConversionRegistrationOptions: AutoConversionRegistrationOptions{},
	}
}

// newTestResource is the package-internal version for unit tests.
func newTestResource(name string) *Resource {
	return NewTestResource(name, nil)
}

// NewTestResourceWithIntField creates a test resource with a single integer field.
// Useful for testing string→number type conversions where the target is int.
// This function is exported for use in integration tests.
func NewTestResourceWithIntField(name, fieldName string) *Resource {
	return NewTestResource(name, map[string]*schema.Schema{
		fieldName: {
			Type:     schema.TypeInt,
			Optional: true,
		},
	})
}

// newTestResourceWithIntField is the package-internal version for unit tests.
func newTestResourceWithIntField(name, fieldName string) *Resource {
	return NewTestResourceWithIntField(name, fieldName)
}

// NewTestResourceWithFloatField creates a test resource with a single float field.
// Useful for testing string→number type conversions where the target is float.
// This function is exported for use in integration tests.
func NewTestResourceWithFloatField(name, fieldName string) *Resource {
	return NewTestResource(name, map[string]*schema.Schema{
		fieldName: {
			Type:     schema.TypeFloat,
			Optional: true,
		},
	})
}

// newTestResourceWithFloatField is the package-internal version for unit tests.
func newTestResourceWithFloatField(name, fieldName string) *Resource {
	return NewTestResourceWithFloatField(name, fieldName)
}

// NewTestResourceWithStringField creates a test resource with a single string field.
// Useful for testing number→string or boolean→string type conversions.
// This function is exported for use in integration tests.
func NewTestResourceWithStringField(name, fieldName string) *Resource {
	return NewTestResource(name, map[string]*schema.Schema{
		fieldName: {
			Type:     schema.TypeString,
			Optional: true,
		},
	})
}

// newTestResourceWithStringField is the package-internal version for unit tests.
func newTestResourceWithStringField(name, fieldName string) *Resource {
	return NewTestResourceWithStringField(name, fieldName)
}

// NewTestResourceWithBoolField creates a test resource with a single boolean field.
// Useful for testing type conversions involving boolean types or schema mismatches.
// This function is exported for use in integration tests.
func NewTestResourceWithBoolField(name, fieldName string) *Resource {
	return NewTestResource(name, map[string]*schema.Schema{
		fieldName: {
			Type:     schema.TypeBool,
			Optional: true,
		},
	})
}

// newTestResourceWithBoolField is the package-internal version for unit tests.
func newTestResourceWithBoolField(name, fieldName string) *Resource {
	return NewTestResourceWithBoolField(name, fieldName)
}

// LoadTestFixture loads a JSON test fixture from the testdata directory.
// The path is relative to pkg/config/testdata/.
// This function is exported for use in integration tests.
//
// Example: data := LoadTestFixture(t, "valid/field-addition.json")
func LoadTestFixture(t *testing.T, relativePath string) []byte {
	t.Helper()

	// Try multiple paths to find the testdata directory
	// 1. From repo root (when running go test from repo root) - pkg/config/testdata
	// 2. From repo root - tests/testdata (for integration test fixtures)
	// 3. From unit test package dir (pkg/config/)
	// 4. From integration test dir (tests/conversion/)
	paths := []string{
		filepath.Join("pkg", "config", "testdata", relativePath),
		filepath.Join("tests", "testdata", relativePath),
		filepath.Join("testdata", relativePath),
		filepath.Join("..", "..", "pkg", "config", "testdata", relativePath),
		filepath.Join("..", "testdata", relativePath),
	}

	var data []byte
	var err error
	for _, path := range paths {
		data, err = os.ReadFile(path) //nolint:gosec
		if err == nil {
			return data
		}
	}

	require.NoError(t, err, "failed to load test fixture: %s", relativePath)
	return data
}

// loadTestFixture is the package-internal version for unit tests.
func loadTestFixture(t *testing.T, relativePath string) []byte {
	return LoadTestFixture(t, relativePath)
}
