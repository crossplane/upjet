// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/resource/json"
)

const (
	errFmtNotJSONString = "parameter at path %q with value %v is not a (JSON) string"
	errFmtCanonicalize  = "failed to canonicalize the parameter at path %q"
)

// CanonicalizeJSONParameters returns a ConfigurationInjector that computes
// and stores the canonical forms of the JSON documents for the specified list
// of top-level Terraform configuration arguments. Please note that currently
// only top-level configuration arguments are supported by this function.
func CanonicalizeJSONParameters(tfPath ...string) ConfigurationInjector {
	return func(jsonMap map[string]any, tfMap map[string]any) error {
		for _, param := range tfPath {
			p, ok := tfMap[param]
			if !ok {
				continue
			}
			s, ok := p.(string)
			if !ok {
				return errors.Errorf(errFmtNotJSONString, param, p)
			}
			if s == "" {
				continue
			}
			cJSON, err := json.Canonicalize(s)
			if err != nil {
				return errors.Wrapf(err, errFmtCanonicalize, param)
			}
			tfMap[param] = cJSON
		}
		return nil
	}
}
