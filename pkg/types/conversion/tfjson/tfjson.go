// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package tfjson

import (
	tfjson "github.com/hashicorp/terraform-json"
	schemav2 "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
)

// NOTE: these are custom workaround additions to the terraform SDK schema.ValueType
// to support generating TF plugin framework nested attribute and dynamic pseudo-types.
// Those are not utilized during runtime, just for facilitating CRD generation.
// https://github.com/hashicorp/terraform-plugin-sdk/blob/42d3ce13b57d6e3696687d59c39f31f07edf6c79/helper/schema/valuetype.go#L14
// TODO: remove these when we generate the types directly from JSON schema
// or framework schemas
const (
	// SchemaTypeObject is the custom schema.ValueType used for
	// distinguishing FW nested attribute types
	SchemaTypeObject = schemav2.ValueType(9)
	// SchemaTypeDynamic is the custom schema.ValueType used for
	// distinguishing dynamic-pseudo value types
	SchemaTypeDynamic = schemav2.ValueType(10)
)

// GetV2ResourceMap converts input resource schemas with
// "terraform-json" representation to terraform-plugin-sdk representation which
// is what Upjet expects today.
//
// What we are trying to achieve here is to convert a lower level
// representation of resource schema map, e.g. output of `terraform providers schema -json`
// to plugin sdk representation. This is mostly the opposite of what the
// following method is doing: https://github.com/hashicorp/terraform-plugin-sdk/blob/7e0a333644f1971a936995677b7a106140a0659f/helper/schema/core_schema.go#L43
//
// Ideally, we should not rely on plugin SDK types in Upjet at all but only
// work with types in https://github.com/hashicorp/terraform-json which is
// there exactly for this purpose, an external representation of Terraform
// schemas. This conversion aims to be an intermediate step for that ultimate
// goal.
func GetV2ResourceMap(resourceSchemas map[string]*tfjson.Schema) map[string]*schemav2.Resource {
	v2map := make(map[string]*schemav2.Resource, len(resourceSchemas))
	for k, v := range resourceSchemas {
		v2map[k] = v2ResourceFromTFJSONSchema(v)
	}
	return v2map
}

func v2ResourceFromTFJSONSchema(s *tfjson.Schema) *schemav2.Resource {
	v2Res := &schemav2.Resource{SchemaVersion: int(s.Version)} //nolint:gosec
	if s.Block == nil {
		return v2Res
	}

	toSchemaMap := make(map[string]*schemav2.Schema, len(s.Block.Attributes)+len(s.Block.NestedBlocks))

	for k, v := range s.Block.Attributes {
		if v.AttributeNestedType != nil {
			toSchemaMap[k] = tfJSONNestedAttributeTypeToV2Schema(v)
		} else {
			toSchemaMap[k] = tfJSONAttributeToV2Schema(v)
		}
	}
	for k, v := range s.Block.NestedBlocks {
		// CRUD timeouts are not part of the generated MR API,
		// they cannot be dynamically configured and they are determined by either
		// the underlying Terraform resource configuration or the upjet resource
		// configuration. Please also see config.Resource.OperationTimeouts.
		if k == schemav2.TimeoutsConfigKey {
			continue
		}
		toSchemaMap[k] = tfJSONBlockTypeToV2Schema(v)
	}

	v2Res.Schema = toSchemaMap
	v2Res.Description = s.Block.Description
	v2Res.DeprecationMessage = deprecatedMessage(s.Block.Deprecated)
	return v2Res
}

func tfJSONAttributeToV2Schema(attr *tfjson.SchemaAttribute) *schemav2.Schema {
	v2sch := &schemav2.Schema{
		Optional:    attr.Optional,
		Required:    attr.Required,
		Description: attr.Description,
		Computed:    attr.Computed,
		Deprecated:  deprecatedMessage(attr.Deprecated),
		Sensitive:   attr.Sensitive,
	}
	if err := schemaV2TypeFromCtyType(attr.AttributeType, v2sch); err != nil {
		panic(err)
	}
	return v2sch
}

func tfJSONNestedAttributeTypeToV2Schema(nestedAttr *tfjson.SchemaAttribute) *schemav2.Schema {
	na := nestedAttr.AttributeNestedType
	v2sch := &schemav2.Schema{
		MinItems: int(na.MinItems),
		MaxItems: int(na.MaxItems),
		Required: nestedAttr.Required,
		Optional: nestedAttr.Optional,
	}
	switch na.NestingMode { //nolint:exhaustive
	case tfjson.SchemaNestingModeSet:
		v2sch.Type = schemav2.TypeSet
	case tfjson.SchemaNestingModeList:
		v2sch.Type = schemav2.TypeList
	case tfjson.SchemaNestingModeMap:
		v2sch.Type = schemav2.TypeMap
	case tfjson.SchemaNestingModeSingle, tfjson.SchemaNestingModeGroup:
		v2sch.Type = SchemaTypeObject
		v2sch.MinItems = 0
		if v2sch.Required {
			v2sch.MinItems = 1
		}
		v2sch.MaxItems = 1
	default:
		panic("unhandled nesting mode: " + na.NestingMode)
	}
	res := &schemav2.Resource{}
	res.Schema = make(map[string]*schemav2.Schema, len(na.Attributes))
	for key, attr := range na.Attributes {
		if attr.AttributeNestedType != nil {
			res.Schema[key] = tfJSONNestedAttributeTypeToV2Schema(attr)
		} else {
			res.Schema[key] = tfJSONAttributeToV2Schema(attr)
		}
	}
	v2sch.Elem = res
	return v2sch
}

func tfJSONBlockTypeToV2Schema(nb *tfjson.SchemaBlockType) *schemav2.Schema { //nolint:gocyclo
	v2sch := &schemav2.Schema{
		MinItems: int(nb.MinItems), //nolint:gosec
		MaxItems: int(nb.MaxItems), //nolint:gosec
	}
	// Note(turkenh): Schema representation returned by the cli for block types
	// does not have optional or computed fields. So, we are trying to infer
	// those fields by doing the opposite of what is done here:
	// https://github.com/hashicorp/terraform-plugin-sdk/blob/6461ac6e9044a44157c4e2c8aec0f1ab7efc2055/helper/schema/core_schema.go#L204
	v2sch.Computed = false
	v2sch.Optional = false
	if nb.MinItems == 0 {
		v2sch.Optional = true
	}
	if nb.MinItems == 0 && nb.MaxItems == 0 {
		v2sch.Computed = true
	}

	switch nb.NestingMode { //nolint:exhaustive
	case tfjson.SchemaNestingModeSet:
		v2sch.Type = schemav2.TypeSet
	case tfjson.SchemaNestingModeList:
		v2sch.Type = schemav2.TypeList
	case tfjson.SchemaNestingModeMap:
		v2sch.Type = schemav2.TypeMap
	case tfjson.SchemaNestingModeSingle:
		v2sch.Type = schemav2.TypeList
		v2sch.MinItems = 0
		// TODO(erhan): not sure whether we need this
		// the block itself can be optional, even if some child attribute
		// or block is required
		v2sch.Required = hasRequiredChild(nb)
		v2sch.Optional = !v2sch.Required
		if v2sch.Required {
			v2sch.MinItems = 1
		}
		v2sch.MaxItems = 1
	default:
		panic("unhandled nesting mode: " + nb.NestingMode)
	}

	if nb.Block == nil {
		return v2sch
	}

	v2sch.Description = nb.Block.Description
	v2sch.Deprecated = deprecatedMessage(nb.Block.Deprecated)

	res := &schemav2.Resource{}
	res.Schema = make(map[string]*schemav2.Schema, len(nb.Block.Attributes)+len(nb.Block.NestedBlocks))
	for key, attr := range nb.Block.Attributes {
		res.Schema[key] = tfJSONAttributeToV2Schema(attr)
	}
	for key, block := range nb.Block.NestedBlocks {
		// Please note that unlike the resource-level CRUD timeout configuration
		// blocks (as mentioned above), we will generate the timeouts parameters
		// for any nested configuration blocks, *if they exist*.
		// We can prevent them here, but they are different than the resource's
		// top-level CRUD timeouts, so we have opted to generate them.
		res.Schema[key] = tfJSONBlockTypeToV2Schema(block)
	}
	v2sch.Elem = res
	return v2sch
}

// checks whether the given tfjson.SchemaBlockType has any required children.
// Children which are themselves blocks (nested blocks) are
// checked recursively.
func hasRequiredChild(nb *tfjson.SchemaBlockType) bool {
	if nb.Block == nil {
		return false
	}
	for _, a := range nb.Block.Attributes {
		if a == nil {
			continue
		}
		if a.Required {
			return true
		}
	}
	for _, b := range nb.Block.NestedBlocks {
		if b == nil {
			continue
		}
		if hasRequiredChild(b) {
			return true
		}
	}
	return false
}

func schemaV2TypeFromCtyType(typ cty.Type, schema *schemav2.Schema) error { //nolint:gocyclo
	configMode := schemav2.SchemaConfigModeAuto

	switch {
	case typ.IsPrimitiveType():
		schema.Type = primitiveToV2SchemaType(typ)
	case typ.IsCollectionType():
		var elemType any
		et := typ.ElementType()
		switch {
		case et.IsPrimitiveType():
			elemType = &schemav2.Schema{
				Type:     primitiveToV2SchemaType(et),
				Computed: schema.Computed,
				Optional: schema.Optional,
			}
		case et.IsCollectionType():
			elemType = &schemav2.Schema{
				Type:     collectionToV2SchemaType(et),
				Computed: schema.Computed,
				Optional: schema.Optional,
			}
			if err := schemaV2TypeFromCtyType(et, elemType.(*schemav2.Schema)); err != nil {
				return err
			}
		case et.IsObjectType():
			configMode = schemav2.SchemaConfigModeAttr
			res := &schemav2.Resource{}
			res.Schema = make(map[string]*schemav2.Schema, len(et.AttributeTypes()))
			for key, attrTyp := range et.AttributeTypes() {
				sch := &schemav2.Schema{
					Computed: schema.Computed,
					Optional: schema.Optional,
				}
				if et.AttributeOptional(key) {
					sch.Optional = true
				}

				if err := schemaV2TypeFromCtyType(attrTyp, sch); err != nil {
					return err
				}
				res.Schema[key] = sch
			}
			elemType = res
		default:
			return errors.Errorf("unexpected cty.Type %s", typ.GoString())
		}
		schema.ConfigMode = configMode
		schema.Type = collectionToV2SchemaType(typ)
		schema.Elem = elemType
	case typ.IsTupleType():
		return errors.New("cannot convert cty TupleType to schema v2 type")
	case typ.IsObjectType():
		res := &schemav2.Resource{}
		res.Schema = make(map[string]*schemav2.Schema, len(typ.AttributeTypes()))
		for key, attrTyp := range typ.AttributeTypes() {
			sch := &schemav2.Schema{
				Computed: schema.Computed,
				Optional: schema.Optional,
				Required: schema.Required,
			}
			if err := schemaV2TypeFromCtyType(attrTyp, sch); err != nil {
				return err
			}
			res.Schema[key] = sch
		}
		schema.ConfigMode = configMode
		schema.Type = SchemaTypeObject
		schema.Elem = res
		schema.MaxItems = 1
		schema.MinItems = 0
	case typ.Equals(cty.DynamicPseudoType):
		schema.Type = SchemaTypeDynamic
	}

	return nil
}

func primitiveToV2SchemaType(typ cty.Type) schemav2.ValueType {
	switch {
	case typ.Equals(cty.String):
		return schemav2.TypeString
	case typ.Equals(cty.Number):
		// TODO(turkenh): Figure out handling floats with IntOrString on type
		//  builder side
		return schemav2.TypeFloat
	case typ.Equals(cty.Bool):
		return schemav2.TypeBool
	}
	return schemav2.TypeInvalid
}

func collectionToV2SchemaType(typ cty.Type) schemav2.ValueType {
	switch {
	case typ.IsSetType():
		return schemav2.TypeSet
	case typ.IsListType():
		return schemav2.TypeList
	case typ.IsMapType():
		return schemav2.TypeMap
	}
	return schemav2.TypeInvalid
}

func deprecatedMessage(deprecated bool) string {
	if deprecated {
		return "deprecated"
	}
	return ""
}
