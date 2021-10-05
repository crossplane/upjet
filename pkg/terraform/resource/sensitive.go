/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License mapping

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resource

import (
	"context"

	"github.com/pkg/errors"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	errCannotExpandWildcards = "cannot expand wildcards"

	errFmtCannotGetStringForFieldPath  = "cannot not get a string for fieldpath %q"
	errFmtCannotGetStringsForFieldPath = "cannot get strings matching field fieldpath: %q"
)

// GetConnectionDetails returns strings matching provided field paths in the
// input data.
// See the unit tests for examples.
func GetConnectionDetails(from map[string]interface{}, at map[string]string) (map[string][]byte, error) {
	vals := make(map[string][]byte)
	for tf := range at {
		vs, err := stringsMatchingFieldPath(from, tf)
		if err != nil {
			return nil, errors.Wrapf(err, errFmtCannotGetStringsForFieldPath, tf)
		}
		for k, v := range vs {
			vals[k] = v
		}
	}
	return vals, nil
}

func stringsMatchingFieldPath(from map[string]interface{}, path string) (map[string][]byte, error) {
	paved := fieldpath.Pave(from)
	segments, err := paved.ExpandWildcards(path)
	if err != nil {
		return nil, errors.Wrap(err, errCannotExpandWildcards)
	}

	res := make(map[string][]byte, len(segments))
	for _, s := range segments {
		v, err := paved.GetString(s)
		if err != nil {
			return nil, errors.Wrapf(err, errFmtCannotGetStringForFieldPath, s)
		}
		res[s] = []byte(v)
	}
	return res, nil
}

// GetSensitiveParameters will collect sensitive information as terraform state
// attributes by following secret references in the spec.
func GetSensitiveParameters(ctx context.Context, client SecretClient, from runtime.Object, into map[string]interface{}, mapping map[string]string) error {
	pv, err := fieldpath.PaveObject(from)
	if err != nil {
		return err
	}
	xpParams := map[string]interface{}{}
	if err = pv.GetValueInto("spec.forProvider", &xpParams); err != nil {
		return err
	}
	paveXP := fieldpath.Pave(xpParams)
	paveTF := fieldpath.Pave(into)

	var sensitive []byte
	for tf, xp := range mapping {
		expXPs, err := paveXP.ExpandWildcards(xp)
		if err != nil {
			return errors.Wrapf(err, "cannot expand wildcard for xp resource")
		}
		for _, expXP := range expXPs {
			sel := v1.SecretKeySelector{}
			if err = paveXP.GetValueInto(expXP, &sel); err != nil {
				return errors.Wrapf(err, "cannot get SecretKeySelector from xp resource for fieldpath %q", expXP)
			}
			sensitive, err = client.GetSecretValue(ctx, sel)
			if err != nil {
				return err
			}
			expTF, err := expandedTFPath(expXP, mapping)
			if err != nil {
				return err
			}
			if err = paveTF.SetString(expTF, string(sensitive)); err != nil {
				return errors.Wrapf(err, "cannot set string as terraform attribute for fieldpath %q", tf)
			}
		}
	}

	return nil
}

// GetSensitiveObservation will return sensitive information as terraform state
// attributes by reading them from connection details.
func GetSensitiveObservation(from runtime.Object, into map[string]interface{}, mapping map[string]string) error {
	pv, err := fieldpath.PaveObject(from)
	if err != nil {
		return err
	}
	xpParams := map[string]interface{}{}
	if err = pv.GetValueInto("status.atProvider", &xpParams); err != nil {
		return err
	}
	paveXP := fieldpath.Pave(xpParams)
	paveTF := fieldpath.Pave(into)

	var sensitive string
	for tf, xp := range mapping {
		expXPs, err := paveXP.ExpandWildcards(xp)
		if err != nil {
			return errors.Wrapf(err, "cannot expand wildcard for xp resource")
		}
		for _, expXP := range expXPs {
			if sensitive, err = paveXP.GetString(expXP); err != nil {
				return errors.Wrapf(err, "cannot get string from xp resource for fieldpath %q", expXP)
			}
			expTF, err := expandedTFPath(expXP, mapping)
			if err != nil {
				return err
			}
			if err = paveTF.SetString(expTF, sensitive); err != nil {
				return errors.Wrapf(err, "cannot set string as terraform attribute for fieldpath %q", tf)
			}
		}
	}
	return nil
}

func expandedTFPath(expandedXP string, mapping map[string]string) (string, error) {
	sExp, err := fieldpath.Parse(expandedXP)
	if err != nil {
		return "", err
	}
	tfWildcard := ""
	for tf, xp := range mapping {
		sxp, err := fieldpath.Parse(xp)
		if err != nil {
			return "", err
		}
		if segmentsMatches(sExp, sxp) {
			tfWildcard = tf
			break
		}
	}
	if tfWildcard == "" {
		return "", errors.Errorf("cannot find corresponding fieldpath mapping for %q", expandedXP)
	}
	sTF, err := fieldpath.Parse(tfWildcard)
	if err != nil {
		return "", err
	}
	for i, s := range sTF {
		if s.Field == "*" {
			sTF[i] = sExp[i]
		}
	}

	return sTF.String(), nil
}

func segmentsMatches(a fieldpath.Segments, b fieldpath.Segments) bool {
	if len(a) != len(b) {
		return false
	}
	for i, s := range a {
		sb := b[i]
		if s.Field == "*" || sb.Field == "*" {
			continue
		}
		if s.Type != sb.Type {
			return false
		}
		if s.Field != sb.Field {
			return false
		}
		if s.Index != sb.Index {
			return false
		}
	}
	return true
}
