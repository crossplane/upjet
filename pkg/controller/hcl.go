// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/base64"
	"fmt"
	"log"
	"regexp"
	"unicode/utf8"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	ctyyaml "github.com/zclconf/go-cty-yaml"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	ctyfuncstdlib "github.com/zclconf/go-cty/cty/function/stdlib"
)

var Base64DecodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{
			Name:        "str",
			Type:        cty.String,
			AllowMarked: true,
		},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		str, strMarks := args[0].Unmark()
		s := str.AsString()
		sDec, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return cty.UnknownVal(cty.String), fmt.Errorf("failed to decode base64 data %s", s)
		}
		if !utf8.Valid(sDec) {
			log.Printf("[DEBUG] the result of decoding the provided string is not valid UTF-8: %s", s)
			return cty.UnknownVal(cty.String), fmt.Errorf("the result of decoding the provided string is not valid UTF-8")
		}
		return cty.StringVal(string(sDec)).WithMarks(strMarks), nil
	},
})

var Base64EncodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{
			Name: "str",
			Type: cty.String,
		},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		return cty.StringVal(base64.StdEncoding.EncodeToString([]byte(args[0].AsString()))), nil
	},
})

// evalCtx registers the known functions for HCL processing
// variable interpolation is not supported, as in our case they are irrelevant
var evalCtx = &hcl.EvalContext{
	Variables: map[string]cty.Value{},
	Functions: map[string]function.Function{
		"abs":             ctyfuncstdlib.AbsoluteFunc,
		"ceil":            ctyfuncstdlib.CeilFunc,
		"chomp":           ctyfuncstdlib.ChompFunc,
		"coalescelist":    ctyfuncstdlib.CoalesceListFunc,
		"compact":         ctyfuncstdlib.CompactFunc,
		"concat":          ctyfuncstdlib.ConcatFunc,
		"contains":        ctyfuncstdlib.ContainsFunc,
		"csvdecode":       ctyfuncstdlib.CSVDecodeFunc,
		"distinct":        ctyfuncstdlib.DistinctFunc,
		"element":         ctyfuncstdlib.ElementFunc,
		"chunklist":       ctyfuncstdlib.ChunklistFunc,
		"flatten":         ctyfuncstdlib.FlattenFunc,
		"floor":           ctyfuncstdlib.FloorFunc,
		"format":          ctyfuncstdlib.FormatFunc,
		"formatdate":      ctyfuncstdlib.FormatDateFunc,
		"formatlist":      ctyfuncstdlib.FormatListFunc,
		"indent":          ctyfuncstdlib.IndentFunc,
		"join":            ctyfuncstdlib.JoinFunc,
		"jsondecode":      ctyfuncstdlib.JSONDecodeFunc,
		"jsonencode":      ctyfuncstdlib.JSONEncodeFunc,
		"keys":            ctyfuncstdlib.KeysFunc,
		"log":             ctyfuncstdlib.LogFunc,
		"lower":           ctyfuncstdlib.LowerFunc,
		"max":             ctyfuncstdlib.MaxFunc,
		"merge":           ctyfuncstdlib.MergeFunc,
		"min":             ctyfuncstdlib.MinFunc,
		"parseint":        ctyfuncstdlib.ParseIntFunc,
		"pow":             ctyfuncstdlib.PowFunc,
		"range":           ctyfuncstdlib.RangeFunc,
		"regex":           ctyfuncstdlib.RegexFunc,
		"regexall":        ctyfuncstdlib.RegexAllFunc,
		"reverse":         ctyfuncstdlib.ReverseListFunc,
		"setintersection": ctyfuncstdlib.SetIntersectionFunc,
		"setproduct":      ctyfuncstdlib.SetProductFunc,
		"setsubtract":     ctyfuncstdlib.SetSubtractFunc,
		"setunion":        ctyfuncstdlib.SetUnionFunc,
		"signum":          ctyfuncstdlib.SignumFunc,
		"slice":           ctyfuncstdlib.SliceFunc,
		"sort":            ctyfuncstdlib.SortFunc,
		"split":           ctyfuncstdlib.SplitFunc,
		"strrev":          ctyfuncstdlib.ReverseFunc,
		"substr":          ctyfuncstdlib.SubstrFunc,
		"timeadd":         ctyfuncstdlib.TimeAddFunc,
		"title":           ctyfuncstdlib.TitleFunc,
		"trim":            ctyfuncstdlib.TrimFunc,
		"trimprefix":      ctyfuncstdlib.TrimPrefixFunc,
		"trimspace":       ctyfuncstdlib.TrimSpaceFunc,
		"trimsuffix":      ctyfuncstdlib.TrimSuffixFunc,
		"upper":           ctyfuncstdlib.UpperFunc,
		"values":          ctyfuncstdlib.ValuesFunc,
		"zipmap":          ctyfuncstdlib.ZipmapFunc,
		"yamldecode":      ctyyaml.YAMLDecodeFunc,
		"yamlencode":      ctyyaml.YAMLEncodeFunc,
		"base64encode":    Base64EncodeFunc,
		"base64decode":    Base64DecodeFunc,
	},
}

// hclBlock is the target type for decoding the specially-crafted HCL document.
// interested in processing HCL snippets for a single parameter
type hclBlock struct {
	Parameter string `hcl:"parameter"`
}

// isHCLSnippetPattern is the regex pattern for determining whether
// the param is an HCL template
var isHCLSnippetPattern = regexp.MustCompile(`\$\{\w+\s*\([\S\s]*\}`)

// processHCLParam processes the given string parameter
// with HCL format and including HCL functions,
// coming from the Managed Resource spec parameters.
// It prepares a tailored HCL snippet which consist of only a single attribute
// parameter = theGivenParameterValueInHCLSyntax
// It only operates on string parameters, and returns a string.
// caller should ensure that the given parameter is an HCL snippet
func processHCLParam(param string) (string, error) {
	param = fmt.Sprintf("parameter = \"%s\"\n", param)
	return processHCLParamBytes([]byte(param))
}

// processHCLParamBytes parses and decodes the HCL snippet
func processHCLParamBytes(paramValueBytes []byte) (string, error) {
	hclParser := hclparse.NewParser()
	// here the filename argument is not important,
	// used by the hcl parser lib for tracking caching purposes
	// it is just a name reference
	hclFile, diag := hclParser.ParseHCL(paramValueBytes, "dummy.hcl")
	if diag.HasErrors() {
		return "", diag
	}

	var paramWrapper hclBlock
	diags := gohcl.DecodeBody(hclFile.Body, evalCtx, &paramWrapper)
	if diags.HasErrors() {
		return "", diags
	}

	return paramWrapper.Parameter, nil
}
