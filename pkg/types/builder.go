/*
Copyright 2021 Upbound Inc.
*/

package types

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	twtypes "github.com/muvaf/typewriter/pkg/types"
	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"

	"github.com/upbound/upjet/pkg/config"
)

const (
	wildcard = "*"

	emptyStruct = "struct{}"
)

// Generated is a struct that holds generated types
type Generated struct {
	Types    []*types.Named
	Comments twtypes.Comments

	ForProviderType *types.Named
	AtProviderType  *types.Named

	ValidationRules string
}

// Builder is used to generate Go type equivalence of given Terraform schema.
type Builder struct {
	Package *types.Package

	genTypes        []*types.Named
	comments        twtypes.Comments
	validationRules string
}

// NewBuilder returns a new Builder.
func NewBuilder(pkg *types.Package) *Builder {
	return &Builder{
		Package:  pkg,
		comments: twtypes.Comments{},
	}
}

// Build returns parameters and observation types built out of Terraform schema.
func (g *Builder) Build(cfg *config.Resource) (Generated, error) {
	fp, ap, err := g.buildResource(cfg.TerraformResource, cfg, nil, nil, false, cfg.Kind)
	return Generated{
		Types:           g.genTypes,
		Comments:        g.comments,
		ForProviderType: fp,
		AtProviderType:  ap,
		ValidationRules: g.validationRules,
	}, errors.Wrapf(err, "cannot build the Types")
}

func (g *Builder) buildResource(res *schema.Resource, cfg *config.Resource, tfPath []string, xpPath []string, asBlocksMode bool, names ...string) (*types.Named, *types.Named, error) { //nolint:gocyclo
	// NOTE(muvaf): There can be fields in the same CRD with same name but in
	// different types. Since we generate the type using the field name, there
	// can be collisions. In order to be able to generate unique names consistently,
	// we need to process all fields in the same order all the time.
	keys := sortedKeys(res.Schema)

	typeNames, err := NewTypeNames(names, g.Package)
	if err != nil {
		return nil, nil, err
	}

	r := &resource{}
	for _, snakeFieldName := range keys {
		var reference *config.Reference
		ref, ok := cfg.References[fieldPath(append(tfPath, snakeFieldName))]
		// if a reference is configured and the field does not belong to status
		if ok && !IsObservation(res.Schema[snakeFieldName]) {
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
		f.AddToResource(g, r, typeNames)
	}

	paramType, obsType := g.AddToBuilder(typeNames, r)
	return paramType, obsType, nil
}

// AddToBuilder adds fields to the Builder.
func (g *Builder) AddToBuilder(typeNames *TypeNames, r *resource) (*types.Named, *types.Named) {
	// NOTE(muvaf): Not every struct has both computed and configurable fields,
	// so some types we generate here are empty and unnecessary. However,
	// there are valid types with zero fields and we don't have the information
	// to differentiate between valid zero fields and unnecessary one. So we generate
	// two structs for every complex type.
	// See usage of wafv2EmptySchema() in aws_wafv2_web_acl here:
	// https://github.com/hashicorp/terraform-provider-aws/blob/main/aws/wafv2_helper.go#L13
	paramType := types.NewNamed(typeNames.ParameterTypeName, types.NewStruct(r.paramFields, r.paramTags), nil)
	g.genTypes = append(g.genTypes, paramType)

	obsType := types.NewNamed(typeNames.ObservationTypeName, types.NewStruct(r.obsFields, r.obsTags), nil)
	g.genTypes = append(g.genTypes, obsType)

	for _, p := range r.topLevelRequiredParams {
		g.validationRules += "\n"
		g.validationRules += fmt.Sprintf(`// +kubebuilder:validation:XValidation:rule="self.managementPolicy == 'ObserveOnly' || has(self.forProvider.%s)",message="%s is a required parameter"`, p, p)
	}

	return paramType, obsType
}

func (g *Builder) buildSchema(f *Field, cfg *config.Resource, names []string, r *resource) (types.Type, error) { // nolint:gocyclo
	switch f.Schema.Type {
	case schema.TypeBool:
		return types.NewPointer(types.Universe.Lookup("bool").Type()), nil
	case schema.TypeFloat:
		return types.NewPointer(types.Universe.Lookup("float64").Type()), nil
	case schema.TypeInt:
		return types.NewPointer(types.Universe.Lookup("int64").Type()), nil
	case schema.TypeString:
		return types.NewPointer(types.Universe.Lookup("string").Type()), nil
	case schema.TypeMap, schema.TypeList, schema.TypeSet:
		names = append(names, f.Name.Camel)
		if f.Schema.Type != schema.TypeMap {
			// We don't want to have a many-to-many relationship in case of a Map, since we use SecretReference as
			// the type of XP field. In this case, we want to have a one-to-many relationship which is handled at
			// runtime in the controller.
			f.TerraformPaths = append(f.TerraformPaths, wildcard)
			f.CRDPaths = append(f.CRDPaths, wildcard)
		}
		var elemType types.Type
		switch et := f.Schema.Elem.(type) {
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
			newf, err := NewField(g, cfg, r, et, f.Name.Snake, f.TerraformPaths, f.CRDPaths, names, false)
			if err != nil {
				return nil, err
			}
			elemType = newf.FieldType
		case *schema.Resource:
			var asBlocksMode bool
			// TODO(muvaf): We skip the other type once we choose one of param
			// or obs types. This might cause some fields to be completely omitted.
			if f.Schema.ConfigMode == schema.SchemaConfigModeAttr {
				asBlocksMode = true
			}
			paramType, obsType, err := g.buildResource(et, cfg, f.TerraformPaths, f.CRDPaths, asBlocksMode, names...)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot infer type from resource schema of element type of %s", fieldPath(names))
			}

			switch {
			case IsObservation(f.Schema):
				if obsType == nil {
					return nil, errors.Errorf("element type of %s is computed but the underlying schema does not return observation type", fieldPath(names))
				}
				elemType = obsType
				// There are some types that are computed and not optional (observation field) but also has nested fields
				// that can go under spec. This check prevents the elimination of fields in parameter type, by checking
				// whether the schema in observation type has nested parameter (spec) fields.
				if paramType.Underlying().String() != emptyStruct {
					field := types.NewField(token.NoPos, g.Package, f.Name.Camel, types.NewSlice(paramType), false)
					r.addParameterField(f, field)
				}
			default:
				if paramType == nil {
					return nil, errors.Errorf("element type of %s is configurable but the underlying schema does not return a parameter type", fieldPath(names))
				}
				elemType = paramType
				// There are some types that are parameter field but also has nested fields that can go under status.
				// This check prevents the elimination of fields in observation type, by checking whether the schema in
				// parameter type has nested observation (status) fields.
				if obsType.Underlying().String() != emptyStruct {
					field := types.NewField(token.NoPos, g.Package, f.Name.Camel, types.NewSlice(obsType), false)
					r.addObservationField(f, field)
				}
			}
		// if unset
		// see: https://github.com/upbound/upjet/issues/177
		case nil:
			elemType = types.Universe.Lookup("string").Type()
		default:
			return nil, errors.Errorf("element type of %s should be either schema.Resource or schema.Schema", fieldPath(names))
		}

		// NOTE(muvaf): Maps and slices are already pointers, so we don't need to
		// wrap them even if they are optional.
		if f.Schema.Type == schema.TypeMap {
			return types.NewMap(types.Universe.Lookup("string").Type(), elemType), nil
		}
		return types.NewSlice(elemType), nil
	case schema.TypeInvalid:
		return nil, errors.Errorf("invalid schema type %s", f.Schema.Type.String())
	default:
		return nil, errors.Errorf("unexpected schema type %s", f.Schema.Type.String())
	}
}

// TypeNames represents the parameter and observation name of the resource.
type TypeNames struct {
	ParameterTypeName   *types.TypeName
	ObservationTypeName *types.TypeName
}

// NewTypeNames returns a new TypeNames object.
func NewTypeNames(fieldPaths []string, pkg *types.Package) (*TypeNames, error) {
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

	return &TypeNames{ParameterTypeName: paramName, ObservationTypeName: obsName}, nil
}

type resource struct {
	paramFields, obsFields []*types.Var
	paramTags, obsTags     []string
	topLevelRequiredParams []string
}

func (r *resource) addParameterField(f *Field, field *types.Var) {
	req := !f.Schema.Optional
	// Note(turkenh): We are collecting the top level required parameters that
	// are not identifier fields. This is for generating CEL validation rules for
	// those parameters and not to require them if the management policy is set
	// Observe Only. In other words, if we are not creating or managing the
	// resource, we don't need to provide those parameters which are:
	// - req => required
	// - !f.Identifier => not identifiers - i.e. region, zone, etc.
	// - len(f.CanonicalPaths) == 1 => top level, i.e. not a nested field
	if req && !f.Identifier && len(f.CanonicalPaths) == 1 {
		req = false
		r.topLevelRequiredParams = append(r.topLevelRequiredParams, f.TransformedName)
	}

	f.Comment.Required = &req
	if !req {
		r.paramTags = append(r.paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, f.JSONTag, f.TFTag))
	} else {
		// Required fields should not have omitempty tag in json tag.
		// TODO(muvaf): This overrides user intent if they provided custom
		// JSON tag.
		r.paramTags = append(r.paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, strings.TrimSuffix(f.JSONTag, ",omitempty"), f.TFTag))
	}
	r.paramFields = append(r.paramFields, field)
}

func (r *resource) addObservationField(f *Field, field *types.Var) {
	for _, obsF := range r.obsFields {
		if obsF.Name() == field.Name() {
			// If the field is already added, we don't add it again.
			// Some nested types could have been previously added as an
			// observation type while building their schema: https://github.com/upbound/upjet/blob/b89baca4ae24c8fbd8eb403c353ca18916093e5e/pkg/types/builder.go#L206
			return
		}
	}
	r.obsFields = append(r.obsFields, field)
	r.obsTags = append(r.obsTags, fmt.Sprintf(`json:"%s" tf:"%s"`, f.JSONTag, f.TFTag))
}

func (r *resource) addReferenceFields(g *Builder, paramName *types.TypeName, field *Field) {
	refFields, refTags := g.generateReferenceFields(paramName, field)
	r.paramTags = append(r.paramTags, refTags...)
	r.paramFields = append(r.paramFields, refFields...)
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

// IsObservation returns whether the specified Schema belongs to an observed
// attribute, i.e., whether it's a required computed field.
func IsObservation(s *schema.Schema) bool {
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
