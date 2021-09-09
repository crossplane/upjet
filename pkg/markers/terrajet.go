package markers

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

const (
	markerPrefixTerrajet = "+terrajet:"

	errFmtCannotParse = "cannot parse as a terrajet prefix: %s"
)

var (
	markerPrefixCRDTFTag   = fmt.Sprintf("%scrdfield:TFTag=", markerPrefixTerrajet)
	markerPrefixCRDJsonTag = fmt.Sprintf("%scrdfield:JsonTag=", markerPrefixTerrajet)
)

// TerrajetOptions represents the whole terrajet options that could be
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
		m += fmt.Sprintf("%s%s\n", markerPrefixCRDJsonTag, *o.FieldJSONTag)
	}

	return m
}

// ParseAsTerrajetOption parses input line as a terrajet option, if it is a
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
	if strings.HasPrefix(ln, markerPrefixCRDJsonTag) {
		t := strings.TrimPrefix(ln, markerPrefixCRDJsonTag)
		opts.FieldJSONTag = &t
		return true, nil
	}
	return false, errors.Errorf(errFmtCannotParse, line)
}
