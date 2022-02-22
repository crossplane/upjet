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
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	twtypes "github.com/muvaf/typewriter/pkg/types"
	"github.com/pkg/errors"

	"github.com/crossplane/terrajet/pkg/config"
	"github.com/crossplane/terrajet/pkg/types/comments"
	"github.com/crossplane/terrajet/pkg/types/name"
)

const (
	wildcard = "*"
)

// NewBuilder returns a new Builder.
func NewBuilder(pkg *types.Package) *Builder {
	return &Builder{
		Package:  pkg,
		comments: twtypes.Comments{},
	}
}

// Generated is a struct that holds generated types
type Generated struct {
	Types    []*types.Named
	Comments twtypes.Comments

	ForProviderType *types.Named
	AtProviderType  *types.Named
}

// Builder is used to generate Go type equivalence of given Terraform schema.
type Builder struct {
	Package *types.Package

	genTypes []*types.Named
	comments twtypes.Comments
}

type field struct {
	sch                            *schema.Schema
	fieldName                      name.Name
	comment                        *comments.Comment
	tfTag, jsonTag, fieldNameCamel string
	tfPaths, xpPaths, cnPaths      []string
	fieldType                      types.Type
}

type resource struct {
	paramFields, obsFields []*types.Var
	paramTags, obsTags     []string
}

// Build returns parameters and observation types built out of Terraform schema.
func (g *Builder) Build(cfg *config.Resource) (Generated, error) {
	fp, ap, err := g.buildResource(cfg.TerraformResource, cfg, nil, nil, cfg.Kind)
	return Generated{
		Types:           g.genTypes,
		Comments:        g.comments,
		ForProviderType: fp,
		AtProviderType:  ap,
	}, errors.Wrapf(err, "cannot build the Types")
}

func (g *Builder) buildResource(res *schema.Resource, cfg *config.Resource, tfPath []string, xpPath []string, names ...string) (*types.Named, *types.Named, error) { //nolint:gocyclo
	// NOTE(muvaf): There can be fields in the same CRD with same name but in
	// different types. Since we generate the type using the field name, there
	// can be collisions. In order to be able to generate unique names consistently,
	// we need to process all fields in the same order all the time.
	keys := sortedKeys(res.Schema)

	paramName, obsName, err := g.getParameterAndObservationName(names)
	if err != nil {
		return nil, nil, err
	}

	r := &resource{}
	for _, snakeFieldName := range keys {
		f, err := prepareField(res, snakeFieldName, tfPath, xpPath, names)
		if err != nil {
			return nil, nil, err
		}

		for _, ignoreField := range cfg.LateInitializer.IgnoredFields {
			// Convert configuration input from Terraform path to canonical path
			// Todo(turkenh/muvaf): Replace with a simple string conversion
			//  like GetIgnoredCanonicalFields where we just make each word
			//  between points camel case using names.go utilities. If the path
			//  doesn't match anything, it's no-op in late-init logic anyway.
			if ignoreField == fieldPath(f.tfPaths) {
				cfg.LateInitializer.AddIgnoredCanonicalFields(fieldPath(f.cnPaths))
			}
		}

		fieldType, err := g.buildSchema(f.sch, cfg, f.tfPaths, f.xpPaths, append(names, f.fieldName.Camel), f, r)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "cannot infer type from schema of field %s", f.fieldName.Snake)
		}
		f.fieldType = fieldType

		if ref, ok := cfg.References[fieldPath(f.tfPaths)]; ok {
			f.comment.Reference = ref
			f.sch.Optional = true
		}

		f.fieldNameCamel = f.fieldName.Camel
		if f.sch.Sensitive {
			drop, err := f.handleSensitiveField(cfg)
			if err != nil {
				return nil, nil, err
			}
			if drop {
				continue
			}
		}

		f.setTags()

		field := types.NewField(token.NoPos, g.Package, f.fieldNameCamel, f.fieldType, false)
		// NOTE(muvaf): If a field is not optional but computed, then it's
		// definitely an observation field.
		// If it's optional but also computed, then it means the field has a server
		// side default but user can change it, so it needs to go to parameters.
		switch {
		case isObservation(f.sch):
			r.addObservationField(f, field)
		default:
			r.addParameterField(f, field)
		}

		if ref, ok := cfg.References[fieldPath(f.tfPaths)]; ok {
			g.addReferenceFields(r, paramName, field, ref)
		}

		g.comments.AddFieldComment(paramName, f.fieldNameCamel, f.comment.Build())
	}

	// NOTE(muvaf): Not every struct has both computed and configurable fields,
	// so some types we generate here are empty and unnecessary. However,
	// there are valid types with zero fields and we don't have the information
	// to differentiate between valid zero fields and unnecessary one. So we generate
	// two structs for every complex type.
	// See usage of wafv2EmptySchema() in aws_wafv2_web_acl here:
	// https://github.com/hashicorp/terraform-provider-aws/blob/main/aws/wafv2_helper.go#L13
	paramType := types.NewNamed(paramName, types.NewStruct(r.paramFields, r.paramTags), nil)
	g.genTypes = append(g.genTypes, paramType)

	obsType := types.NewNamed(obsName, types.NewStruct(r.obsFields, r.obsTags), nil)
	g.genTypes = append(g.genTypes, obsType)

	return paramType, obsType, nil
}

func (g *Builder) getParameterAndObservationName(names []string) (*types.TypeName, *types.TypeName, error) {
	paramTypeName, err := g.generateTypeName("Parameters", names...)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "cannot generate parameters type name of %s", fieldPath(names))
	}
	paramName := types.NewTypeName(token.NoPos, g.Package, paramTypeName, nil)

	obsTypeName, err := g.generateTypeName("Observation", names...)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "cannot generate observation type name of %s", fieldPath(names))
	}
	obsName := types.NewTypeName(token.NoPos, g.Package, obsTypeName, nil)

	// We insert them to the package scope so that the type name calculations in
	// recursive calls are checked against their upper level type's name as well.
	g.Package.Scope().Insert(paramName)
	g.Package.Scope().Insert(obsName)

	return paramName, obsName, nil
}

func prepareField(res *schema.Resource, snakeFieldName string, tfPath, xpPath, names []string) (*field, error) {
	f := &field{
		sch:       res.Schema[snakeFieldName],
		fieldName: name.NewFromSnake(snakeFieldName),
	}

	comment, err := comments.New(f.sch.Description)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot build comment for description: %s", f.sch.Description)
	}
	f.comment = comment
	f.tfTag = fmt.Sprintf("%s,omitempty", f.fieldName.Snake)
	f.jsonTag = fmt.Sprintf("%s,omitempty", f.fieldName.LowerCamelComputed)

	// Terraform paths, e.g. { "lifecycle_rule", "*", "transition", "*", "days" } for https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket#lifecycle_rule
	f.tfPaths = append(tfPath, f.fieldName.Snake) // nolint:gocritic
	// Crossplane paths, e.g. {"lifecycleRule", "*", "transition", "*", "days"}
	f.xpPaths = append(xpPath, f.fieldName.LowerCamel) // nolint:gocritic
	// Canonical paths, e.g. {"LifecycleRule", "Transition", "Days"}
	f.cnPaths = append(names[1:], f.fieldName.Camel) // nolint:gocritic

	return f, nil
}

func (f *field) handleSensitiveField(cfg *config.Resource) (bool, error) { // nolint:gocyclo
	if isObservation(f.sch) {
		cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(f.tfPaths), "status.atProvider."+fieldPathWithWildcard(f.xpPaths))
		// Drop an observation field from schema if it is sensitive.
		// Data will be stored in connection details secret
		return true, nil
	}
	sfx := "SecretRef"
	cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(f.tfPaths), "spec.forProvider."+fieldPathWithWildcard(f.xpPaths)+sfx)
	// todo(turkenh): do we need to support other field types as sensitive?
	if f.fieldType.String() != "string" && f.fieldType.String() != "*string" && f.fieldType.String() != "[]string" &&
		f.fieldType.String() != "[]*string" && f.fieldType.String() != "map[string]string" && f.fieldType.String() != "map[string]*string" {
		return false, fmt.Errorf(`got type %q for field %q, only types "string", "*string", []string, []*string, "map[string]string" and "map[string]*string" supported as sensitive`, f.fieldType.String(), f.fieldNameCamel)
	}
	// Replace a parameter field with secretKeyRef if it is sensitive.
	// If it is an observation field, it will be dropped.
	// Data will be loaded from the referenced secret key.
	f.fieldNameCamel += sfx

	f.tfTag = "-"
	switch f.fieldType.String() {
	case "string", "*string":
		f.fieldType = typeSecretKeySelector
	case "[]string", "[]*string":
		f.fieldType = types.NewSlice(typeSecretKeySelector)
	case "map[string]string", "map[string]*string":
		f.fieldType = types.NewMap(types.Universe.Lookup("string").Type(), typeSecretKeySelector)
	}
	f.jsonTag = name.NewFromCamel(f.fieldNameCamel).LowerCamelComputed
	if f.sch.Optional {
		f.fieldType = types.NewPointer(f.fieldType)
		f.jsonTag += ",omitempty"
	}
	return false, nil
}

func (f *field) setTags() {
	if f.comment.TerrajetOptions.FieldTFTag != nil {
		f.tfTag = *f.comment.TerrajetOptions.FieldTFTag
	}
	if f.comment.TerrajetOptions.FieldJSONTag != nil {
		f.jsonTag = *f.comment.TerrajetOptions.FieldJSONTag
	}
}

func (r *resource) addObservationField(f *field, field *types.Var) {
	r.obsFields = append(r.obsFields, field)
	r.obsTags = append(r.obsTags, fmt.Sprintf(`json:"%s" tf:"%s"`, f.jsonTag, f.tfTag))
}

func (r *resource) addParameterField(f *field, field *types.Var) {
	if f.sch.Optional {
		r.paramTags = append(r.paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, f.jsonTag, f.tfTag))
	} else {
		// Required fields should not have omitempty tag in json tag.
		// TODO(muvaf): This overrides user intent if they provided custom
		// JSON tag.
		r.paramTags = append(r.paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, strings.TrimSuffix(f.jsonTag, ",omitempty"), f.tfTag))
	}
	req := !f.sch.Optional
	f.comment.Required = &req
	r.paramFields = append(r.paramFields, field)
}

func (g *Builder) addReferenceFields(r *resource, paramName *types.TypeName, field *types.Var, ref config.Reference) {
	refFields, refTags := g.generateReferenceFields(paramName, field, ref)
	r.paramTags = append(r.paramTags, refTags...)
	r.paramFields = append(r.paramFields, refFields...)
}

func (g *Builder) buildSchema(sch *schema.Schema, cfg *config.Resource, tfPath []string, xpPath []string, names []string, rootField *field, r *resource) (types.Type, error) { // nolint:gocyclo
	switch sch.Type {
	case schema.TypeBool:
		return types.NewPointer(types.Universe.Lookup("bool").Type()), nil
	case schema.TypeFloat:
		return types.NewPointer(types.Universe.Lookup("float64").Type()), nil
	case schema.TypeInt:
		return types.NewPointer(types.Universe.Lookup("int64").Type()), nil
	case schema.TypeString:
		return types.NewPointer(types.Universe.Lookup("string").Type()), nil
	case schema.TypeMap, schema.TypeList, schema.TypeSet:
		tfPath = append(tfPath, wildcard)
		xpPath = append(xpPath, wildcard)
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
				return nil, errors.Errorf("element type of %s is basic but not one of known basic types", fieldPath(names))
			}
		case *schema.Schema:
			elemType, err = g.buildSchema(et, cfg, tfPath, xpPath, names, rootField, r)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot infer type from schema of element type of %s", fieldPath(names))
			}
		case *schema.Resource:
			// TODO(muvaf): We skip the other type once we choose one of param
			// or obs types. This might cause some fields to be completely omitted.
			paramType, obsType, err := g.buildResource(et, cfg, tfPath, xpPath, names...)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot infer type from resource schema of element type of %s", fieldPath(names))
			}

			// NOTE(muvaf): If a field is not optional but computed, then it's
			// definitely an observation field.
			// If it's optional but also computed, then it means the field has a server
			// side default but user can change it, so it needs to go to parameters.
			switch {
			case isObservation(sch):
				if obsType == nil {
					return nil, errors.Errorf("element type of %s is computed but the underlying schema does not return observation type", fieldPath(names))
				}
				elemType = obsType
				if paramType.Underlying().String() != "struct{}" {
					field := types.NewField(token.NoPos, g.Package, rootField.fieldName.Camel, paramType, false)
					r.addParameterField(rootField, field)
				}
			default:
				if paramType == nil {
					return nil, errors.Errorf("element type of %s is configurable but the underlying schema does not return a parameter type", fieldPath(names))
				}
				elemType = paramType
				if obsType.Underlying().String() != "struct{}" {
					field := types.NewField(token.NoPos, g.Package, rootField.fieldName.Camel, obsType, false)
					r.addObservationField(rootField, field)
				}
			}
		// if unset
		// see: https://github.com/crossplane/terrajet/issues/177
		case nil:
			elemType = types.Universe.Lookup("string").Type()
		default:
			return nil, errors.Errorf("element type of %s should be either schema.Resource or schema.Schema", fieldPath(names))
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
func (g *Builder) generateTypeName(suffix string, names ...string) (string, error) {
	n := names[len(names)-1] + suffix
	for i := len(names) - 2; i >= 0; i-- {
		if g.Package.Scope().Lookup(n) == nil {
			return n, nil
		}
		n = names[i] + n
	}
	if g.Package.Scope().Lookup(n) == nil {
		return n, nil
	}
	// start from 2 considering the 1st of this type is the one without an
	// index.
	for i := 2; i < 10; i++ {
		nn := fmt.Sprintf("%s_%d", n, i)
		if g.Package.Scope().Lookup(nn) == nil {
			return nn, nil
		}
	}
	return "", errors.Errorf("could not generate a unique name for %s", n)
}

func isObservation(s *schema.Schema) bool {
	return s.Computed && !s.Optional
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

func fieldPath(parts []string) string {
	seg := make(fieldpath.Segments, len(parts))
	for i, p := range parts {
		if p == wildcard {
			continue
		}
		seg[i] = fieldpath.Field(p)
	}
	return seg.String()
}

func fieldPathWithWildcard(parts []string) string {
	seg := make(fieldpath.Segments, len(parts))
	for i, p := range parts {
		seg[i] = fieldpath.Field(p)
	}
	return seg.String()
}
