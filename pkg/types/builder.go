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

	emptyStruct = "struct{}"
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

// Field represents a field that is built from the Terraform schema.
// It contains the go field related information such as tags, field type, comment.
type Field struct {
	Sch                            *schema.Schema
	FieldName                      name.Name
	Comment                        *comments.Comment
	TfTag, JSONTag, FieldNameCamel string
	TfPaths, XpPaths, CnPaths      []string
	FieldType                      types.Type
	AsBlocksMode                   bool
	Reference                      *config.Reference
}

// NewField returns a constructed Field object.
func NewField(g *Builder, cfg *config.Resource, r *resource, sch *schema.Schema, snakeFieldName string, tfPath, xpPath, names []string, asBlocksMode bool) (*Field, error) {
	f := &Field{
		Sch:            sch,
		FieldName:      name.NewFromSnake(snakeFieldName),
		FieldNameCamel: name.NewFromSnake(snakeFieldName).Camel,
		AsBlocksMode:   asBlocksMode,
	}

	comment, err := comments.New(f.Sch.Description)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot build comment for description: %s", f.Sch.Description)
	}
	f.Comment = comment
	f.TfTag = fmt.Sprintf("%s,omitempty", f.FieldName.Snake)
	f.JSONTag = fmt.Sprintf("%s,omitempty", f.FieldName.LowerCamelComputed)

	// Terraform paths, e.g. { "lifecycle_rule", "*", "transition", "*", "days" } for https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket#lifecycle_rule
	f.TfPaths = append(tfPath, f.FieldName.Snake) // nolint:gocritic
	// Crossplane paths, e.g. {"lifecycleRule", "*", "transition", "*", "days"}
	f.XpPaths = append(xpPath, f.FieldName.LowerCamelComputed) // nolint:gocritic
	// Canonical paths, e.g. {"LifecycleRule", "Transition", "Days"}
	f.CnPaths = append(names[1:], f.FieldName.Camel) // nolint:gocritic

	f.handleIgnoredFields(cfg)
	fieldType, err := g.buildSchema(f, cfg, names, r)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot infer type from schema of field %s", f.FieldName.Snake)
	}
	f.FieldType = fieldType

	return f, nil
}

// NewSensitiveField returns a constructed sensitive Field object.
func NewSensitiveField(g *Builder, cfg *config.Resource, r *resource, sch *schema.Schema, snakeFieldName string, tfPath, xpPath, names []string, asBlocksMode bool) (*Field, bool, error) {
	f, err := NewField(g, cfg, r, sch, snakeFieldName, tfPath, xpPath, names, asBlocksMode)
	if err != nil {
		return nil, false, err
	}

	drop, err := f.handleSensitiveField(cfg)
	if err != nil {
		return nil, false, err
	}
	if drop {
		return nil, true, nil
	}
	return f, false, nil
}

// NewReferenceField returns a constructed reference Field object.
func NewReferenceField(g *Builder, cfg *config.Resource, r *resource, sch *schema.Schema, ref *config.Reference, snakeFieldName string, tfPath, xpPath, names []string, asBlocksMode bool) (*Field, error) {
	f, err := NewField(g, cfg, r, sch, snakeFieldName, tfPath, xpPath, names, asBlocksMode)
	if err != nil {
		return nil, err
	}
	f.Reference = ref

	f.Comment.Reference = *ref
	f.Sch.Optional = true

	return f, nil
}

// Namer represents the parameter and observation name of the resource.
type Namer struct {
	ParameterTypeName   *types.TypeName
	ObservationTypeName *types.TypeName
}

// NewNamer returns a new Namer object.
func NewNamer(fieldPaths []string, pkg *types.Package) (*Namer, error) {
	paramTypeName, err := generateTypeName("Parameters", pkg, fieldPaths...)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot generate parameters type name of %s", fieldPath(fieldPaths))
	}
	paramName := types.NewTypeName(token.NoPos, pkg, paramTypeName, nil)

	obsTypeName, err := generateTypeName("Observation", pkg, fieldPaths...)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot generate observation type name of %s", fieldPath(fieldPaths))
	}
	obsName := types.NewTypeName(token.NoPos, pkg, obsTypeName, nil)

	// We insert them to the package scope so that the type name calculations in
	// recursive calls are checked against their upper level type's name as well.
	pkg.Scope().Insert(paramName)
	pkg.Scope().Insert(obsName)

	return &Namer{ParameterTypeName: paramName, ObservationTypeName: obsName}, nil
}

type resource struct {
	paramFields, obsFields []*types.Var
	paramTags, obsTags     []string
}

// Build returns parameters and observation types built out of Terraform schema.
func (g *Builder) Build(cfg *config.Resource) (Generated, error) {
	fp, ap, err := g.buildResource(cfg.TerraformResource, cfg, nil, nil, false, cfg.Kind)
	return Generated{
		Types:           g.genTypes,
		Comments:        g.comments,
		ForProviderType: fp,
		AtProviderType:  ap,
	}, errors.Wrapf(err, "cannot build the Types")
}

func (g *Builder) buildResource(res *schema.Resource, cfg *config.Resource, tfPath []string, xpPath []string, asBlocksMode bool, names ...string) (*types.Named, *types.Named, error) { //nolint:gocyclo
	// NOTE(muvaf): There can be fields in the same CRD with same name but in
	// different types. Since we generate the type using the field name, there
	// can be collisions. In order to be able to generate unique names consistently,
	// we need to process all fields in the same order all the time.
	keys := sortedKeys(res.Schema)

	namer, err := NewNamer(names, g.Package)
	if err != nil {
		return nil, nil, err
	}

	r := &resource{}
	for _, snakeFieldName := range keys {
		var reference *config.Reference
		ref, ok := cfg.References[fieldPath(append(tfPath, snakeFieldName))]
		if ok {
			reference = &ref
		}

		var f *Field
		switch {
		case res.Schema[snakeFieldName].Sensitive:
			var drop bool
			f, drop, err = NewSensitiveField(g, cfg, r, res.Schema[snakeFieldName], snakeFieldName, tfPath, xpPath, names, asBlocksMode)
			if err != nil {
				return nil, nil, err
			}
			if drop {
				continue
			}
		case reference != nil:
			f, err = NewReferenceField(g, cfg, r, res.Schema[snakeFieldName], reference, snakeFieldName, tfPath, xpPath, names, asBlocksMode)
			if err != nil {
				return nil, nil, err
			}
		default:
			f, err = NewField(g, cfg, r, res.Schema[snakeFieldName], snakeFieldName, tfPath, xpPath, names, asBlocksMode)
			if err != nil {
				return nil, nil, err
			}
		}

		f.AddToResource(g, r, namer)
	}

	paramType, obsType := g.AddToBuilder(namer, r)
	return paramType, obsType, nil
}

func (f *Field) handleSensitiveField(cfg *config.Resource) (bool, error) { // nolint:gocyclo
	if isObservation(f.Sch) {
		cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(f.TfPaths), "status.atProvider."+fieldPathWithWildcard(f.XpPaths))
		// Drop an observation field from schema if it is sensitive.
		// Data will be stored in connection details secret
		return true, nil
	}
	sfx := "SecretRef"
	cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(f.TfPaths), "spec.forProvider."+fieldPathWithWildcard(f.XpPaths)+sfx)
	// todo(turkenh): do we need to support other field types as sensitive?
	if f.FieldType.String() != "string" && f.FieldType.String() != "*string" && f.FieldType.String() != "[]string" &&
		f.FieldType.String() != "[]*string" && f.FieldType.String() != "map[string]string" && f.FieldType.String() != "map[string]*string" {
		return false, fmt.Errorf(`got type %q for field %q, only types "string", "*string", []string, []*string, "map[string]string" and "map[string]*string" supported as sensitive`, f.FieldType.String(), f.FieldNameCamel)
	}
	// Replace a parameter field with secretKeyRef if it is sensitive.
	// If it is an observation field, it will be dropped.
	// Data will be loaded from the referenced secret key.
	f.FieldNameCamel += sfx

	f.TfTag = "-"
	switch f.FieldType.String() {
	case "string", "*string":
		f.FieldType = typeSecretKeySelector
	case "[]string", "[]*string":
		f.FieldType = types.NewSlice(typeSecretKeySelector)
	case "map[string]string", "map[string]*string":
		f.FieldType = types.NewMap(types.Universe.Lookup("string").Type(), typeSecretKeySelector)
	}
	f.JSONTag = name.NewFromCamel(f.FieldNameCamel).LowerCamelComputed
	if f.Sch.Optional {
		f.FieldType = types.NewPointer(f.FieldType)
		f.JSONTag += ",omitempty"
	}
	return false, nil
}

func (f *Field) handleIgnoredFields(cfg *config.Resource) {
	for _, ignoreField := range cfg.LateInitializer.IgnoredFields {
		// Convert configuration input from Terraform path to canonical path
		// Todo(turkenh/muvaf): Replace with a simple string conversion
		//  like GetIgnoredCanonicalFields where we just make each word
		//  between points camel case using names.go utilities. If the path
		//  doesn't match anything, it's no-op in late-init logic anyway.
		if ignoreField == fieldPath(f.TfPaths) {
			cfg.LateInitializer.AddIgnoredCanonicalFields(fieldPath(f.CnPaths))
		}
	}
}

func (r *resource) addParameterField(f *Field, field *types.Var) {
	if f.Sch.Optional {
		r.paramTags = append(r.paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, f.JSONTag, f.TfTag))
	} else {
		// Required fields should not have omitempty tag in json tag.
		// TODO(muvaf): This overrides user intent if they provided custom
		// JSON tag.
		r.paramTags = append(r.paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, strings.TrimSuffix(f.JSONTag, ",omitempty"), f.TfTag))
	}
	req := !f.Sch.Optional
	f.Comment.Required = &req
	r.paramFields = append(r.paramFields, field)
}

func (r *resource) addObservationField(f *Field, field *types.Var) {
	r.obsFields = append(r.obsFields, field)
	r.obsTags = append(r.obsTags, fmt.Sprintf(`json:"%s" tf:"%s"`, f.JSONTag, f.TfTag))
}

func (r *resource) addReferenceFields(g *Builder, paramName *types.TypeName, field *types.Var, ref config.Reference) {
	refFields, refTags := g.generateReferenceFields(paramName, field, ref)
	r.paramTags = append(r.paramTags, refTags...)
	r.paramFields = append(r.paramFields, refFields...)
}

// AddToResource adds built field to the resource.
func (f *Field) AddToResource(g *Builder, r *resource, namer *Namer) {
	if f.Comment.TerrajetOptions.FieldTFTag != nil {
		f.TfTag = *f.Comment.TerrajetOptions.FieldTFTag
	}
	if f.Comment.TerrajetOptions.FieldJSONTag != nil {
		f.JSONTag = *f.Comment.TerrajetOptions.FieldJSONTag
	}

	field := types.NewField(token.NoPos, g.Package, f.FieldNameCamel, f.FieldType, false)
	switch {
	case isObservation(f.Sch):
		r.addObservationField(f, field)
	default:
		if f.AsBlocksMode {
			f.TfTag = strings.TrimSuffix(f.TfTag, ",omitempty")
		}
		r.addParameterField(f, field)
	}

	if f.Reference != nil {
		r.addReferenceFields(g, namer.ParameterTypeName, field, *f.Reference)
	}

	g.comments.AddFieldComment(namer.ParameterTypeName, f.FieldNameCamel, f.Comment.Build())
}

// AddToBuilder adds fields to the Builder.
func (g *Builder) AddToBuilder(namer *Namer, r *resource) (*types.Named, *types.Named) {
	// NOTE(muvaf): Not every struct has both computed and configurable fields,
	// so some types we generate here are empty and unnecessary. However,
	// there are valid types with zero fields and we don't have the information
	// to differentiate between valid zero fields and unnecessary one. So we generate
	// two structs for every complex type.
	// See usage of wafv2EmptySchema() in aws_wafv2_web_acl here:
	// https://github.com/hashicorp/terraform-provider-aws/blob/main/aws/wafv2_helper.go#L13
	paramType := types.NewNamed(namer.ParameterTypeName, types.NewStruct(r.paramFields, r.paramTags), nil)
	g.genTypes = append(g.genTypes, paramType)

	obsType := types.NewNamed(namer.ObservationTypeName, types.NewStruct(r.obsFields, r.obsTags), nil)
	g.genTypes = append(g.genTypes, obsType)

	return paramType, obsType
}

func (g *Builder) buildSchema(f *Field, cfg *config.Resource, names []string, r *resource) (types.Type, error) { // nolint:gocyclo
	rootField := f.DeepCopy()
	switch f.Sch.Type {
	case schema.TypeBool:
		return types.NewPointer(types.Universe.Lookup("bool").Type()), nil
	case schema.TypeFloat:
		return types.NewPointer(types.Universe.Lookup("float64").Type()), nil
	case schema.TypeInt:
		return types.NewPointer(types.Universe.Lookup("int64").Type()), nil
	case schema.TypeString:
		return types.NewPointer(types.Universe.Lookup("string").Type()), nil
	case schema.TypeMap, schema.TypeList, schema.TypeSet:
		names = append(names, f.FieldName.Camel)
		f.TfPaths = append(f.TfPaths, wildcard)
		f.XpPaths = append(f.XpPaths, wildcard)
		var elemType types.Type
		switch et := f.Sch.Elem.(type) {
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
			newf, err := NewField(g, cfg, r, et, f.FieldName.Snake, f.TfPaths, f.XpPaths, names, false)
			if err != nil {
				return nil, err
			}
			elemType = newf.FieldType
		case *schema.Resource:
			var asBlocksMode bool
			// TODO(muvaf): We skip the other type once we choose one of param
			// or obs types. This might cause some fields to be completely omitted.
			if f.Sch.ConfigMode == schema.SchemaConfigModeAttr {
				asBlocksMode = true
			}
			paramType, obsType, err := g.buildResource(et, cfg, f.TfPaths, f.XpPaths, asBlocksMode, names...)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot infer type from resource schema of element type of %s", fieldPath(names))
			}

			// NOTE(muvaf): If a field is not optional but computed, then it's
			// definitely an observation field.
			// If it's optional but also computed, then it means the field has a server
			// side default but user can change it, so it needs to go to parameters.
			switch {
			case isObservation(f.Sch):
				if obsType == nil {
					return nil, errors.Errorf("element type of %s is computed but the underlying schema does not return observation type", fieldPath(names))
				}
				elemType = obsType
				if paramType.Underlying().String() != emptyStruct {
					field := types.NewField(token.NoPos, g.Package, rootField.FieldName.Camel, types.NewSlice(paramType), false)
					r.addParameterField(rootField, field)
				}
			default:
				if paramType == nil {
					return nil, errors.Errorf("element type of %s is configurable but the underlying schema does not return a parameter type", fieldPath(names))
				}
				elemType = paramType
				if obsType.Underlying().String() != emptyStruct {
					field := types.NewField(token.NoPos, g.Package, rootField.FieldName.Camel, types.NewSlice(obsType), false)
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
		if f.Sch.Type == schema.TypeMap {
			return types.NewMap(types.Universe.Lookup("string").Type(), elemType), nil
		}
		return types.NewSlice(elemType), nil
	case schema.TypeInvalid:
		return nil, errors.Errorf("invalid schema type %s", f.Sch.Type.String())
	default:
		return nil, errors.Errorf("unexpected schema type %s", f.Sch.Type.String())
	}
}

// generateTypeName generates a unique name for the type if its original name
// is used by another one. It adds the former field names recursively until it
// finds a unique name.
func generateTypeName(suffix string, pkg *types.Package, names ...string) (string, error) {
	n := names[len(names)-1] + suffix
	for i := len(names) - 2; i >= 0; i-- {
		if pkg.Scope().Lookup(n) == nil {
			return n, nil
		}
		n = names[i] + n
	}
	if pkg.Scope().Lookup(n) == nil {
		return n, nil
	}
	// start from 2 considering the 1st of this type is the one without an
	// index.
	for i := 2; i < 10; i++ {
		nn := fmt.Sprintf("%s_%d", n, i)
		if pkg.Scope().Lookup(nn) == nil {
			return nn, nil
		}
	}
	return "", errors.Errorf("could not generate a unique name for %s", n)
}

func isObservation(s *schema.Schema) bool {
	// NOTE(muvaf): If a field is not optional but computed, then it's
	// definitely an observation field.
	// If it's optional but also computed, then it means the field has a server
	// side default but user can change it, so it needs to go to parameters.
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

// DeepCopy generates a deep copy of Field
func (f *Field) DeepCopy() *Field { //nolint: gocyclo
	var cp Field = *f
	if f.Sch != nil {
		cp.Sch = new(schema.Schema)
		*cp.Sch = *f.Sch
		if f.Sch.ComputedWhen != nil {
			cp.Sch.ComputedWhen = make([]string, len(f.Sch.ComputedWhen))
			copy(cp.Sch.ComputedWhen, f.Sch.ComputedWhen)
		}
		if f.Sch.ConflictsWith != nil {
			cp.Sch.ConflictsWith = make([]string, len(f.Sch.ConflictsWith))
			copy(cp.Sch.ConflictsWith, f.Sch.ConflictsWith)
		}
		if f.Sch.ExactlyOneOf != nil {
			cp.Sch.ExactlyOneOf = make([]string, len(f.Sch.ExactlyOneOf))
			copy(cp.Sch.ExactlyOneOf, f.Sch.ExactlyOneOf)
		}
		if f.Sch.AtLeastOneOf != nil {
			cp.Sch.AtLeastOneOf = make([]string, len(f.Sch.AtLeastOneOf))
			copy(cp.Sch.AtLeastOneOf, f.Sch.AtLeastOneOf)
		}
		if f.Sch.RequiredWith != nil {
			cp.Sch.RequiredWith = make([]string, len(f.Sch.RequiredWith))
			copy(cp.Sch.RequiredWith, f.Sch.RequiredWith)
		}
	}
	if f.Comment != nil {
		cp.Comment = new(comments.Comment)
		*cp.Comment = *f.Comment
		if f.Comment.Options.TerrajetOptions.FieldTFTag != nil {
			cp.Comment.Options.TerrajetOptions.FieldTFTag = new(string)
			*cp.Comment.Options.TerrajetOptions.FieldTFTag = *f.Comment.Options.TerrajetOptions.FieldTFTag
		}
		if f.Comment.Options.TerrajetOptions.FieldJSONTag != nil {
			cp.Comment.Options.TerrajetOptions.FieldJSONTag = new(string)
			*cp.Comment.Options.TerrajetOptions.FieldJSONTag = *f.Comment.Options.TerrajetOptions.FieldJSONTag
		}
		if f.Comment.Options.KubebuilderOptions.Required != nil {
			cp.Comment.Options.KubebuilderOptions.Required = new(bool)
			*cp.Comment.Options.KubebuilderOptions.Required = *f.Comment.Options.KubebuilderOptions.Required
		}
		if f.Comment.Options.KubebuilderOptions.Minimum != nil {
			cp.Comment.Options.KubebuilderOptions.Minimum = new(int)
			*cp.Comment.Options.KubebuilderOptions.Minimum = *f.Comment.Options.KubebuilderOptions.Minimum
		}
		if f.Comment.Options.KubebuilderOptions.Maximum != nil {
			cp.Comment.Options.KubebuilderOptions.Maximum = new(int)
			*cp.Comment.Options.KubebuilderOptions.Maximum = *f.Comment.Options.KubebuilderOptions.Maximum
		}
	}
	if f.TfPaths != nil {
		cp.TfPaths = make([]string, len(f.TfPaths))
		copy(cp.TfPaths, f.TfPaths)
	}
	if f.XpPaths != nil {
		cp.XpPaths = make([]string, len(f.XpPaths))
		copy(cp.XpPaths, f.XpPaths)
	}
	if f.CnPaths != nil {
		cp.CnPaths = make([]string, len(f.CnPaths))
		copy(cp.CnPaths, f.CnPaths)
	}
	return &cp
}
