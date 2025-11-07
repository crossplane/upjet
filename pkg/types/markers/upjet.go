// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package markers

import (
	"fmt"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/errors"

	"github.com/crossplane/upjet/pkg/types/structtag"
)

const (
	markerPrefixUpjet = "+upjet:"
)

const (
	errFmtCannotParseAsUpjet = "cannot parse as an upjet prefix: %s"
	errFmtInvalidTFTag       = "not a valid tf tag: %s"
	errFmtInvalidJSONTag     = "not a valid json tag: %s"
)

var (
	markerPrefixCRDTFTag   = fmt.Sprintf("%scrd:field:TFTag=", markerPrefixUpjet)
	markerPrefixCRDJSONTag = fmt.Sprintf("%scrd:field:JSONTag=", markerPrefixUpjet)
)

// UpjetOptions represents the whole upjet options that could be
// controlled with markers.
type UpjetOptions struct {
	FieldTFTag   *structtag.Value
	FieldJSONTag *structtag.Value
}

func (o UpjetOptions) String() string {
	m := ""

	if o.FieldTFTag != nil {
		m += fmt.Sprintf("%s%s\n", markerPrefixCRDTFTag, o.FieldTFTag.StringWithoutKey())
	}
	if o.FieldJSONTag != nil {
		m += fmt.Sprintf("%s%s\n", markerPrefixCRDJSONTag, o.FieldJSONTag.StringWithoutKey())
	}

	return m
}

// ParseAsUpjetOption parses input line as an upjet option, if it is a
// valid Upjet Option. Returns whether it is parsed or not.
func ParseAsUpjetOption(opts *UpjetOptions, line string) (bool, error) {
	if !strings.HasPrefix(line, markerPrefixUpjet) {
		return false, nil
	}
	ln := strings.TrimSpace(line)
	if strings.HasPrefix(ln, markerPrefixCRDTFTag) {
		t := strings.TrimPrefix(ln, markerPrefixCRDTFTag)
		tag, err := structtag.ParseTF(t)
		if err != nil {
			return false, errors.Wrapf(err, errFmtInvalidTFTag, t)
		}
		opts.FieldTFTag = tag
		return true, nil
	}
	if strings.HasPrefix(ln, markerPrefixCRDJSONTag) {
		t := strings.TrimPrefix(ln, markerPrefixCRDJSONTag)
		tag, err := structtag.ParseJSON(t)
		if err != nil {
			return false, errors.Wrapf(err, errFmtInvalidJSONTag, t)
		}
		opts.FieldJSONTag = tag
		return true, nil
	}
	return false, errors.Errorf(errFmtCannotParseAsUpjet, line)
}
