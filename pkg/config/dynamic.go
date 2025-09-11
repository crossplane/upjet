// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func frameworkDynamicTypeAttributePaths(name string, resource fwresource.Resource) ([]string, error) {
	if resource == nil {
		return nil, errors.New("resource is nil")
	}
	schemaResp := fwresource.SchemaResponse{}
	resource.Schema(context.TODO(), fwresource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		return nil, errors.Errorf("failed to retrieve framework schema for resource %q: %v", name, schemaResp.Diagnostics)
	}
	tfType := schemaResp.Schema.Type().TerraformType(context.TODO())
	paths := traverseTypeForDynamicType(tfType, "")
	for i, path := range paths {
		paths[i] = strings.TrimPrefix(path, ".")
	}
	return paths, nil
}

// traverseTypeForDynamicType recursively traverses a type and finds all paths with DynamicType
func traverseTypeForDynamicType(t tftypes.Type, prefix string) []string {
	var dynamicPaths []string

	// Check if the type is DynamicType
	if t.Is(tftypes.DynamicPseudoType) {
		dynamicPaths = append(dynamicPaths, prefix)
		return dynamicPaths
	}
	// If the type is a nested object, dive into its attributes
	switch {
	case t.Is(tftypes.Object{}):
		for key, attr := range t.(tftypes.Object).AttributeTypes {
			// Recursively traverse each attribute
			dynamicPaths = append(dynamicPaths, traverseTypeForDynamicType(attr, prefix+"."+key)...)
		}
	case t.Is(tftypes.List{}):
		// For list types, recursively inspect the element type
		dynamicPaths = append(dynamicPaths, traverseTypeForDynamicType(t.(tftypes.List).ElementType, prefix+"[*]")...)
	case t.Is(tftypes.Map{}):
		// For map types, recursively inspect the element type
		dynamicPaths = append(dynamicPaths, traverseTypeForDynamicType(t.(tftypes.Map).ElementType, prefix+"[*]")...)
	case t.Is(tftypes.Set{}):
		// For set types, recursively inspect the element type
		dynamicPaths = append(dynamicPaths, traverseTypeForDynamicType(t.(tftypes.Set).ElementType, prefix+"[*]")...)
	case t.Is(tftypes.Tuple{}):
		for _, elementType := range t.(tftypes.Tuple).ElementTypes {
			dynamicPaths = append(dynamicPaths, traverseTypeForDynamicType(elementType, prefix+"[*]")...)
		}
	case t.Is(tftypes.Bool), t.Is(tftypes.Number), t.Is(tftypes.String):
		// skip primitive types
	default:
		// above should be exhaustive, if not we will be notified at generation time
		panic(fmt.Sprintf("unexpected type %T", t))
	}

	return dynamicPaths
}
