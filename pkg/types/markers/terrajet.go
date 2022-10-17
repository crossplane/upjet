package markers

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

const (
	markerPrefixUpjet = "+upjet:"

	errFmtCannotParseAsUpjet = "cannot parse as a upjet prefix: %s"
)

var (
	markerPrefixCRDTFTag   = fmt.Sprintf("%scrd:field:TFTag=", markerPrefixUpjet)
	markerPrefixCRDJSONTag = fmt.Sprintf("%scrd:field:JSONTag=", markerPrefixUpjet)
)

// UpjetOptions represents the whole upjet options that could be
// controlled with markers.
type UpjetOptions struct {
	FieldTFTag   *string
	FieldJSONTag *string
}

func (o UpjetOptions) String() string {
	m := ""

	if o.FieldTFTag != nil {
		m += fmt.Sprintf("%s%s\n", markerPrefixCRDTFTag, *o.FieldTFTag)
	}
	if o.FieldJSONTag != nil {
		m += fmt.Sprintf("%s%s\n", markerPrefixCRDJSONTag, *o.FieldJSONTag)
	}

	return m
}

// ParseAsUpjetOption parses input line as a upjet option, if it is a
// valid Upjet Option. Returns whether it is parsed or not.
func ParseAsUpjetOption(opts *UpjetOptions, line string) (bool, error) {
	if !strings.HasPrefix(line, markerPrefixUpjet) {
		return false, nil
	}
	ln := strings.TrimSpace(line)
	if strings.HasPrefix(ln, markerPrefixCRDTFTag) {
		t := strings.TrimPrefix(ln, markerPrefixCRDTFTag)
		opts.FieldTFTag = &t
		return true, nil
	}
	if strings.HasPrefix(ln, markerPrefixCRDJSONTag) {
		t := strings.TrimPrefix(ln, markerPrefixCRDJSONTag)
		opts.FieldJSONTag = &t
		return true, nil
	}
	return false, errors.Errorf(errFmtCannotParseAsUpjet, line)
}
