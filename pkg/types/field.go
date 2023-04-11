package types

import (
	"fmt"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg"
	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/types/comments"
	"github.com/upbound/upjet/pkg/types/name"
)

var parentheses = regexp.MustCompile(`\(([^)]+)\)`)

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
	TransformedName                          string
	SelectorName                             string
	Identifier                               bool
}

// getDocString tries to extract the documentation string for the specified
// field by:
// - first, looking up the field's hierarchical name in
// the dictionary of extracted doc strings
// - second, looking up the terminal name in the same dictionary
// - and third, tries to match hierarchical name with
// the longest suffix matching
func getDocString(cfg *config.Resource, f *Field, tfPath []string) string { //nolint:gocyclo
	hName := f.Name.Snake
	if len(tfPath) > 0 {
		hName = fieldPath(append(tfPath, hName))
	}
	docString := ""
	if cfg.MetaResource != nil {
		// 1st, look up the hierarchical name
		if s, ok := cfg.MetaResource.ArgumentDocs[hName]; ok {
			return getDescription(s)
		}
		lm := 0
		match := ""
		sortedKeys := make([]string, 0, len(cfg.MetaResource.ArgumentDocs))
		for k := range cfg.MetaResource.ArgumentDocs {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Strings(sortedKeys)
		// look up the terminal name
		for _, k := range sortedKeys {
			parts := strings.Split(k, ".")
			if parts[len(parts)-1] == f.Name.Snake {
				lm = len(f.Name.Snake)
				match = k
			}
		}
		if lm == 0 {
			// do longest suffix matching
			for _, k := range sortedKeys {
				if strings.HasSuffix(hName, k) {
					if len(k) > lm {
						lm = len(k)
						match = k
					}
				}
			}
		}
		if lm > 0 {
			docString = getDescription(cfg.MetaResource.ArgumentDocs[match])
		}
	}
	return docString
}

// NewField returns a constructed Field object.
func NewField(g *Builder, cfg *config.Resource, r *resource, sch *schema.Schema, snakeFieldName string, tfPath, xpPath, names []string, asBlocksMode bool) (*Field, error) {
	f := &Field{
		Schema:         sch,
		Name:           name.NewFromSnake(snakeFieldName),
		FieldNameCamel: name.NewFromSnake(snakeFieldName).Camel,
		AsBlocksMode:   asBlocksMode,
	}

	for _, ident := range cfg.ExternalName.IdentifierFields {
		// TODO(turkenh): Could there be a nested identifier field? No, known
		// cases so far but we would need to handle that if/once there is one,
		// which is missing here.
		if ident == snakeFieldName {
			f.Identifier = true
			break
		}
	}

	var commentText string
	docString := getDocString(cfg, f, tfPath)
	if len(docString) > 0 {
		commentText = docString + "\n"
	}
	commentText += f.Schema.Description
	commentText = pkg.FilterDescription(commentText, pkg.TerraformKeyword)
	comment, err := comments.New(commentText)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot build comment for description: %s", commentText)
	}
	f.Comment = comment
	f.TFTag = fmt.Sprintf("%s,omitempty", f.Name.Snake)
	f.JSONTag = fmt.Sprintf("%s,omitempty", f.Name.LowerCamelComputed)
	f.TransformedName = f.Name.LowerCamelComputed

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

	if IsObservation(f.Schema) {
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
		f.FieldType = typeSecretReference
	}
	f.TransformedName = name.NewFromCamel(f.FieldNameCamel).LowerCamelComputed
	f.JSONTag = f.TransformedName
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
	if f.Comment.UpjetOptions.FieldTFTag != nil {
		f.TFTag = *f.Comment.UpjetOptions.FieldTFTag
	}
	if f.Comment.UpjetOptions.FieldJSONTag != nil {
		f.JSONTag = *f.Comment.UpjetOptions.FieldJSONTag
	}

	field := types.NewField(token.NoPos, g.Package, f.FieldNameCamel, f.FieldType, false)

	// Note(turkenh): We want atProvider to be a superset of forProvider, so
	// we always add the field as an observation field and then add it as a
	// parameter field if it's not an observation (only) field, i.e. parameter.
	//
	// We do this only if tf tag is not set to "-" because otherwise it won't
	// be populated from the tfstate. We typically set tf tag to "-" for
	// sensitive fields which were replaced with secretKeyRefs.
	if f.TFTag != "-" {
		r.addObservationField(f, field)
	}
	if !IsObservation(f.Schema) {
		if f.AsBlocksMode {
			f.TFTag = strings.TrimSuffix(f.TFTag, ",omitempty")
		}
		r.addParameterField(f, field)
	}

	if f.Reference != nil {
		r.addReferenceFields(g, typeNames.ParameterTypeName, f)
	}

	g.comments.AddFieldComment(typeNames.ParameterTypeName, f.FieldNameCamel, f.Comment.Build())
	// Note(turkenh): We don't want reference resolver to be generated for
	// fields under status.atProvider. So, we don't want reference comments to
	// be added, hence we are unsetting reference on the field comment just
	// before adding it as an observation field.
	f.Comment.Reference = config.Reference{}
	// Note(turkenh): We don't need required/optional markers for observation
	// fields.
	f.Comment.Required = nil
	g.comments.AddFieldComment(typeNames.ObservationTypeName, f.FieldNameCamel, f.Comment.Build())
}

func getDescription(s string) string {
	// Remove dash
	s = strings.TrimSpace(s)[strings.Index(s, "-")+1:]

	// Remove 'Reqiured' || 'Optional' information
	matches := parentheses.FindAllString(s, -1)
	for _, m := range matches {
		if strings.HasPrefix(strings.ToLower(m), "(optional") || strings.HasPrefix(strings.ToLower(m), "(required") {
			s = strings.ReplaceAll(s, m, "")
		}
	}
	return strings.TrimSpace(s)
}
