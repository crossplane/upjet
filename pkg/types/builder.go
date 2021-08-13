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
func NewBuilder(name string, source *schema.Resource, pkg *types.Package) *Builder {
	return &Builder{
		Name:     name,
		Source:   source,
		Package:  pkg,
		genTypes: map[string]*types.Named{},
	}
}

// Builder is used to generate Go type equivalence of given Terraform schema.
type Builder struct {
	Source  *schema.Resource
	Name    string
	Package *types.Package

	genTypes map[string]*types.Named
}

// Build returns parameters and observation types built out of Terraform schema.
func (g *Builder) Build() ([]*types.Named, error) {
	_, _, err := g.buildResource(g.Name, g.Source)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot build the types")
	}
	if len(g.genTypes) == 0 {
		return nil, nil
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
		var field *types.Var
		switch sch.Type {
		case schema.TypeBool:
			field = types.NewField(token.NoPos, g.Package, fName, types.Universe.Lookup("bool").Type(), false)
		case schema.TypeFloat:
			field = types.NewField(token.NoPos, g.Package, fName, types.Universe.Lookup("float64").Type(), false)
		case schema.TypeInt:
			field = types.NewField(token.NoPos, g.Package, fName, types.Universe.Lookup("int64").Type(), false)
		case schema.TypeString:
			field = types.NewField(token.NoPos, g.Package, fName, types.Universe.Lookup("string").Type(), false)
		case schema.TypeList, schema.TypeSet, schema.TypeMap:
			var elemType types.Type
			switch r := sch.Elem.(type) {
			case *schema.Resource:
				lParamType, lObsType, err := g.buildResource(fName, r)
				if err != nil {
					return nil, nil, errors.Wrapf(err, "cannot build type for schema of element of list/set field named %s", n)
				}
				switch {
				// There are fields that are computed only if user doesn't supply
				// input, they should be in parameters.
				case sch.Computed && !sch.Optional:
					if lObsType == nil {
						return nil, nil, errors.Wrapf(err, "field is computed but the underlying schema does not return observation type: %s", n)
					}
					elemType = lObsType.Obj().Type()
				default:
					if lParamType == nil {
						return nil, nil, errors.Wrapf(err, "field is configurable but the underlying schema does not return parameter type: %s", n)
					}
					elemType = lParamType.Obj().Type()
				}
			case *schema.Schema:
				switch r.Type {
				case schema.TypeBool:
					elemType = types.Universe.Lookup("bool").Type()
				case schema.TypeFloat:
					elemType = types.Universe.Lookup("float64").Type()
				case schema.TypeInt:
					elemType = types.Universe.Lookup("int64").Type()
				case schema.TypeString:
					elemType = types.Universe.Lookup("string").Type()
				case schema.TypeMap, schema.TypeList, schema.TypeSet:
					return nil, nil, errors.Errorf("element of list cannot have non-basic schema: %s", n)
				case schema.TypeInvalid:
					continue
				}
			}
			if sch.Type == schema.TypeMap {
				field = types.NewField(token.NoPos, g.Package, fName, types.NewMap(types.Universe.Lookup("string").Type(), elemType), false)
			} else {
				// List and Sets both correspond to slices in Go.
				field = types.NewField(token.NoPos, g.Package, fName, types.NewSlice(elemType), false)
			}
		case schema.TypeInvalid:
			continue
		}
		switch {
		// If a field is not optional but computed, then it's definitely
		// an observation field.
		case sch.Computed && !sch.Optional:
			obsFields = append(obsFields, field)
			obsTags = append(obsTags, fmt.Sprintf("json:\"%s\" tf:\"%s\"", strcase.ToLowerCamel(n), n))
		default:
			paramFields = append(paramFields, field)
			paramTags = append(paramTags, fmt.Sprintf("json:\"%s\" tf:\"%s\"", strcase.ToLowerCamel(n), n))
		}
	}
	var paramType, obsType *types.Named
	if len(paramFields) != 0 {
		tName := types.NewTypeName(token.NoPos, g.Package, paramTypeName, nil)
		paramType = types.NewNamed(tName, types.NewStruct(paramFields, paramTags), nil)
		g.genTypes[paramType.Obj().Name()] = paramType
	}
	if len(obsFields) != 0 {
		tName := types.NewTypeName(token.NoPos, g.Package, obsTypeName, nil)
		obsType = types.NewNamed(tName, types.NewStruct(obsFields, obsTags), nil)
		g.genTypes[obsType.Obj().Name()] = obsType
	}
	return paramType, obsType, nil
}

func (g *Builder) buildSchema(namePrefix string, s *schema.Schema) (*types.Named, *types.Named, error) {
	return nil, nil, nil
}
