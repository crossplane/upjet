/*
 Copyright 2022 Upbound Inc.

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

package tfjson

import (
	tfjson "github.com/hashicorp/terraform-json"
	schemav2 "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
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
	v2Res := &schemav2.Resource{SchemaVersion: int(s.Version)}
	if s.Block == nil {
		return v2Res
	}

	toSchemaMap := make(map[string]*schemav2.Schema, len(s.Block.Attributes)+len(s.Block.NestedBlocks))

	for k, v := range s.Block.Attributes {
		toSchemaMap[k] = tfJSONAttributeToV2Schema(v)
	}
	for k, v := range s.Block.NestedBlocks {
		// Note(turkenh): We see resource timeouts here as NestingModeSingle.
		// However, in plugin SDK resource timeouts is not part of resource
		// schema map but set as a separate field. So, we just need to ignore
		// here.
		// https://github.com/hashicorp/terraform-plugin-sdk/blob/6461ac6e9044a44157c4e2c8aec0f1ab7efc2055/helper/schema/core_schema.go#L315
		if v.NestingMode == tfjson.SchemaNestingModeSingle {
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

func tfJSONBlockTypeToV2Schema(nb *tfjson.SchemaBlockType) *schemav2.Schema { //nolint:gocyclo
	v2sch := &schemav2.Schema{
		MinItems: int(nb.MinItems),
		MaxItems: int(nb.MaxItems),
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

	switch nb.NestingMode {
	case tfjson.SchemaNestingModeSet:
		v2sch.Type = schemav2.TypeSet
	case tfjson.SchemaNestingModeList:
		v2sch.Type = schemav2.TypeList
	case tfjson.SchemaNestingModeMap:
		v2sch.Type = schemav2.TypeMap
	case tfjson.SchemaNestingModeSingle, tfjson.SchemaNestingModeGroup:
		panic("unexpected nesting mode: " + nb.NestingMode)
	default:
		panic("unknown nesting mode: " + nb.NestingMode)
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
		// Note(turkenh): We see resource timeouts here as NestingModeSingle.
		// However, in plugin SDK resource timeouts is not part of resource
		// schema map but set as a separate field. So, we just need to ignore
		// here.
		// https://github.com/hashicorp/terraform-plugin-sdk/blob/6461ac6e9044a44157c4e2c8aec0f1ab7efc2055/helper/schema/core_schema.go#L315
		if block.NestingMode == tfjson.SchemaNestingModeSingle {
			continue
		}
		res.Schema[key] = tfJSONBlockTypeToV2Schema(block)
	}
	v2sch.Elem = res
	return v2sch
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
	case typ.Equals(cty.DynamicPseudoType):
		return errors.New("cannot convert cty DynamicPseudoType to schema v2 type")
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
