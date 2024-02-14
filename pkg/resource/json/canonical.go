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
	var d any
	if err := cJSON.Unmarshal([]byte(json), &d); err != nil {
		return "", errors.Wrapf(err, errFmtJSONUnmarshal, json)
	}
	buff, err := cJSON.Marshal(d)
	return string(buff), errors.Wrapf(err, errFmtJSONMarshal, d)
}
