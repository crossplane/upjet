package types

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/types/comments"
	"github.com/upbound/upjet/pkg/types/name"
)

// Field represents a field that is built from the Terraform schema.
// It contains the go field related information such as tags, field type, comment.
type Field struct {
	Schema                                   *schema.Schema
	Name                                     name.Name
	Comment                                  *comments.Comment
	TFTag, JSONTag, FieldNameCamel           string
	TerraformPaths, CRDPaths, CanonicalPaths []string
	FieldType                                types.Type
	AsBlocksMode                             bool
	Reference                                *config.Reference
}

// NewField returns a constructed Field object.
func NewField(g *Builder, cfg *config.Resource, r *resource, sch *schema.Schema, snakeFieldName string, tfPath, xpPath, names []string, asBlocksMode bool) (*Field, error) {
	f := &Field{
		Schema:         sch,
		Name:           name.NewFromSnake(snakeFieldName),
		FieldNameCamel: name.NewFromSnake(snakeFieldName).Camel,
		AsBlocksMode:   asBlocksMode,
	}

	comment, err := comments.New(f.Schema.Description)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot build comment for description: %s", f.Schema.Description)
	}
	f.Comment = comment
	f.TFTag = fmt.Sprintf("%s,omitempty", f.Name.Snake)
	f.JSONTag = fmt.Sprintf("%s,omitempty", f.Name.LowerCamelComputed)

	// Terraform paths, e.g. { "lifecycle_rule", "*", "transition", "*", "days" } for https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket#lifecycle_rule
	f.TerraformPaths = append(tfPath, f.Name.Snake) // nolint:gocritic
	// Crossplane paths, e.g. {"lifecycleRule", "*", "transition", "*", "days"}
	f.CRDPaths = append(xpPath, f.Name.LowerCamelComputed) // nolint:gocritic
	// Canonical paths, e.g. {"LifecycleRule", "Transition", "Days"}
	f.CanonicalPaths = append(names[1:], f.Name.Camel) // nolint:gocritic

	for _, ignoreField := range cfg.LateInitializer.IgnoredFields {
		// Convert configuration input from Terraform path to canonical path
		// Todo(turkenh/muvaf): Replace with a simple string conversion
		//  like GetIgnoredCanonicalFields where we just make each word
		//  between points camel case using names.go utilities. If the path
		//  doesn't match anything, it's no-op in late-init logic anyway.
		if ignoreField == fieldPath(f.TerraformPaths) {
			cfg.LateInitializer.AddIgnoredCanonicalFields(fieldPath(f.CanonicalPaths))
		}
	}

	fieldType, err := g.buildSchema(f, cfg, names, r)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot infer type from schema of field %s", f.Name.Snake)
	}
	f.FieldType = fieldType

	return f, nil
}

// NewSensitiveField returns a constructed sensitive Field object.
func NewSensitiveField(g *Builder, cfg *config.Resource, r *resource, sch *schema.Schema, snakeFieldName string, tfPath, xpPath, names []string, asBlocksMode bool) (*Field, bool, error) { //nolint:gocyclo
	f, err := NewField(g, cfg, r, sch, snakeFieldName, tfPath, xpPath, names, asBlocksMode)
	if err != nil {
		return nil, false, err
	}

	if isObservation(f.Schema) {
		cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(f.TerraformPaths), "status.atProvider."+fieldPathWithWildcard(f.CRDPaths))
		// Drop an observation field from schema if it is sensitive.
		// Data will be stored in connection details secret
		return nil, true, nil
	}
	sfx := "SecretRef"
	cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(f.TerraformPaths), "spec.forProvider."+fieldPathWithWildcard(f.CRDPaths)+sfx)
	// todo(turkenh): do we need to support other field types as sensitive?
	if f.FieldType.String() != "string" && f.FieldType.String() != "*string" && f.FieldType.String() != "[]string" &&
		f.FieldType.String() != "[]*string" && f.FieldType.String() != "map[string]string" && f.FieldType.String() != "map[string]*string" {
		return nil, false, fmt.Errorf(`got type %q for field %q, only types "string", "*string", []string, []*string, "map[string]string" and "map[string]*string" supported as sensitive`, f.FieldType.String(), f.FieldNameCamel)
	}
	// Replace a parameter field with secretKeyRef if it is sensitive.
	// If it is an observation field, it will be dropped.
	// Data will be loaded from the referenced secret key.
	f.FieldNameCamel += sfx

	f.TFTag = "-"
	switch f.FieldType.String() {
	case "string", "*string":
		f.FieldType = typeSecretKeySelector
	case "[]string", "[]*string":
		f.FieldType = types.NewSlice(typeSecretKeySelector)
	case "map[string]string", "map[string]*string":
		f.FieldType = types.NewMap(types.Universe.Lookup("string").Type(), typeSecretKeySelector)
	}
	f.JSONTag = name.NewFromCamel(f.FieldNameCamel).LowerCamelComputed
	if f.Schema.Optional {
		f.FieldType = types.NewPointer(f.FieldType)
		f.JSONTag += ",omitempty"
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
	f.Schema.Optional = true

	return f, nil
}

// AddToResource adds built field to the resource.
func (f *Field) AddToResource(g *Builder, r *resource, typeNames *TypeNames) {
	if f.Comment.TerrajetOptions.FieldTFTag != nil {
		f.TFTag = *f.Comment.TerrajetOptions.FieldTFTag
	}
	if f.Comment.TerrajetOptions.FieldJSONTag != nil {
		f.JSONTag = *f.Comment.TerrajetOptions.FieldJSONTag
	}

	field := types.NewField(token.NoPos, g.Package, f.FieldNameCamel, f.FieldType, false)
	switch {
	case isObservation(f.Schema):
		r.addObservationField(f, field)
	default:
		if f.AsBlocksMode {
			f.TFTag = strings.TrimSuffix(f.TFTag, ",omitempty")
		}
		r.addParameterField(f, field)
	}

	if f.Reference != nil {
		r.addReferenceFields(g, typeNames.ParameterTypeName, field, *f.Reference)
	}

	g.comments.AddFieldComment(typeNames.ParameterTypeName, f.FieldNameCamel, f.Comment.Build())
}
