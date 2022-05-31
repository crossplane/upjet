package markers

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

const (
	markerPrefixTerrajet = "+terrajet:"

	errFmtCannotParseAsTerrajet = "cannot parse as a terrajet prefix: %s"
)

var (
	markerPrefixCRDTFTag   = fmt.Sprintf("%scrd:field:TFTag=", markerPrefixTerrajet)
	markerPrefixCRDJSONTag = fmt.Sprintf("%scrd:field:JSONTag=", markerPrefixTerrajet)
)

// TerrajetOptions represents the whole upjet options that could be
// controlled with markers.
type TerrajetOptions struct {
	FieldTFTag   *string
	FieldJSONTag *string
}

func (o TerrajetOptions) String() string {
	m := ""

	if o.FieldTFTag != nil {
		m += fmt.Sprintf("%s%s\n", markerPrefixCRDTFTag, *o.FieldTFTag)
	}
	if o.FieldJSONTag != nil {
		m += fmt.Sprintf("%s%s\n", markerPrefixCRDJSONTag, *o.FieldJSONTag)
	}

	return m
}

// ParseAsTerrajetOption parses input line as a upjet option, if it is a
// valid Terrajet Option. Returns whether it is parsed or not.
func ParseAsTerrajetOption(opts *TerrajetOptions, line string) (bool, error) {
	if !strings.HasPrefix(line, markerPrefixTerrajet) {
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
	return false, errors.Errorf(errFmtCannotParseAsTerrajet, line)
}
