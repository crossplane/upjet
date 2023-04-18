/*
Copyright 2021 Upbound Inc.
*/

package config

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
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
// parameters: A tree of parameters that you'd normally see in a Terraform HCL
//
//	file. You can use TF registry documentation of given resource to
//	see what's available.
//
// setup.configuration: The Terraform configuration object of the provider. You can
//
//	take a look at the TF registry provider configuration object
//	to see what's available. Not to be confused with ProviderConfig
//	custom resource of the Crossplane provider.
//
// setup.client_metadata: The Terraform client metadata available for the provider,
//
//	such as the AWS account ID for the AWS provider.
//
// external_name: The value of external name annotation of the custom resource.
//
//	It is required to use this as part of the template.
//
// The following template functions are available:
// ToLower: Converts the contents of the pipeline to lower-case
// ToUpper: Converts the contents of the pipeline to upper-case
// Please note that it's currently *not* possible to use
// the template functions on the .external_name template variable.
// Example usages:
// TemplatedStringAsIdentifier("index_name", "/subscriptions/{{ .setup.configuration.subscription }}/{{ .external_name }}")
// TemplatedStringAsIdentifier("index_name", "/resource/{{ .external_name }}/static")
// TemplatedStringAsIdentifier("index_name", "{{ .parameters.cluster_id }}:{{ .parameters.node_id }}:{{ .external_name }}")
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
