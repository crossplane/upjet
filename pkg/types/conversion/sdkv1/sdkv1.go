/*
 Copyright 2022 The Crossplane Authors.

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

package sdkv1

import (
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
	schemav2 "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"
)

// GetV2ResourceMap returns a Terraform provider SDK v2 resource map for an
// input SDK v1 Terraform Resource Provider.
func GetV2ResourceMap(p terraform.ResourceProvider) map[string]*schemav2.Resource {
	v1map := p.(*schema.Provider).ResourcesMap
	v2map := make(map[string]*schemav2.Resource, len(v1map))
	for k, v := range v1map {
		v2map[k] = toV2Resource(v)
	}
	return v2map
}

func toV2Resource(v1res *schema.Resource) *schemav2.Resource {
	v1SchemaMap := v1res.Schema
	v2SchemaMap := make(map[string]*schemav2.Schema, len(v1SchemaMap))
	for k, v := range v1SchemaMap {
		v2SchemaMap[k] = toV2Schema(v)
	}
	v2Res := &schemav2.Resource{
		Schema:             v2SchemaMap,
		SchemaVersion:      v1res.SchemaVersion,
		DeprecationMessage: v1res.DeprecationMessage,
		Timeouts:           (*schemav2.ResourceTimeout)(v1res.Timeouts),
	}
	return v2Res
}

func toV2Schema(v1sch *schema.Schema) *schemav2.Schema {
	v2sch := &schemav2.Schema{
		Type:          schemav2.ValueType(v1sch.Type),
		ConfigMode:    schemav2.SchemaConfigMode(v1sch.ConfigMode),
		Optional:      v1sch.Optional,
		Required:      v1sch.Required,
		Default:       v1sch.Default,
		DefaultFunc:   schemav2.SchemaDefaultFunc(v1sch.DefaultFunc),
		Description:   v1sch.Description,
		InputDefault:  v1sch.InputDefault,
		Computed:      v1sch.Computed,
		ForceNew:      v1sch.ForceNew,
		StateFunc:     schemav2.SchemaStateFunc(v1sch.StateFunc),
		MaxItems:      v1sch.MaxItems,
		MinItems:      v1sch.MinItems,
		Set:           schemav2.SchemaSetFunc(v1sch.Set),
		ComputedWhen:  v1sch.ComputedWhen,
		ConflictsWith: v1sch.ConflictsWith,
		ExactlyOneOf:  v1sch.ExactlyOneOf,
		AtLeastOneOf:  v1sch.AtLeastOneOf,
		Deprecated:    v1sch.Deprecated,
		Sensitive:     v1sch.Sensitive,
	}
	v2sch.Type = schemav2.ValueType(v1sch.Type)
	switch v1sch.Type {
	case schema.TypeBool, schema.TypeInt, schema.TypeString, schema.TypeFloat:
		// no action required
	case schema.TypeMap, schema.TypeList, schema.TypeSet:
		switch et := v1sch.Elem.(type) {
		case schema.ValueType:
			v2sch.Elem = v1sch.Elem.(schemav2.ValueType)
		case *schema.Schema:
			v2sch.Elem = toV2Schema(et)
		case *schema.Resource:
			v2sch.Elem = toV2Resource(et)
		default:
			// Note(turkenh): We are defaulting to "String" as element type when
			// it is not explicitly provided as element type of a collection.
			v2sch.Elem = schemav2.TypeString
		}
	case schema.TypeInvalid:
		panic(errors.Errorf("invalid schema type %s", v1sch.Type.String()))
	default:
		panic(errors.Errorf("unexpected schema type %s", v1sch.Type.String()))
	}

	return v2sch
}
