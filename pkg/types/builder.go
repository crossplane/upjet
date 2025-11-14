// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	twtypes "github.com/muvaf/typewriter/pkg/types"
	"github.com/pkg/errors"
	"k8s.io/utils/ptr"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/schema/traverser"
	conversiontfjson "github.com/crossplane/upjet/v2/pkg/types/conversion/tfjson"
)

const (
	wildcard = "*"

	emptyStruct = "struct{}"

	// ref: https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#validation-rules
	celEscapeSequence = "__%s__"
	// description for an injected list map key field in the context of the
	// server-side apply object list merging
	descriptionInjectedKey = "This is an injected field with a default value for being able to merge items of the parent object list."

	CRDScopeNamespaced CRDScope = "Namespaced"
	CRDScopeCluster    CRDScope = "Cluster"
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

type CRDScope string

// Builder is used to generate Go type equivalence of given Terraform schema.
type Builder struct {
	Package *types.Package

	genTypes        []*types.Named
	comments        twtypes.Comments
	validationRules string

	scope CRDScope
}

// NewBuilder returns a new Builder.
func NewBuilder(pkg *types.Package, scope CRDScope) *Builder {
	return &Builder{
		Package:  pkg,
		comments: twtypes.Comments{},
		scope:    scope,
	}
}

// Build returns parameters and observation types built out of Terraform schema.
func (g *Builder) Build(cfg *config.Resource) (Generated, error) {
	if err := injectServerSideApplyListMergeKeys(cfg); err != nil {
		return Generated{}, errors.Wrapf(err, "cannot inject server-side apply merge keys for resource %q", cfg.Name)
	}

	fp, ap, ip, err := g.buildResource(cfg.TerraformResource, cfg, nil, nil, false, cfg.Kind)
	return Generated{
		Types:            g.genTypes,
		Comments:         g.comments,
		ForProviderType:  fp,
		InitProviderType: ip,
		AtProviderType:   ap,
		ValidationRules:  g.validationRules,
	}, errors.Wrapf(err, "cannot build the Types for resource %q", cfg.Name)
}

func injectServerSideApplyListMergeKeys(cfg *config.Resource) error { //nolint:gocyclo // Easier to follow the logic in a single function
	for f, s := range cfg.ServerSideApplyMergeStrategies {
		if s.ListMergeStrategy.MergeStrategy != config.ListTypeMap {
			continue
		}
		if s.ListMergeStrategy.ListMapKeys.InjectedKey.Key == "" && len(s.ListMergeStrategy.ListMapKeys.Keys) == 0 {
			return errors.Errorf("list map keys configuration for the object list %q is empty", f)
		}
		if s.ListMergeStrategy.ListMapKeys.InjectedKey.Key == "" {
			continue
		}
		sch := config.GetSchema(cfg.TerraformResource, f)
		if sch == nil {
			return errors.Errorf("cannot find the Terraform schema for the argument at the path %q", f)
		}
		if sch.Type != schema.TypeList && sch.Type != schema.TypeSet {
			return errors.Errorf("fieldpath %q is not a Terraform list or set", f)
		}
		el, ok := sch.Elem.(*schema.Resource)
		if !ok {
			return errors.Errorf("fieldpath %q is a Terraform list or set but its element type is not a Terraform *schema.Resource", f)
		}
		for k := range el.Schema {
			if k == s.ListMergeStrategy.ListMapKeys.InjectedKey.Key {
				return errors.Errorf("element schema for the object list %q already contains the argument key %q", f, k)
			}
		}
		el.Schema[s.ListMergeStrategy.ListMapKeys.InjectedKey.Key] = &schema.Schema{
			Type:        schema.TypeString,
			Required:    true,
			Description: descriptionInjectedKey,
		}
		if s.ListMergeStrategy.ListMapKeys.InjectedKey.DefaultValue != "" {
			el.Schema[s.ListMergeStrategy.ListMapKeys.InjectedKey.Key].Default = s.ListMergeStrategy.ListMapKeys.InjectedKey.DefaultValue
		}
	}
	return nil
}

func (g *Builder) buildResource(res *schema.Resource, cfg *config.Resource, tfPath []string, xpPath []string, asBlocksMode bool, names ...string) (*types.Named, *types.Named, *types.Named, error) { //nolint:gocyclo
	// NOTE(muvaf): There can be fields in the same CRD with same name but in
	// different types. Since we generate the type using the field name, there
	// can be collisions. In order to be able to generate unique names consistently,
	// we need to process all fields in the same order all the time.
	keys := sortedKeys(res.Schema)

	typeNames, err := NewTypeNames(names, g.Package, cfg.OverrideFieldNames) //nolint:staticcheck // still handling deprecated field behavior
	if err != nil {
		return nil, nil, nil, err
	}

	r := &resource{}
	for _, snakeFieldName := range keys {
		var reference *config.Reference
		cPath := traverser.FieldPath(append(tfPath, snakeFieldName))
		ref, ok := cfg.References[cPath]
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
				return nil, nil, nil, err
			}
			if drop {
				continue
			}
		case reference != nil:
			f, err = NewReferenceField(g, cfg, r, res.Schema[snakeFieldName], reference, snakeFieldName, tfPath, xpPath, names, asBlocksMode)
			if err != nil {
				return nil, nil, nil, err
			}
		default:
			f, err = NewField(g, cfg, r, res.Schema[snakeFieldName], snakeFieldName, tfPath, xpPath, names, asBlocksMode)
			if err != nil {
				return nil, nil, nil, err
			}
		}
		f.AddToResource(g, r, typeNames, ptr.Deref(cfg.SchemaElementOptions[cPath], config.SchemaElementOption{}))
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

	for _, p := range r.topLevelRequiredParams {
		g.validationRules += "\n"
		sp := sanitizePath(p.path)
		if p.includeInit {
			g.validationRules += fmt.Sprintf(`// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.%s) || (has(self.initProvider) && has(self.initProvider.%s))",message="spec.forProvider.%s is a required parameter"`, sp, sp, p.path)
		} else {
			g.validationRules += fmt.Sprintf(`// +kubebuilder:validation:XValidation:rule="!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies) || has(self.forProvider.%s)",message="spec.forProvider.%s is a required parameter"`, sp, p.path)
		}
	}

	return paramType, obsType, initType
}

func (g *Builder) buildSchema(f *Field, cfg *config.Resource, names []string, cpath string, r *resource) (types.Type, types.Type, error) { //nolint:gocyclo
	switch f.Schema.Type {
	case schema.TypeBool:
		return types.NewPointer(types.Universe.Lookup("bool").Type()), nil, nil
	case schema.TypeFloat:
		return types.NewPointer(types.Universe.Lookup("float64").Type()), nil, nil
	case schema.TypeInt:
		return types.NewPointer(types.Universe.Lookup("int64").Type()), nil, nil
	case schema.TypeString:
		return types.NewPointer(types.Universe.Lookup("string").Type()), nil, nil
	case schema.TypeMap, schema.TypeList, schema.TypeSet, conversiontfjson.SchemaTypeObject:
		names = append(names, f.Name.Camel)
		_, hasNonPrimitiveElement := f.Schema.Elem.(*schema.Resource)
		isNonPrimitiveMap := (f.Schema.Type == schema.TypeMap) && hasNonPrimitiveElement
		if (f.Schema.Type != schema.TypeMap && f.Schema.Type != conversiontfjson.SchemaTypeObject) || isNonPrimitiveMap {
			// We don't want to have a many-to-many relationship in case of a Map of primitives , since we use SecretReference as
			// the type of XP field. In this case, we want to have a one-to-many relationship which is handled at
			// runtime in the controller.
			f.TerraformPaths = append(f.TerraformPaths, wildcard)
			if !cfg.SchemaElementOptions.EmbeddedObject(cpath) {
				f.CRDPaths = append(f.CRDPaths, wildcard)
			}
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
				return nil, nil, errors.Errorf("element type of %s is basic but not one of known basic types", traverser.FieldPath(names))
			default:
				return nil, nil, errors.Errorf("element type of %s is basic but not one of known types: %v", traverser.FieldPath(names), et)
			}
			initElemType = elemType
		case *schema.Schema:
			newf, err := NewField(g, cfg, r, et, f.Name.Snake, f.TerraformPaths, f.CRDPaths, names, false)
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
			paramType, obsType, initType, err := g.buildResource(et, cfg, f.TerraformPaths, f.CRDPaths, asBlocksMode, names...)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "cannot infer type from resource schema of element type of %s", traverser.FieldPath(names))
			}
			initElemType = initType

			switch {
			case IsObservation(f.Schema):
				if obsType == nil {
					return nil, nil, errors.Errorf("element type of %s is computed but the underlying schema does not return observation type", traverser.FieldPath(names))
				}
				elemType = obsType
			default:
				if paramType == nil {
					return nil, nil, errors.Errorf("element type of %s is configurable but the underlying schema does not return a parameter type", traverser.FieldPath(names))
				}
				elemType = paramType
				// There are some types that are parameter field but also has nested fields that can go under status.
				// This check prevents the elimination of fields in observation type, by checking whether the schema in
				// parameter type has nested observation (status) fields.
				if obsType.Underlying().String() != emptyStruct {
					var t types.Type
					if cfg.SchemaElementOptions.EmbeddedObject(cpath) || f.Schema.Type == conversiontfjson.SchemaTypeObject { //nolint:gocritic
						t = types.NewPointer(obsType)
					} else if f.Schema.Type == schema.TypeMap {
						t = types.NewMap(types.Universe.Lookup("string").Type(), obsType)
					} else {
						t = types.NewSlice(obsType)
					}
					field := types.NewField(token.NoPos, g.Package, f.Name.Camel, t, false)
					r.addObservationField(f, field)
				}
			}
		// if unset
		// see: https://github.com/crossplane/upjet/issues/177
		case nil:
			elemType = types.Universe.Lookup("string").Type()
			initElemType = elemType
		default:
			return nil, nil, errors.Errorf("element type of %s should be either schema.Resource or schema.Schema", traverser.FieldPath(names))
		}

		// if the singleton list is to be replaced by an embedded object
		if cfg.SchemaElementOptions.EmbeddedObject(cpath) || f.Schema.Type == conversiontfjson.SchemaTypeObject {
			return types.NewPointer(elemType), types.NewPointer(initElemType), nil
		}
		// NOTE(muvaf): Maps and slices are already pointers, so we don't need to
		// wrap them even if they are optional.
		if f.Schema.Type == schema.TypeMap {
			return types.NewMap(types.Universe.Lookup("string").Type(), elemType), types.NewMap(types.Universe.Lookup("string").Type(), initElemType), nil
		}
		return types.NewSlice(elemType), types.NewSlice(initElemType), nil
	case conversiontfjson.SchemaTypeDynamic:
		return types.NewPointer(typeK8sAPIExtensionsJson), types.NewPointer(typeK8sAPIExtensionsJson), nil
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
func NewTypeNames(fieldPaths []string, pkg *types.Package, overrideFieldNames map[string]string) (*TypeNames, error) {
	paramTypeName, err := generateTypeName("Parameters", pkg, overrideFieldNames, fieldPaths...)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot generate parameters type name of %s", traverser.FieldPath(fieldPaths))
	}
	paramName := types.NewTypeName(token.NoPos, pkg, paramTypeName, nil)

	initTypeName, err := generateTypeName("InitParameters", pkg, overrideFieldNames, fieldPaths...)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot generate init parameters type name of %s", traverser.FieldPath(fieldPaths))
	}
	initName := types.NewTypeName(token.NoPos, pkg, initTypeName, nil)

	obsTypeName, err := generateTypeName("Observation", pkg, overrideFieldNames, fieldPaths...)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot generate observation type name of %s", traverser.FieldPath(fieldPaths))
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
	topLevelRequiredParams             []*topLevelRequiredParam
}

type topLevelRequiredParam struct {
	path        string
	includeInit bool
}

func newTopLevelRequiredParam(path string, includeInit bool) *topLevelRequiredParam {
	return &topLevelRequiredParam{path: path, includeInit: includeInit}
}

func (r *resource) addParameterField(f *Field, field *types.Var) {
	requiredBySchema := (!f.Schema.Optional && !f.Schema.Computed) || f.Required
	// Note(turkenh): We are collecting the top level required parameters that
	// are not identifier fields. This is for generating CEL validation rules for
	// those parameters and not to require them if the management policy is set
	// Observe Only. In other words, if we are not creating or managing the
	// resource, we don't need to provide those parameters which are:
	// - requiredBySchema => required
	// - !f.Identifier => not identifiers - i.e. region, zone, etc.
	// - len(f.CanonicalPaths) == 1 => top level, i.e. not a nested field
	// TODO (lsviben): We should add CEL rules for all required fields,
	// not just the top level ones, due to having all forProvider
	// fields now optional. CEL rules should check if a field is
	// present either in forProvider or initProvider.
	// https://github.com/crossplane/upjet/issues/239
	if requiredBySchema && !f.Identifier && len(f.CanonicalPaths) == 1 {
		requiredBySchema = false
		// If the field is not a terraform field, we should not require it in init,
		// as it is not an initProvider field.
		r.topLevelRequiredParams = append(r.topLevelRequiredParams, newTopLevelRequiredParam(f.TransformedName, !f.TFTag.AlwaysOmitted()))
	}

	// Note(lsviben): Only fields which are not also initProvider fields should have a required kubebuilder comment.
	f.Comment.KubebuilderOptions.Required = ptr.To(requiredBySchema && !f.isInit())

	// For removing omitempty tag from json tag, we are just checking if the field is required by the schema.
	if requiredBySchema {
		// Required fields should not have omitempty tag in json tag.
		r.paramTags = append(r.paramTags, fmt.Sprintf("%s %s", f.JSONTag.NoOmit(), f.TFTag))
	} else {
		r.paramTags = append(r.paramTags, fmt.Sprintf("%s %s", f.JSONTag, f.TFTag))
	}

	r.paramFields = append(r.paramFields, field)
}

func (r *resource) addInitField(f *Field, field *types.Var, g *Builder, typeNames *types.TypeName, o config.TagOverrides) {
	// If the field is not an init field, we don't add it.
	if !f.isInit() {
		return
	}

	r.initTags = append(r.initTags, fmt.Sprintf("%s %s", f.JSONTag.OverrideFrom(o.JSONTag), f.TFTag.OverrideFrom(o.TFTag)))

	// If the field is a nested type, we need to add it as the init type.
	if f.InitType != nil {
		field = types.NewField(token.NoPos, g.Package, f.Name.Camel, f.InitType, false)
	}

	r.initFields = append(r.initFields, field)

	if f.Reference != nil {
		r.addReferenceFields(g, typeNames, f, true)
	}
}

func (r *resource) addObservationField(f *Field, field *types.Var) {
	for _, obsF := range r.obsFields {
		if obsF.Name() == field.Name() {
			// If the field is already added, we don't add it again.
			// Some nested types could have been previously added as an
			// observation type while building their schema: https://github.com/crossplane/upjet/blob/b89baca4ae24c8fbd8eb403c353ca18916093e5e/pkg/types/builder.go#L206
			return
		}
	}
	r.obsFields = append(r.obsFields, field)
	r.obsTags = append(r.obsTags, fmt.Sprintf("%s %s", f.JSONTag, f.TFTag))
}

func (r *resource) addReferenceFields(g *Builder, paramName *types.TypeName, field *Field, isInit bool) {
	refFields, refTags := g.generateReferenceFields(paramName, field)
	if isInit {
		r.initTags = append(r.initTags, refTags...)
		r.initFields = append(r.initFields, refFields...)
	} else {
		r.paramTags = append(r.paramTags, refTags...)
		r.paramFields = append(r.paramFields, refFields...)
	}
}

// generateTypeName generates a unique name for the type if its original name
// is used by another one. It adds the former field names recursively until it
// finds a unique name.
func generateTypeName(suffix string, pkg *types.Package, overrideFieldNames map[string]string, names ...string) (calculated string, _ error) {
	defer func() {
		if v, ok := overrideFieldNames[calculated]; ok {
			calculated = v
		}
	}()
	n := names[len(names)-1] + suffix
	for i := len(names) - 2; i >= 0; i-- {
		if pkg.Scope().Lookup(n) == nil {
			calculated = n
			return
		}
		n = names[i] + n
	}
	if pkg.Scope().Lookup(n) == nil {
		calculated = n
		return
	}
	// start from 2 considering the 1st of this type is the one without an
	// index.
	for i := 2; i < 10; i++ {
		nn := fmt.Sprintf("%s_%d", n, i)
		if pkg.Scope().Lookup(nn) == nil {
			calculated = nn
			return
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

func sanitizePath(p string) string {
	for _, reserved := range celReservedKeywords {
		if p == reserved {
			return fmt.Sprintf(celEscapeSequence, p)
		}
	}
	return p
}
