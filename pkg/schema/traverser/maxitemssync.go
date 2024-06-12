// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package traverser

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"
)

// maxItemsSync is a visitor to sync the MaxItems constraints from the
// Go schema to the JSON schema. We've observed that some MaxItems constraints
// in the JSON schemas are not set where the corresponding MaxItems constraints
// in the Go schemas are set to 1. This inconsistency results in some singleton
// lists not being properly converted in the MR API whereas at runtime we may
// try to invoke the corresponding Terraform conversion functions. This
// traverser can mitigate such inconsistencies by syncing the MaxItems
// constraints from the Go schema to the JSON schema.
type maxItemsSync struct {
	NoopTraverser

	jsonSchema TFResourceSchema
}

// NewMaxItemsSync returns a new schema traverser capable of
// syncing the MaxItems constraints from the Go schema to the specified JSON
// schema. This will ensure the generation time and the runtime schema MaxItems
// constraints will be consistent. This is needed only if the generation time
// and runtime schemas differ.
func NewMaxItemsSync(s TFResourceSchema) SchemaTraverser {
	return &maxItemsSync{
		jsonSchema: s,
	}
}

func (m *maxItemsSync) VisitResource(r *ResourceNode) error {
	// this visitor only works on singleton lists
	if (r.Schema.Type != schema.TypeList && r.Schema.Type != schema.TypeSet) || r.Schema.MaxItems != 1 {
		return nil
	}
	return errors.Wrapf(AccessSchema(m.jsonSchema[r.TFName], r.TFPath,
		func(s *schema.Schema) error {
			s.MaxItems = 1
			return nil
		}), "failed to access the schema element at path %v for resource %q", r.TFPath, r.TFName)
}
