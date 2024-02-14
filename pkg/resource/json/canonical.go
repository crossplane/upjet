// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package json

import (
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
)

const (
	errFmtJSONUnmarshal = "failed to unmarshal the JSON document: %s"
	errFmtJSONMarshal   = "failed to marshal the dictionary: %v"
)

var (
	cJSON = jsoniter.Config{
		SortMapKeys: true,
	}.Froze()
)

// Canonicalize minifies and orders the keys of the specified JSON document
// to return a canonical form of it, along with any errors encountered during
// the process.
func Canonicalize(json string) (string, error) {
	var m map[string]any
	if err := cJSON.Unmarshal([]byte(json), &m); err != nil {
		return "", errors.Wrapf(err, errFmtJSONUnmarshal, json)
	}
	buff, err := cJSON.Marshal(m)
	return string(buff), errors.Wrapf(err, errFmtJSONMarshal, m)
}
