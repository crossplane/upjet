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
	_, _, err := g.buildResource(name, schema)
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

func (g *Builder) buildResource(namePrefix string, s *schema.Resource) (*types.Named, *types.Named, error) { // nolint:gocyclo
	paramTypeName := strcase.ToCamel(namePrefix) + "Parameters"
	obsTypeName := strcase.ToCamel(namePrefix) + "Observation"
	if g.genTypes[paramTypeName] != nil && g.genTypes[obsTypeName] != nil {
		return g.genTypes[paramTypeName], g.genTypes[obsTypeName], nil
	}
	var paramFields []*types.Var
	var paramTags []string
	var obsFields []*types.Var
	var obsTags []string
	for n, sch := range s.Schema {
		fName := strcase.ToCamel(n)
		fieldType, err := g.buildSchema(fName, sch)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "cannot infer type from schema of field %s", fName)
		}
		field := types.NewField(token.NoPos, g.Package, fName, fieldType, false)
		switch {
		// If a field is not optional but computed, then it's definitely
		// an observation field.
		case sch.Computed && !sch.Optional:
			obsFields = append(obsFields, field)
			obsTags = append(obsTags, fmt.Sprintf("json:\"%s\" tf:\"%s\"", strcase.ToLowerCamel(n), n))
		default:
			if sch.Optional {
				paramTags = append(paramTags, fmt.Sprintf("json:\"%s,omitempty\" tf:\"%s\"", strcase.ToLowerCamel(n), n))
			} else {
				paramTags = append(paramTags, fmt.Sprintf("json:\"%s\" tf:\"%s\"", strcase.ToLowerCamel(n), n))
			}
			paramFields = append(paramFields, field)
		}
	}
	// NOTE(muvaf): Types with zero fields are valid. See usage of wafv2EmptySchema()
	// in aws_wafv2_web_acl here: https://github.com/hashicorp/terraform-provider-aws/blob/main/aws/wafv2_helper.go#L13
	var paramType, obsType *types.Named
	paramName := types.NewTypeName(token.NoPos, g.Package, paramTypeName, nil)
	paramType = types.NewNamed(paramName, types.NewStruct(paramFields, paramTags), nil)
	g.genTypes[paramType.Obj().Name()] = paramType

	obsName := types.NewTypeName(token.NoPos, g.Package, obsTypeName, nil)
	obsType = types.NewNamed(obsName, types.NewStruct(obsFields, obsTags), nil)
	g.genTypes[obsType.Obj().Name()] = obsType

	return paramType, obsType, nil
}

func (g *Builder) buildSchema(typeNamePrefix string, sch *schema.Schema) (types.Type, error) {
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
			default:
				return nil, errors.Errorf("element type is basic but not one of known basic types")
			}
		case *schema.Schema:
			elemType, err = g.buildSchema(typeNamePrefix, et)
			if err != nil {
				return nil, errors.Wrap(err, "cannot infer type from schema of element type")
			}
		case *schema.Resource:
			// TODO(muvaf): We skip the other type once we choose one of param
			// or obs types. This might cause some fields to be completely omitted.
			paramType, obsType, err := g.buildResource(typeNamePrefix, et)
			if err != nil {
				return nil, errors.Wrap(err, "cannot infer type from resource schema of element type")
			}
			switch {
			// There are fields that are computed only if user doesn't supply
			// input, they should be in parameters.
			case sch.Computed && !sch.Optional:
				if obsType == nil {
					return nil, errors.Errorf("field is computed but the underlying schema does not return observation type: %s", typeNamePrefix)
				}
				elemType = obsType
			default:
				if paramType == nil {
					return nil, errors.Errorf("field is configurable but the underlying schema does not return parameter type: %s", typeNamePrefix)
				}
				elemType = paramType
			}
		default:
			return nil, errors.New("element type should be either schema.Resource or schema.Schema")
		}
		// NOTE(muvaf): Maps and slices are already pointers, so we don't need to
		// wrap them even if they are optional.
		switch sch.Type {
		case schema.TypeMap:
			return types.NewMap(types.Universe.Lookup("string").Type(), elemType), nil
		default:
			return types.NewSlice(elemType), nil
		}
	default:
		return nil, errors.Errorf("unexpected schema type %s", sch.Type)
	}
}
