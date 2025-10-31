// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
)

const (
	errIDNotFoundInTFState = "id does not exist in tfstate"
)

var (
	externalNameRegex = regexp.MustCompile(`{{\ *\.external_name\b\ *}}`)
)

var (
	// NameAsIdentifier uses "name" field in the arguments as the identifier of
	// the resource.
	NameAsIdentifier = ExternalName{
		SetIdentifierArgumentFn: func(base map[string]any, name string) {
			base["name"] = name
		},
		GetExternalNameFn: IDAsExternalName,
		GetIDFn:           ExternalNameAsID,
		OmittedFields: []string{
			"name",
			"name_prefix",
		},
	}

	// IdentifierFromProvider is used in resources whose identifier is assigned by
	// the remote client, such as AWS VPC where it gets an identifier like
	// vpc-2213das instead of letting user choose a name.
	IdentifierFromProvider = ExternalName{
		SetIdentifierArgumentFn: NopSetIdentifierArgument,
		GetExternalNameFn:       IDAsExternalName,
		GetIDFn:                 ExternalNameAsID,
		DisableNameInitializer:  true,
	}

	parameterPattern = regexp.MustCompile(`{{\s*\.parameters\.([^\s}]+)\s*}}`)
)

// ParameterAsIdentifier uses the given field name in the arguments as the
// identifier of the resource.
func ParameterAsIdentifier(param string) ExternalName {
	e := NameAsIdentifier
	e.SetIdentifierArgumentFn = func(base map[string]any, name string) {
		base[param] = name
	}
	e.OmittedFields = []string{
		param,
		param + "_prefix",
	}
	e.IdentifierFields = []string{param}
	return e
}

// TemplatedStringAsIdentifier accepts a template as the shape of the Terraform
// ID and lets you provide a field path for the argument you're using as external
// name. The available variables you can use in the template are as follows:
//
// parameters: A tree of parameters that you'd normally see in a Terraform HCL
// file. You can use TF registry documentation of given resource to
// see what's available.
//
// setup.configuration: The Terraform configuration object of the provider. You can
// take a look at the TF registry provider configuration object
// to see what's available. Not to be confused with ProviderConfig
// custom resource of the Crossplane provider.
//
// setup.client_metadata: The Terraform client metadata available for the provider,
// such as the AWS account ID for the AWS provider.
//
// external_name: The value of external name annotation of the custom resource.
// It is required to use this as part of the template.
//
// The following template functions are available:
//
// ToLower: Converts the contents of the pipeline to lower-case
//
// ToUpper: Converts the contents of the pipeline to upper-case
//
// Please note that it's currently *not* possible to use
// the template functions on the .external_name template variable.
// Example usages:
//
// TemplatedStringAsIdentifier("index_name", "/subscriptions/{{ .setup.configuration.subscription }}/{{ .external_name }}")
//
// TemplatedStringAsIdentifier("index_name", "/resource/{{ .external_name }}/static")
//
// TemplatedStringAsIdentifier("index_name", "{{ .parameters.cluster_id }}:{{ .parameters.node_id }}:{{ .external_name }}")
//
// TemplatedStringAsIdentifier("", "arn:aws:network-firewall:{{ .setup.configuration.region }}:{{ .setup.client_metadata.account_id }}:{{ .parameters.type | ToLower }}-rulegroup/{{ .external_name }}")
func TemplatedStringAsIdentifier(nameFieldPath, tmpl string) ExternalName {
	t, err := template.New("getid").Funcs(template.FuncMap{
		"ToLower": strings.ToLower,
		"ToUpper": strings.ToUpper,
	}).Parse(tmpl)
	if err != nil {
		panic(errors.Wrap(err, "cannot parse template"))
	}

	// Note(turkenh): If a parameter is used in the external name template,
	// it is an identifier field.
	var identifierFields []string
	for _, node := range t.Root.Nodes {
		if node.Type() == parse.NodeAction {
			match := parameterPattern.FindStringSubmatch(node.String())
			if len(match) == 2 {
				identifierFields = append(identifierFields, match[1])
			}
		}
	}
	return ExternalName{
		SetIdentifierArgumentFn: func(base map[string]any, externalName string) {
			if nameFieldPath == "" {
				return
			}
			// TODO(muvaf): Return error in this function? Not returning error
			// is a valid option since the schemas are static so we'd get the
			// panic right when you create a resource. It's not generation-time
			// error though.
			if err := fieldpath.Pave(base).SetString(nameFieldPath, externalName); err != nil {
				panic(errors.Wrapf(err, "cannot set %s to fieldpath %s", externalName, nameFieldPath))
			}
		},
		OmittedFields: []string{
			nameFieldPath,
			nameFieldPath + "_prefix",
		},
		GetIDFn: func(ctx context.Context, externalName string, parameters map[string]any, setup map[string]any) (string, error) {
			o := map[string]any{
				"external_name": externalName,
				"parameters":    parameters,
				"setup":         setup,
			}
			b := bytes.Buffer{}
			if err := t.Execute(&b, o); err != nil {
				return "", errors.Wrap(err, "cannot execute template")
			}
			return b.String(), nil
		},
		GetExternalNameFn: func(tfstate map[string]any) (string, error) {
			id, ok := tfstate["id"]
			if !ok {
				return "", errors.New(errIDNotFoundInTFState)
			}
			return GetExternalNameFromTemplated(tmpl, id.(string))
		},
		IdentifierFields: identifierFields,
	}
}

// GetExternalNameFromTemplated takes a Terraform ID and the template it's produced
// from and reverse it to get the external name. For example, you can supply
// "/subscription/{{ .paramters.some }}/{{ .external_name }}" with
// "/subscription/someval/myname" and get "myname" returned.
func GetExternalNameFromTemplated(tmpl, val string) (string, error) { //nolint:gocyclo
	// gocyclo: I couldn't find any more room.
	loc := externalNameRegex.FindStringIndex(tmpl)
	// A template without external name usage.
	if loc == nil {
		return val, nil
	}
	leftIndex := loc[0]
	rightIndex := loc[1]

	leftSeparator := ""
	if leftIndex > 0 {
		leftSeparator = string(tmpl[leftIndex-1])
	}
	rightSeparator := ""
	if rightIndex < len(tmpl) {
		rightSeparator = string(tmpl[rightIndex])
	}

	switch {
	// {{ .external_name }}
	case leftSeparator == "" && rightSeparator == "":
		return val, nil
	// {{ .external_name }}/someother
	case leftSeparator == "" && rightSeparator != "":
		return strings.Split(val, rightSeparator)[0], nil
	// /another/{{ .external_name }}/someother
	case leftSeparator != "" && rightSeparator != "":
		leftSeparatorCount := strings.Count(tmpl[:leftIndex+1], leftSeparator)
		// ["", "another","myname/someother"]
		separatedLeft := strings.SplitAfterN(val, leftSeparator, leftSeparatorCount+1)
		// myname/someother
		rightString := separatedLeft[len(separatedLeft)-1]
		// myname
		return strings.Split(rightString, rightSeparator)[0], nil
	// /another/{{ .external_name }}
	case leftSeparator != "" && rightSeparator == "":
		separated := strings.Split(val, leftSeparator)
		return separated[len(separated)-1], nil
	}
	return "", errors.Errorf("unhandled case with template %s and value %s", tmpl, val)
}

// FrameworkResourceWithComputedIdentifier returns an ExternalName
// configuration for a Terraform plugin framework resource with the specified
// computed identifier attribute and the specified placeholder identifier for
// the initial external read calls.
func FrameworkResourceWithComputedIdentifier(identifier, placeholder string) ExternalName {
	en := NewExternalNameFrom(IdentifierFromProvider,
		WithSetIdentifierArgumentsFn(func(fn SetIdentifierArgumentsFn, base map[string]any, externalName string) {
			if id, ok := base[identifier]; !ok || id == placeholder {
				if externalName == "" {
					base[identifier] = placeholder
				} else {
					base[identifier] = externalName
				}
			}
		}),
		WithGetExternalNameFn(func(fn GetExternalNameFn, tfState map[string]any) (string, error) {
			if id, ok := tfState[identifier]; ok {
				idStr := fmt.Sprintf("%v", id)
				if len(idStr) > 0 {
					return idStr, nil
				}
			}
			return "", errors.Errorf("cannot find attribute %q in tfstate", identifier)
		}),
	)
	en.TFPluginFrameworkOptions.ComputedIdentifierAttributes = []string{identifier}
	return en
}

// ExternalNameFrom is an ExternalName configuration which uses a parent
// configuration as its base and modifies any of the GetIDFn,
// GetExternalNameFn or SetIdentifierArgumentsFn. This enables us to reuse
// the existing ExternalName configurations with modifications in their
// behaviors via compositions.
type ExternalNameFrom struct {
	ExternalName
	getIDFn                 func(GetIDFn, context.Context, string, map[string]any, map[string]any) (string, error)
	getExternalNameFn       func(GetExternalNameFn, map[string]any) (string, error)
	setIdentifierArgumentFn func(SetIdentifierArgumentsFn, map[string]any, string)
}

// ExternalNameFromOption is an option that modifies the behavior of an
// ExternalNameFrom external-name configuration.
type ExternalNameFromOption func(from *ExternalNameFrom)

// WithGetIDFn sets the GetIDFn for the ExternalNameFrom configuration.
// The function parameter fn receives the parent ExternalName's GetIDFn, and
// implementations may invoke the parent's GetIDFn via this
// parameter. For the description of the rest of the parameters and return
// values, please see the documentation of GetIDFn.
func WithGetIDFn(fn func(fn GetIDFn, ctx context.Context, externalName string, parameters map[string]any, terraformProviderConfig map[string]any) (string, error)) ExternalNameFromOption {
	return func(ec *ExternalNameFrom) {
		ec.getIDFn = fn
	}
}

// WithGetExternalNameFn sets the GetExternalNameFn for the ExternalNameFrom
// configuration. The function parameter fn receives the parent ExternalName's
// GetExternalNameFn, and implementations may invoke the parent's
// GetExternalNameFn via this parameter. For the description of the rest
// of the parameters and return values, please see the documentation of
// GetExternalNameFn.
func WithGetExternalNameFn(fn func(fn GetExternalNameFn, tfstate map[string]any) (string, error)) ExternalNameFromOption {
	return func(ec *ExternalNameFrom) {
		ec.getExternalNameFn = fn
	}
}

// WithSetIdentifierArgumentsFn sets the SetIdentifierArgumentsFn for the
// ExternalNameFrom configuration. The function parameter fn receives the
// parent ExternalName's SetIdentifierArgumentsFn, and implementations may
// invoke the parent's SetIdentifierArgumentsFn via this
// parameter. For the description of the rest of the parameters and return
// values, please see the documentation of SetIdentifierArgumentsFn.
func WithSetIdentifierArgumentsFn(fn func(fn SetIdentifierArgumentsFn, base map[string]any, externalName string)) ExternalNameFromOption {
	return func(ec *ExternalNameFrom) {
		ec.setIdentifierArgumentFn = fn
	}
}

// NewExternalNameFrom initializes a new ExternalNameFrom with the given parent
// and with the given options. An example configuration that uses a
// TemplatedStringAsIdentifier as its parent (base) and sets a default value
// for the external-name if the external-name is yet not populated is as
// follows:
//
// config.NewExternalNameFrom(config.TemplatedStringAsIdentifier("", "{{ .parameters.type }}/{{ .setup.client_metadata.account_id }}/{{ .external_name }}"),
//
//	config.WithGetIDFn(func(fn config.GetIDFn, ctx context.Context, externalName string, parameters map[string]any, terraformProviderConfig map[string]any) (string, error) {
//		if externalName == "" {
//			externalName = "some random string"
//		}
//		return fn(ctx, externalName, parameters, terraformProviderConfig)
//	}))
func NewExternalNameFrom(parent ExternalName, opts ...ExternalNameFromOption) ExternalName {
	ec := &ExternalNameFrom{
		ExternalName: parent,
	}
	for _, o := range opts {
		o(ec)
	}

	ec.ExternalName.GetIDFn = func(ctx context.Context, externalName string, parameters map[string]any, terraformProviderConfig map[string]any) (string, error) {
		if ec.getIDFn == nil {
			return parent.GetIDFn(ctx, externalName, parameters, terraformProviderConfig)
		}
		return ec.getIDFn(parent.GetIDFn, ctx, externalName, parameters, terraformProviderConfig)
	}
	ec.ExternalName.GetExternalNameFn = func(tfstate map[string]any) (string, error) {
		if ec.getExternalNameFn == nil {
			return parent.GetExternalNameFn(tfstate)
		}
		return ec.getExternalNameFn(parent.GetExternalNameFn, tfstate)
	}
	ec.ExternalName.SetIdentifierArgumentFn = func(base map[string]any, externalName string) {
		if ec.setIdentifierArgumentFn == nil {
			parent.SetIdentifierArgumentFn(base, externalName)
			return
		}
		ec.setIdentifierArgumentFn(parent.SetIdentifierArgumentFn, base, externalName)
	}
	return ec.ExternalName
}
