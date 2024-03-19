// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

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
	"k8s.io/utils/ptr"

	"github.com/crossplane/upjet/pkg"
	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/types/comments"
	"github.com/crossplane/upjet/pkg/types/name"
)

const (
	errFmtInvalidSSAConfiguration = "invalid server-side apply merge strategy configuration: Field schema for %q is of type %q and the specified configuration must only set %q"
	errFmtUnsupportedSSAField     = "cannot configure the server-side apply merge strategy for %q: Configuration can only be specified for lists, sets or maps"
	errFmtMissingListMapKeys      = "server-side apply merge strategy configuration for %q belongs to a list of type map but list map keys configuration is missing"
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
	InitType                                 types.Type
	AsBlocksMode                             bool
	Reference                                *config.Reference
	TransformedName                          string
	SelectorName                             string
	Identifier                               bool
	Required                                 bool
	// Injected is set if this Field is an injected field to the Terraform
	// schema as an object list map key for server-side apply merges.
	Injected bool
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

	for _, required := range cfg.RequiredFields() {
		if required == snakeFieldName {
			f.Required = true
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
	f.TerraformPaths = append(tfPath, f.Name.Snake) //nolint:gocritic
	// Crossplane paths, e.g. {"lifecycleRule", "*", "transition", "*", "days"}
	f.CRDPaths = append(xpPath, f.Name.LowerCamelComputed) //nolint:gocritic
	// Canonical paths, e.g. {"LifecycleRule", "Transition", "Days"}
	f.CanonicalPaths = append(names[1:], f.Name.Camel) //nolint:gocritic

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

	fieldType, initType, err := g.buildSchema(f, cfg, names, fieldPath(append(tfPath, snakeFieldName)), r)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot infer type from schema of field %s", f.Name.Snake)
	}
	f.FieldType = fieldType
	f.InitType = initType

	AddServerSideApplyMarkers(f)
	return f, errors.Wrapf(AddServerSideApplyMarkersFromConfig(f, cfg), "cannot add the server-side apply merge strategy markers for the field")
}

// AddServerSideApplyMarkers adds server-side apply comment markers to indicate
// that scalar maps and sets can be merged granularly, not replace atomically.
func AddServerSideApplyMarkers(f *Field) {
	// for sensitive fields, we generate secret or secret key references
	if f.Schema.Sensitive {
		return
	}

	switch f.Schema.Type { //nolint:exhaustive
	case schema.TypeMap:
		// A map should always have an element of type Schema.
		if es, ok := f.Schema.Elem.(*schema.Schema); ok {
			switch es.Type { //nolint:exhaustive
			// We assume scalar types can be granular maps.
			case schema.TypeString, schema.TypeBool, schema.TypeInt, schema.TypeFloat:
				f.Comment.ServerSideApplyOptions.MapType = ptr.To[config.MapType](config.MapTypeGranular)
			}
		}
	case schema.TypeSet:
		if es, ok := f.Schema.Elem.(*schema.Schema); ok {
			switch es.Type { //nolint:exhaustive
			// We assume scalar types can be granular sets.
			case schema.TypeString, schema.TypeBool, schema.TypeInt, schema.TypeFloat:
				f.Comment.ServerSideApplyOptions.ListType = ptr.To[config.ListType](config.ListTypeSet)
			}
		}
	}
	// TODO(negz): Can we reliably add SSA markers for lists of objects? Do we
	// have cases where we're turning a Terraform map of maps into a list of
	// objects with a well-known key that we could merge on?
}

func setInjectedField(fp, k string, f *Field, s config.MergeStrategy) bool {
	if fp != fmt.Sprintf("%s.%s", k, s.ListMergeStrategy.ListMapKeys.InjectedKey.Key) {
		return false
	}

	if s.ListMergeStrategy.ListMapKeys.InjectedKey.DefaultValue != "" {
		f.Comment.KubebuilderOptions.Default = ptr.To[string](s.ListMergeStrategy.ListMapKeys.InjectedKey.DefaultValue)
	}
	f.TFTag = "-" // prevent serialization into Terraform configuration
	f.Injected = true
	return true
}

func AddServerSideApplyMarkersFromConfig(f *Field, cfg *config.Resource) error { //nolint:gocyclo // Easier to follow the logic in a single function
	// for sensitive fields, we generate secret or secret key references
	if f.Schema.Sensitive {
		return nil
	}
	fp := strings.ReplaceAll(strings.Join(f.TerraformPaths, "."), ".*.", ".")
	fp = strings.TrimSuffix(fp, ".*")
	for k, s := range cfg.ServerSideApplyMergeStrategies {
		if setInjectedField(fp, k, f, s) || k != fp {
			continue
		}
		switch f.Schema.Type { //nolint:exhaustive
		case schema.TypeList, schema.TypeSet:
			if s.ListMergeStrategy.MergeStrategy == "" || s.MapMergeStrategy != "" || s.StructMergeStrategy != "" {
				return errors.Errorf(errFmtInvalidSSAConfiguration, k, "list", "ListMergeStrategy")
			}
			f.Comment.ServerSideApplyOptions.ListType = ptr.To[config.ListType](s.ListMergeStrategy.MergeStrategy)
			if s.ListMergeStrategy.MergeStrategy != config.ListTypeMap {
				continue
			}
			f.Comment.ServerSideApplyOptions.ListMapKey = make([]string, 0, len(s.ListMergeStrategy.ListMapKeys.Keys)+1)
			f.Comment.ServerSideApplyOptions.ListMapKey = append(f.Comment.ServerSideApplyOptions.ListMapKey, s.ListMergeStrategy.ListMapKeys.Keys...)
			if s.ListMergeStrategy.ListMapKeys.InjectedKey.Key != "" {
				f.Comment.ServerSideApplyOptions.ListMapKey = append(f.Comment.ServerSideApplyOptions.ListMapKey, s.ListMergeStrategy.ListMapKeys.InjectedKey.Key)
			}
			if len(f.Comment.ServerSideApplyOptions.ListMapKey) == 0 {
				return errors.Errorf(errFmtMissingListMapKeys, k)
			}
		case schema.TypeMap:
			if s.MapMergeStrategy == "" || s.ListMergeStrategy.MergeStrategy != "" || s.StructMergeStrategy != "" {
				return errors.Errorf(errFmtInvalidSSAConfiguration, k, "map", "MapMergeStrategy")
			}
			f.Comment.ServerSideApplyOptions.MapType = ptr.To[config.MapType](s.MapMergeStrategy) // better to have a copy of the strategy
		default:
			// currently the generated APIs do not contain embedded objects, embedded
			// objects are represented as lists of max size 1. However, this may
			// change in the future, i.e., we may decide to generate HCL lists of max
			// size 1 as embedded objects.
			return errors.Errorf(errFmtUnsupportedSSAField, k)
		}
	}
	return nil
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
	switch f.FieldType.(type) {
	case *types.Slice:
		f.CRDPaths[len(f.CRDPaths)-2] = f.CRDPaths[len(f.CRDPaths)-2] + sfx
		cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(f.TerraformPaths), "spec.forProvider."+fieldPathWithWildcard(f.CRDPaths))
	default:
		cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(f.TerraformPaths), "spec.forProvider."+fieldPathWithWildcard(f.CRDPaths)+sfx)
	}
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
func (f *Field) AddToResource(g *Builder, r *resource, typeNames *TypeNames, addToObservation bool) { //nolint:gocyclo
	if f.Comment.UpjetOptions.FieldJSONTag != nil {
		f.JSONTag = *f.Comment.UpjetOptions.FieldJSONTag
	}

	field := types.NewField(token.NoPos, g.Package, f.FieldNameCamel, f.FieldType, false)
	// if the field is explicitly configured to be added to
	// the Observation type
	if addToObservation {
		r.addObservationField(f, field)
	}

	if f.Comment.UpjetOptions.FieldTFTag != nil {
		f.TFTag = *f.Comment.UpjetOptions.FieldTFTag
	}

	// Note(turkenh): We want atProvider to be a superset of forProvider, so
	// we always add the field as an observation field and then add it as a
	// parameter field if it's not an observation (only) field, i.e. parameter.
	//
	// We do this only if tf tag is not set to "-" because otherwise it won't
	// be populated from the tfstate. Injected fields are included in the
	// observation because an associative-list in the spec should also be
	// an associative-list in the observation (status).
	// We also make sure that this field has not already been added to the
	// observation type via an explicit resource configuration.
	// We typically set tf tag to "-" for sensitive fields which were replaced
	// with secretKeyRefs, or for injected fields into the CRD schema,
	// which do not exist in the Terraform schema.
	if (f.TFTag != "-" || f.Injected) && !addToObservation {
		r.addObservationField(f, field)
	}

	if !IsObservation(f.Schema) {
		if f.AsBlocksMode {
			f.TFTag = strings.TrimSuffix(f.TFTag, ",omitempty")
		}
		r.addParameterField(f, field)
		r.addInitField(f, field, g, typeNames.InitTypeName)
	}

	if f.Reference != nil {
		r.addReferenceFields(g, typeNames.ParameterTypeName, f, false)
	}

	// Note(lsviben): All fields are optional because observation fields are
	// optional by default, and forProvider and initProvider fields should
	// be checked through CEL rules.
	// This doesn't count for identifiers and references, which are not
	// mirrored in initProvider.
	if f.isInit() {
		f.Comment.Required = ptr.To(false)
	}
	g.comments.AddFieldComment(typeNames.ParameterTypeName, f.FieldNameCamel, f.Comment.Build())

	// initProvider and observation fields are always optional.
	f.Comment.Required = nil
	g.comments.AddFieldComment(typeNames.InitTypeName, f.FieldNameCamel, f.Comment.Build())

	if addToObservation {
		g.comments.AddFieldComment(typeNames.ObservationTypeName, f.FieldNameCamel, f.Comment.CommentWithoutOptions().Build())
	} else {
		// Note(turkenh): We don't want reference resolver to be generated for
		// fields under status.atProvider. So, we don't want reference comments to
		// be added, hence we are unsetting reference on the field comment just
		// before adding it as an observation field.
		f.Comment.Reference = config.Reference{}
		g.comments.AddFieldComment(typeNames.ObservationTypeName, f.FieldNameCamel, f.Comment.Build())
	}
}

// isInit returns true if the field should be added to initProvider.
// We don't add Identifiers, references or fields which tag is set to
// "-" unless they are injected object list map keys for server-side apply
// merges.
//
// Identifiers as they should not be ignorable or part of init due
// the fact being created for one identifier and then updated for another
// means a different resource could be targeted.
//
// Because of how upjet works, the main.tf file is created and filled
// in the Connect step of the reconciliation. So we merge the initProvider
// and forProvider there and write it to the main.tf file. So fields that are
// not part of terraform are not included in this merge, plus they cant be
// ignored through ignore_changes. References similarly get resolved in
// an earlier step, so they cannot be included as well. Plus probably they
// should also not change for Create and Update steps.
func (f *Field) isInit() bool {
	return !f.Identifier && (f.TFTag != "-" || f.Injected)
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
