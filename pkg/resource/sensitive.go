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
	"fmt"
	"regexp"
	"strings"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
)

const (
	errCannotExpandWildcards = "cannot expand wildcards"

	errFmtCannotGetStringForFieldPath = "cannot not get a string for fieldpath %q"
)

var reEndsWithIndex *regexp.Regexp
var reMiddleIndex *regexp.Regexp
var reInsideThreeDotsBlock *regexp.Regexp

func init() {
	reEndsWithIndex = regexp.MustCompile(`\.(\d+?)$`)
	reMiddleIndex = regexp.MustCompile(`\.(\d+?)\.`)
	reInsideThreeDotsBlock = regexp.MustCompile(`\.\.\.(.*?)\.\.\.`)
}

// SecretClient is the client to get sensitive data from kubernetes secrets
//go:generate go run github.com/golang/mock/mockgen -copyright_file ../../hack/boilerplate.txt -destination ./fake/mocks/mock.go -package mocks github.com/crossplane-contrib/terrajet/pkg/resource SecretClient
type SecretClient interface {
	GetSecretData(ctx context.Context, ref *v1.SecretReference) (map[string][]byte, error)
	GetSecretValue(ctx context.Context, sel v1.SecretKeySelector) ([]byte, error)
}

// GetConnectionDetails returns strings matching provided field paths in the
// input data.
// See the unit tests for examples.
func GetConnectionDetails(from map[string]interface{}, mapping map[string]string, idField string) (map[string][]byte, error) {
	if len(mapping) == 0 {
		return nil, nil
	}
	vals := make(map[string][]byte)
	for tf := range mapping {
		paved := fieldpath.Pave(from)
		fieldPaths, err := paved.ExpandWildcards(tf)
		if err != nil {
			return nil, errors.Wrap(err, errCannotExpandWildcards)
		}

		for _, fp := range fieldPaths {
			v, err := paved.GetString(fp)
			if err != nil {
				return nil, errors.Wrapf(err, errFmtCannotGetStringForFieldPath, fp)
			}
			k, err := fieldPathToSecretKey(fp)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot convert fieldpath %q to secret key", fp)
			}
			vals[k] = []byte(v)
		}
	}

	id, ok := from[idField].(string)
	if ok {
		vals[idField] = []byte(id)
	}
	return vals, nil
}

// GetSensitiveParameters will collect sensitive information as terraform state
// attributes by following secret references in the spec.
func GetSensitiveParameters(ctx context.Context, client SecretClient, from runtime.Object, into map[string]interface{}, mapping map[string]string) error { //nolint: gocyclo
	// Note(turkenh): Cyclomatic complexity of this function is slightly higher
	// than the threshold but preferred to use nolint directive for better
	// readability and not to split the logic.

	if len(mapping) == 0 {
		return nil
	}

	pavedJSON, err := fieldpath.PaveObject(from)
	if err != nil {
		return err
	}
	pavedTF := fieldpath.Pave(into)

	var sensitive []byte
	for tfPath, jsonPath := range mapping {
		jsonPathSet, err := pavedJSON.ExpandWildcards(jsonPath)
		if err != nil {
			return errors.Wrapf(err, "cannot expand wildcard for xp resource")
		}
		for _, expandedJSONPath := range jsonPathSet {
			sel := &v1.SecretKeySelector{}
			if err = pavedJSON.GetValueInto(expandedJSONPath, sel); err != nil {
				return errors.Wrapf(err, "cannot get SecretKeySelector from xp resource for fieldpath %q", expandedJSONPath)
			}
			sensitive, err = client.GetSecretValue(ctx, *sel)
			if err != nil {
				return errors.Wrapf(err, "cannot get secret value for %v", sel)
			}
			expTF, err := expandedTFPath(expandedJSONPath, mapping)
			if err != nil {
				return err
			}
			if err = pavedTF.SetString(expTF, string(sensitive)); err != nil {
				return errors.Wrapf(err, "cannot set string as terraform attribute for fieldpath %q", tfPath)
			}
		}
	}

	return nil
}

// GetSensitiveObservation will return sensitive information as terraform state
// attributes by reading them from connection details.
func GetSensitiveObservation(ctx context.Context, client SecretClient, from *v1.SecretReference, into map[string]interface{}) error {
	if from == nil {
		// No secret reference set
		return nil
	}
	conn, err := client.GetSecretData(ctx, from)
	if kerrors.IsNotFound(err) {
		// Secret not available/created yet
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "cannot get connection secret")
	}

	paveTF := fieldpath.Pave(into)
	for k, v := range conn {
		fp, err := secretKeyToFieldPath(k)
		if err != nil {
			return errors.Wrapf(err, "cannot convert secret key %q to fieldpath", k)
		}
		if err = paveTF.SetString(fp, string(v)); err != nil {
			return errors.Wrapf(err, "cannot set sensitive string in tf attributes for fieldpath %q", fp)
		}
	}
	return nil
}

func expandedTFPath(expandedXP string, mapping map[string]string) (string, error) {
	sExp, err := fieldpath.Parse(normalizeJSONPath(expandedXP))
	if err != nil {
		return "", err
	}
	tfWildcard := ""
	for tf, xp := range mapping {
		sxp, err := fieldpath.Parse(normalizeJSONPath(xp))
		if err != nil {
			return "", err
		}
		if expandedFor(sExp, sxp) {
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

func expandedFor(expanded fieldpath.Segments, withWildcard fieldpath.Segments) bool {
	if len(withWildcard) != len(expanded) {
		return false
	}
	for i, w := range withWildcard {
		exp := expanded[i]
		if w.Field == "*" {
			continue
		}
		if w.Type != exp.Type {
			return false
		}
		if w.Field != exp.Field {
			return false
		}
		if w.Index != exp.Index {
			return false
		}
	}
	return true
}

func normalizeJSONPath(s string) string {
	return strings.TrimPrefix(strings.TrimPrefix(s, "spec.forProvider."), "status.atProvider.")
}

func secretKeyToFieldPath(s string) (string, error) {
	s1 := reInsideThreeDotsBlock.ReplaceAllString(s, "[$1]")
	s2 := reEndsWithIndex.ReplaceAllString(s1, "[$1]")
	s3 := reMiddleIndex.ReplaceAllString(s2, "[$1].")
	seg, err := fieldpath.Parse(s3)
	if err != nil {
		return "", errors.Wrapf(err, "cannot parse secret key %q as fieldpath", s3)
	}
	return seg.String(), nil
}

func fieldPathToSecretKey(s string) (string, error) {
	sg, err := fieldpath.Parse(s)
	if err != nil {
		return "", errors.Wrapf(err, "cannot parse %q as fieldpath", s)
	}

	var b strings.Builder
	for _, s := range sg {
		switch s.Type {
		case fieldpath.SegmentField:
			if strings.ContainsRune(s.Field, '.') {
				b.WriteString(fmt.Sprintf("...%s...", s.Field))
				continue
			}
			b.WriteString(fmt.Sprintf(".%s", s.Field))
		case fieldpath.SegmentIndex:
			b.WriteString(fmt.Sprintf(".%d", s.Index))
		}
	}

	return strings.TrimPrefix(b.String(), "."), nil
}
