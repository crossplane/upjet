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
	"k8s.io/utils/pointer"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"

	"github.com/upbound/upjet/pkg/config"
)

const (
	wildcard = "*"

	emptyStruct = "struct{}"

	// ref: https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation-rules
	celEscapeSequence = "__%s__"
)

var (
	// ref: https://github.com/google/cel-spec/blob/v0.6.0/doc/langdef.md#syntax
	celReservedKeywords = []string{"true", "false", "null", "in", "as", "break", "const", "continue",
		"else", "for", "function", "if", "import", "let", "loop", "package", "namespace", "return", "var",
		"void", "while"}
)

// Generated is a struct that holds generated types
type Generated struct {
	Types    []*types.Named
	Comments twtypes.Comments

	ForProviderType  *types.Named
	InitProviderType *types.Named
	AtProviderType   *types.Named

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
	fp, ap, ip, err := g.buildResource(cfg.TerraformResource, cfg, nil, nil, nil, false, cfg.Kind)
	return Generated{
		Types:            g.genTypes,
		Comments:         g.comments,
		ForProviderType:  fp,
		InitProviderType: ip,
		AtProviderType:   ap,
		ValidationRules:  g.validationRules,
	}, errors.Wrapf(err, "cannot build the Types")
}

func (g *Builder) buildResource(res *schema.Resource, cfg *config.Resource, tfPath, xpPath, celPath []string, asBlocksMode bool, names ...string) (*types.Named, *types.Named, *types.Named, error) { //nolint:gocyclo
	// NOTE(muvaf): There can be fields in the same CRD with same name but in
	// different types. Since we generate the type using the field name, there
	// can be collisions. In order to be able to generate unique names consistently,
	// we need to process all fields in the same order all the time.
	keys := sortedKeys(res.Schema)

	typeNames, err := NewTypeNames(names, g.Package)
	if err != nil {
		return nil, nil, nil, err
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
			f, drop, err = NewSensitiveField(g, cfg, r, res.Schema[snakeFieldName], snakeFieldName, tfPath, xpPath, celPath, names, asBlocksMode)
			if err != nil {
				return nil, nil, nil, err
			}
			if drop {
				continue
			}
		case reference != nil:
			f, err = NewReferenceField(g, cfg, r, res.Schema[snakeFieldName], reference, snakeFieldName, tfPath, xpPath, celPath, names, asBlocksMode)
			if err != nil {
				return nil, nil, nil, err
			}
		default:
			f, err = NewField(g, cfg, r, res.Schema[snakeFieldName], snakeFieldName, tfPath, xpPath, celPath, asBlocksMode, names)
			if err != nil {
				return nil, nil, nil, err
			}
		}
		f.AddToResource(g, r, typeNames)
	}

	paramType, obsType, initType := g.AddToBuilder(typeNames, r)
	return paramType, obsType, initType, nil
}

// AddToBuilder adds fields to the Builder.
func (g *Builder) AddToBuilder(typeNames *TypeNames, r *resource) (*types.Named, *types.Named, *types.Named) {
	// NOTE(muvaf): Not every struct has both computed and configurable fields,
	// so some types we generate here are empty and unnecessary. However,
	// there are valid types with zero fields and we don't have the information
	// to differentiate between valid zero fields and unnecessary one. So we generate
	// two structs for every complex type.
	// See usage of wafv2EmptySchema() in aws_wafv2_web_acl here:
	// https://github.com/hashicorp/terraform-provider-aws/blob/main/aws/wafv2_helper.go#L13
	paramType := types.NewNamed(typeNames.ParameterTypeName, types.NewStruct(r.paramFields, r.paramTags), nil)
	g.genTypes = append(g.genTypes, paramType)

	initType := types.NewNamed(typeNames.InitTypeName, types.NewStruct(r.initFields, r.initTags), nil)
	g.genTypes = append(g.genTypes, initType)

	obsType := types.NewNamed(typeNames.ObservationTypeName, types.NewStruct(r.obsFields, r.obsTags), nil)
	g.genTypes = append(g.genTypes, obsType)

	for _, p := range r.celRules {
		g.validationRules += "\n"
		if p.initProviderRule != "" {
			g.validationRules += fmt.Sprintf(`// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || %s || %s",message="%s is a required parameter"`, p.rule, p.initProviderRule, p.path)
		} else {
			g.validationRules += fmt.Sprintf(`// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || %s",message="%s is a required parameter"`, p.rule, p.path)
		}
	}

	return paramType, obsType, initType
}

func (g *Builder) buildSchema(f *Field, cfg *config.Resource, names []string, r *resource) (types.Type, types.Type, error) { // nolint:gocyclo
	switch f.Schema.Type {
	case schema.TypeBool:
		return types.NewPointer(types.Universe.Lookup("bool").Type()), nil, nil
	case schema.TypeFloat:
		return types.NewPointer(types.Universe.Lookup("float64").Type()), nil, nil
	case schema.TypeInt:
		return types.NewPointer(types.Universe.Lookup("int64").Type()), nil, nil
	case schema.TypeString:
		return types.NewPointer(types.Universe.Lookup("string").Type()), nil, nil
	case schema.TypeMap, schema.TypeList, schema.TypeSet:
		names = append(names, f.Name.Camel)
		if f.Schema.Type != schema.TypeMap {
			// We don't want to have a many-to-many relationship in case of a Map, since we use SecretReference as
			// the type of XP field. In this case, we want to have a one-to-many relationship which is handled at
			// runtime in the controller.
			f.TerraformPaths = append(f.TerraformPaths, wildcard)
			f.CRDPaths = append(f.CRDPaths, wildcard)
			f.CELPaths = append(f.CELPaths, wildcard)
		}
		var elemType types.Type
		var initElemType types.Type
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
				return nil, nil, errors.Errorf("element type of %s is basic but not one of known basic types", fieldPath(names))
			}
			initElemType = elemType
		case *schema.Schema:
			newf, err := NewField(g, cfg, r, et, f.Name.Snake, f.TerraformPaths, f.CRDPaths, f.CELPaths, false, names)
			if err != nil {
				return nil, nil, err
			}
			elemType = newf.FieldType
			initElemType = elemType
		case *schema.Resource:
			var asBlocksMode bool
			// TODO(muvaf): We skip the other type once we choose one of param
			// or obs types. This might cause some fields to be completely omitted.
			if f.Schema.ConfigMode == schema.SchemaConfigModeAttr {
				asBlocksMode = true
			}
			paramType, obsType, initType, err := g.buildResource(et, cfg, f.TerraformPaths, f.CRDPaths, f.CELPaths, asBlocksMode, names...)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "cannot infer type from resource schema of element type of %s", fieldPath(names))
			}
			initElemType = initType

			switch {
			case IsObservation(f.Schema):
				if obsType == nil {
					return nil, nil, errors.Errorf("element type of %s is computed but the underlying schema does not return observation type", fieldPath(names))
				}
				elemType = obsType
				// There are some types that are computed and not optional (observation field) but also has nested fields
				// that can go under spec. This check prevents the elimination of fields in parameter type, by checking
				// whether the schema in observation type has nested parameter (spec) fields.
				if paramType.Underlying().String() != emptyStruct {
					field := types.NewField(token.NoPos, g.Package, f.Name.Camel, types.NewSlice(paramType), false)
					r.addParameterField(f, field)
					r.addInitField(f, field, g.Package)
				}
			default:
				if paramType == nil {
					return nil, nil, errors.Errorf("element type of %s is configurable but the underlying schema does not return a parameter type", fieldPath(names))
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
			initElemType = elemType
		default:
			return nil, nil, errors.Errorf("element type of %s should be either schema.Resource or schema.Schema", fieldPath(names))
		}

		// NOTE(muvaf): Maps and slices are already pointers, so we don't need to
		// wrap them even if they are optional.
		if f.Schema.Type == schema.TypeMap {
			return types.NewMap(types.Universe.Lookup("string").Type(), elemType), types.NewMap(types.Universe.Lookup("string").Type(), initElemType), nil
		}
		return types.NewSlice(elemType), types.NewSlice(initElemType), nil
	case schema.TypeInvalid:
		return nil, nil, errors.Errorf("invalid schema type %s", f.Schema.Type.String())
	default:
		return nil, nil, errors.Errorf("unexpected schema type %s", f.Schema.Type.String())
	}
}

// TypeNames represents the parameter and observation name of the resource.
type TypeNames struct {
	ParameterTypeName   *types.TypeName
	InitTypeName        *types.TypeName
	ObservationTypeName *types.TypeName
}

// NewTypeNames returns a new TypeNames object.
func NewTypeNames(fieldPaths []string, pkg *types.Package) (*TypeNames, error) {
	paramTypeName, err := generateTypeName("Parameters", pkg, fieldPaths...)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot generate parameters type name of %s", fieldPath(fieldPaths))
	}
	paramName := types.NewTypeName(token.NoPos, pkg, paramTypeName, nil)

	initTypeName, err := generateTypeName("InitParameters", pkg, fieldPaths...)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot generate init parameters type name of %s", fieldPath(fieldPaths))
	}
	initName := types.NewTypeName(token.NoPos, pkg, initTypeName, nil)

	obsTypeName, err := generateTypeName("Observation", pkg, fieldPaths...)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot generate observation type name of %s", fieldPath(fieldPaths))
	}
	obsName := types.NewTypeName(token.NoPos, pkg, obsTypeName, nil)

	// We insert them to the package scope so that the type name calculations in
	// recursive calls are checked against their upper level type's name as well.
	pkg.Scope().Insert(paramName)
	pkg.Scope().Insert(initName)
	pkg.Scope().Insert(obsName)

	return &TypeNames{ParameterTypeName: paramName, InitTypeName: initName, ObservationTypeName: obsName}, nil
}

type resource struct {
	paramFields, initFields, obsFields []*types.Var
	paramTags, initTags, obsTags       []string
	celRules                           []*celRule
}

type celRule struct {
	rule             string
	initProviderRule string
	path             string
}

func newCelRule(rule, initProviderRule, path string) *celRule {
	return &celRule{
		rule:             rule,
		initProviderRule: initProviderRule,
		path:             path,
	}
}

func (r *resource) addParameterField(f *Field, field *types.Var) {
	req := !f.Schema.Optional

	// Note(lsviben): We are constructing the CEL rules for required fields that
	// are not identifier fields. If we are not creating or managing the
	// resource, we don't need to provide those parameters which are:
	// - req => required
	// - !f.Identifier => not identifiers - i.e. region, zone, etc.
	// If they are initProvider fields, we also add a check so that the field can
	// be set either in spec.forProvider or spec.initProvider.
	if req && !f.Identifier {
		req = false
		r.celRules = append(r.celRules, constructCELRules(f.CELPaths, f.isInit()))
	}

	// Note(lsviben): Only fields which are not also initProvider fields should have a required kubebuilder comment.
	f.Comment.Required = pointer.Bool(req && !f.isInit())

	// For removing omitempty tag from json tag, we are just checking if the field is required by the schema.
	if !f.Schema.Optional {
		// Required fields should not have omitempty tag in json tag.
		// TODO(muvaf): This overrides user intent if they provided custom
		// JSON tag.
		r.paramTags = append(r.paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, strings.TrimSuffix(f.JSONTag, ",omitempty"), f.TFTag))
	} else {
		r.paramTags = append(r.paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, f.JSONTag, f.TFTag))
	}

	r.paramFields = append(r.paramFields, field)
}

func (r *resource) addInitField(f *Field, field *types.Var, pkg *types.Package) {
	// If the field is not an init field, we don't add it.
	if !f.isInit() {
		return
	}

	r.initTags = append(r.initTags, fmt.Sprintf(`json:"%s" tf:"%s"`, f.JSONTag, f.TFTag))

	// If the field is a nested type, we need to add it as the init type.
	if f.InitType != nil {
		field = types.NewField(token.NoPos, pkg, f.Name.Camel, f.InitType, false)
	}

	r.initFields = append(r.initFields, field)
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

func sanitizePath(p string) string {
	for _, reserved := range celReservedKeywords {
		if p == reserved {
			return fmt.Sprintf(celEscapeSequence, p)
		}
	}
	return p
}

func constructCELPath(celPath []string) string {
	currentPath := make([]string, 0, len(celPath))
	// Go through the list of fields, and if a wildcard is encountered,
	// it means that the previous field is a list, so we add "[0]" to the path.
	// This is not ideal, but as there is no "list[*]" in CEL, we are at
	// least checking the first element of the list.
	// In most cases, due to the way we generate the types, the lists are
	// artificial and have only one element, so this should not be a problem.
	// In other cases, it just means that we miss validation for the other
	// elements of the list.
	for i, p := range celPath {
		if p == wildcard {
			// if a wildcard is at the end, we don't need to add "[0]" to the path
			if i == len(celPath)-1 {
				continue
			}
			currentPath[len(currentPath)-1] = fmt.Sprintf("%s[0]", currentPath[len(currentPath)-1])
			continue
		}
		currentPath = append(currentPath, sanitizePath(p))
	}
	return strings.Join(currentPath, ".")
}

func constructCELRules(celPath []string, isInit bool) *celRule {
	if len(celPath) == 0 {
		return &celRule{}
	}
	var rule []string
	currentPath := make([]string, 0, len(celPath))

	// construct forProvider rule.
	// If the field is a wildcard, it means that the previous field is a list,
	// so we need to make a check if the list is not empty. This is needed
	// as we should not require a required field of a listed resource if the
	// list is empty and not required. If the list is required, then it would
	// be covered by another rule.
	for i, p := range celPath {
		currentPath = append(currentPath, p)
		if p == wildcard {
			// if a wildcard is at the end, we don't need to create the rule
			if i != len(celPath)-1 {
				rule = append(rule, fmt.Sprintf(`!has(self.forProvider.%s)`, constructCELPath(currentPath)))
			}
			continue
		}
	}

	path := constructCELPath(celPath)
	rule = append(rule, fmt.Sprintf(`has(self.forProvider.%s)`, path))
	forProviderRule := strings.Join(rule, " || ")

	// construct initProvider rule if the field is an init field
	var initRule string
	if isInit {
		initRule = fmt.Sprintf(`has(self.initProvider.%s)`, path)
	}

	return newCelRule(forProviderRule, initRule, path)
}
