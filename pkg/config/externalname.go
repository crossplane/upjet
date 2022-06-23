/*
Copyright 2021 Upbound Inc.
*/

package config

import (
	"bytes"
	"context"
	"strings"
	"text/template"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
)

const (
	errIDNotFoundInTFState = "id does not exist in tfstate"
)

var (
	// NameAsIdentifier uses "name" field in the arguments as the identifier of
	// the resource.
	NameAsIdentifier = ExternalName{
		SetIdentifierArgumentFn: func(base map[string]interface{}, name string) {
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
)

// ParameterAsIdentifier uses the given field name in the arguments as the
// identifier of the resource.
func ParameterAsIdentifier(param string) ExternalName {
	e := NameAsIdentifier
	e.SetIdentifierArgumentFn = func(base map[string]interface{}, name string) {
		base[param] = name
	}
	e.OmittedFields = []string{
		param,
		param + "_prefix",
	}
	return e
}

// TemplatedStringAsIdentifier accepts a template as the shape of the Terraform
// ID and lets you provide a field path for the argument you're using as external
// name. The available variables you can use in the template are as follows:
// parameters: A tree of parameters that you'd normally see in a Terraform HCL
//             file. You can use TF registry documentation of given resource to
//             see what's available.
// providerConfig: The Terraform configuration object of the provider. You can
//                 take a look at the TF registry provider configuration object
//                 to see what's available. Not to be confused with ProviderConfig
//                 custom resource of the Crossplane provider.
// externalName: The value of external name annotation of the custom resource.
//               It is required to use this as part of the template.
//
// Example usages:
// TemplatedStringAsIdentifier("index_name", "/subscriptions/{{ .providerConfig.subscription }}/{{ .externalName }}")
// TemplatedStringAsIdentifier("index.name", "/resource/{{ .externalName }}/static")
// TemplatedStringAsIdentifier("index.name", "{{ .parameters.cluster_id }}:{{ .parameters.node_id }}:{{ .externalName }}")
func TemplatedStringAsIdentifier(nameFieldPath, tmpl string) ExternalName {
	t, err := template.New("getid").Parse(tmpl)
	if err != nil {
		panic(errors.Wrap(err, "cannot parse template"))
	}
	return ExternalName{
		SetIdentifierArgumentFn: func(base map[string]interface{}, externalName string) {
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
		GetIDFn: func(ctx context.Context, externalName string, parameters map[string]interface{}, providerConfig map[string]interface{}) (string, error) {
			o := map[string]interface{}{
				"externalName":   externalName,
				"parameters":     parameters,
				"providerConfig": providerConfig,
			}
			b := bytes.Buffer{}
			if err := t.Execute(&b, o); err != nil {
				return "", errors.Wrap(err, "cannot execute template")
			}
			return b.String(), nil
		},
		GetExternalNameFn: func(tfstate map[string]interface{}) (string, error) {
			id, ok := tfstate["id"]
			if !ok {
				return "", errors.New(errIDNotFoundInTFState)
			}
			return GetExternalNameFromTemplated(tmpl, id.(string))
		},
	}
}

// GetExternalNameFromTemplated takes a Terraform ID and the template it's produced
// from and reverse it to get the external name. For example, you can supply
// "/subscription/{{ .paramters.some }}/{{ .externalName }}" with
// "/subscription/someval/myname" and get "myname" returned.
func GetExternalNameFromTemplated(tmpl, val string) (string, error) { //nolint:gocyclo
	// gocyclo: I couldn't find any more room.
	leftIndex, length := findExternalNameInTemplate(tmpl)
	// A template without external name usage.
	if length == 0 {
		return val, nil
	}
	leftSeparator := ""
	if leftIndex > 0 {
		leftSeparator = string(tmpl[leftIndex-1])
	}
	rightSeparator := ""
	rightIndex := leftIndex + length
	if rightIndex < len(tmpl) {
		rightSeparator = string(tmpl[rightIndex])
	}

	switch {
	// {{ .externalName }}
	case leftSeparator == "" && rightSeparator == "":
		return val, nil
	// {{ .externalName }}/someother
	case leftSeparator == "" && rightSeparator != "":
		return strings.Split(val, rightSeparator)[0], nil
	// /another/{{ .externalName }}/someother
	case leftSeparator != "" && rightSeparator != "":
		leftSeparatorCount := strings.Count(tmpl[:leftIndex+1], leftSeparator)
		// ["", "another","myname/someother"]
		separatedLeft := strings.SplitAfterN(val, leftSeparator, leftSeparatorCount+1)
		// myname/someother
		rightString := separatedLeft[len(separatedLeft)-1]
		// myname
		return strings.Split(rightString, rightSeparator)[0], nil
	// /another/{{ .externalName }}
	case leftSeparator != "" && rightSeparator == "":
		separated := strings.Split(val, leftSeparator)
		return separated[len(separated)-1], nil
	}
	return "", errors.Errorf("unhandled case with template %s and value %s", tmpl, val)
}

func findExternalNameInTemplate(tmpl string) (i int, length int) {
	cases := []string{
		"{{ .externalName }}",
		"{{.externalName }}",
		"{{ .externalName}}",
		"{{.externalName}}",
	}
	i = -1
	length = 0
	for _, c := range cases {
		i = strings.Index(tmpl, c)
		if i != -1 {
			length = len(c)
			break
		}
	}
	return
}
