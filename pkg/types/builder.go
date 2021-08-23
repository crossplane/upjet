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

package types

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"

	"github.com/pkg/errors"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/iancoleman/strcase"
)

// NewBuilder returns a new Builder.
func NewBuilder(pkg *types.Package) *Builder {
	return &Builder{
		Package:  pkg,
		genTypes: map[string]*types.Named{},
	}
}

// Builder is used to generate Go type equivalence of given Terraform schema.
type Builder struct {
	Package *types.Package

	genTypes map[string]*types.Named
}

// Build returns parameters and observation types built out of Terraform schema.
func (g *Builder) Build(name string, schema *schema.Resource) ([]*types.Named, error) {
	_, _, err := g.buildResource(schema, name)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot build the types")
	}
	if len(g.genTypes) == 0 {
		return nil, errors.Errorf("no type has been generated from resource %s", name)
	}
	result := make([]*types.Named, len(g.genTypes))
	i := 0
	for _, t := range g.genTypes {
		result[i] = t
		i++
	}
	return result, nil
}

func (g *Builder) buildResource(res *schema.Resource, names ...string) (*types.Named, *types.Named, error) {
	// NOTE(muvaf): There can be fields in the same CRD with same name but in
	// different types. Since we generate the type using the field name, there
	// can be collisions. In order to be able to generate unique names consistently,
	// we need to process all fields in the same order all the time.
	keys := sortedKeys(res.Schema)

	var paramFields []*types.Var
	var paramTags []string
	var obsFields []*types.Var
	var obsTags []string
	for _, snakeFieldName := range keys {
		sch := res.Schema[snakeFieldName]
		fieldName := strcase.ToCamel(snakeFieldName)
		lowerCamelFieldName := strcase.ToLowerCamel(snakeFieldName)
		fieldType, err := g.buildSchema(sch, append(names, fieldName))
		if err != nil {
			return nil, nil, errors.Wrapf(err, "cannot infer type from schema of field %s", fieldName)
		}
		field := types.NewField(token.NoPos, g.Package, fieldName, fieldType, false)

		// NOTE(muvaf): If a field is not optional but computed, then it's
		// definitely an observation field.
		// If it's optional but also computed, then it means the field has a server
		// side default but user can change it, so it needs to go to parameters.
		switch {
		case sch.Computed && !sch.Optional:
			obsFields = append(obsFields, field)
			obsTags = append(obsTags, fmt.Sprintf("json:\"%s\" tf:\"%s\"", lowerCamelFieldName, snakeFieldName))
		default:
			if sch.Optional {
				paramTags = append(paramTags, fmt.Sprintf("json:\"%s,omitempty\" tf:\"%s\"", lowerCamelFieldName, snakeFieldName))
			} else {
				paramTags = append(paramTags, fmt.Sprintf("json:\"%s\" tf:\"%s\"", lowerCamelFieldName, snakeFieldName))
			}
			paramFields = append(paramFields, field)
		}
	}

	// NOTE(muvaf): Not every struct has both computed and configurable fields,
	// so some of the types we generate here are empty and unnecessary. However,
	// there are valid types with zero fields and we don't have the information
	// to differentiate between valid zero fields and unnecessary one. So we generate
	// two structs for every complex type.
	// See usage of wafv2EmptySchema() in aws_wafv2_web_acl here:
	// https://github.com/hashicorp/terraform-provider-aws/blob/main/aws/wafv2_helper.go#L13
	var paramType, obsType *types.Named

	paramTypeName := g.generateTypeName("Parameters", names...)
	paramName := types.NewTypeName(token.NoPos, g.Package, paramTypeName, nil)
	paramType = types.NewNamed(paramName, types.NewStruct(paramFields, paramTags), nil)
	g.genTypes[paramType.Obj().Name()] = paramType

	obsTypeName := g.generateTypeName("Observation", names...)
	obsName := types.NewTypeName(token.NoPos, g.Package, obsTypeName, nil)
	obsType = types.NewNamed(obsName, types.NewStruct(obsFields, obsTags), nil)
	g.genTypes[obsType.Obj().Name()] = obsType

	return paramType, obsType, nil
}

func (g *Builder) buildSchema(sch *schema.Schema, names []string) (types.Type, error) { // nolint:gocyclo
	switch sch.Type {
	case schema.TypeBool:
		if sch.Optional {
			return types.NewPointer(types.Universe.Lookup("bool").Type()), nil
		}
		return types.Universe.Lookup("bool").Type(), nil
	case schema.TypeFloat:
		if sch.Optional {
			return types.NewPointer(types.Universe.Lookup("float64").Type()), nil
		}
		return types.Universe.Lookup("float64").Type(), nil
	case schema.TypeInt:
		if sch.Optional {
			return types.NewPointer(types.Universe.Lookup("int64").Type()), nil
		}
		return types.Universe.Lookup("int64").Type(), nil
	case schema.TypeString:
		if sch.Optional {
			return types.NewPointer(types.Universe.Lookup("string").Type()), nil
		}
		return types.Universe.Lookup("string").Type(), nil
	case schema.TypeMap, schema.TypeList, schema.TypeSet:
		var elemType types.Type
		var err error
		switch et := sch.Elem.(type) {
		case schema.ValueType:
			switch et {
			case schema.TypeBool:
				elemType = types.Universe.Lookup("bool").Type()
			case schema.TypeFloat:
				elemType = types.Universe.Lookup("float64").Type()
			case schema.TypeInt:
				elemType = types.Universe.Lookup("int64").Type()
			case schema.TypeString:
				elemType = types.Universe.Lookup("string").Type()
			case schema.TypeMap, schema.TypeList, schema.TypeSet, schema.TypeInvalid:
				return nil, errors.Errorf("element type of %s is basic but not one of known basic types", fieldPath(names...))
			}
		case *schema.Schema:
			elemType, err = g.buildSchema(et, names)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot infer type from schema of element type of %s", fieldPath(names...))
			}
		case *schema.Resource:
			// TODO(muvaf): We skip the other type once we choose one of param
			// or obs types. This might cause some fields to be completely omitted.
			paramType, obsType, err := g.buildResource(et, names...)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot infer type from resource schema of element type of %s", fieldPath(names...))
			}

			// NOTE(muvaf): If a field is not optional but computed, then it's
			// definitely an observation field.
			// If it's optional but also computed, then it means the field has a server
			// side default but user can change it, so it needs to go to parameters.
			switch {
			case sch.Computed && !sch.Optional:
				if obsType == nil {
					return nil, errors.Errorf("element type of %s is computed but the underlying schema does not return observation type", fieldPath(names...))
				}
				elemType = obsType
			default:
				if paramType == nil {
					return nil, errors.Errorf("fielement type of %s is configurable but the underlying schema does not return parameter type: %s", fieldPath(names...))
				}
				elemType = paramType
			}
		default:
			return nil, errors.Errorf("element type of %s should be either schema.Resource or schema.Schema", fieldPath(names...))
		}

		// NOTE(muvaf): Maps and slices are already pointers, so we don't need to
		// wrap them even if they are optional.
		if sch.Type == schema.TypeMap {
			return types.NewMap(types.Universe.Lookup("string").Type(), elemType), nil
		}
		return types.NewSlice(elemType), nil
	case schema.TypeInvalid:
		return nil, errors.Errorf("invalid schema type %s", sch.Type.String())
	default:
		return nil, errors.Errorf("unexpected schema type %s", sch.Type.String())
	}
}

// generateTypeName generates a unique name for the type if its original name
// is used by another one. It adds the former field names recursively until it
// finds a unique name.
func (g *Builder) generateTypeName(suffix string, names ...string) string {
	n := names[len(names)-1] + suffix
	for i := len(names) - 2; i > 0; i-- {
		if _, ok := g.genTypes[n]; !ok {
			break
		}
		n = names[i] + n
	}
	return n
}

func sortedKeys(m map[string]*schema.Schema) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	return keys
}

func fieldPath(names ...string) string {
	path := ""
	for _, n := range names {
		path += "." + n
	}
	return path
}
