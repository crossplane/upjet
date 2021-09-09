package markers

import (
	"fmt"
	"strings"
)

const (
	markerPrefixTerrajet = "+terrajet:"
)

var (
	markerPrefixCRDTFTag   = fmt.Sprintf("%scrdfield:TFTag=", markerPrefixTerrajet)
	markerPrefixCRDJsonTag = fmt.Sprintf("%scrdfield:JsonTag=", markerPrefixTerrajet)
)

// TerrajetOptions represents the whole terrajet options that could be
// controlled with markers.
type TerrajetOptions struct {
	FieldTFTag   *string
	FieldJsonTag *string
}

func (o TerrajetOptions) String() string {
	m := ""

	if o.FieldTFTag != nil {
		m += fmt.Sprintf("%s%s\n", markerPrefixCRDTFTag, *o.FieldTFTag)
	}
	if o.FieldJsonTag != nil {
		m += fmt.Sprintf("%s%s\n", markerPrefixCRDJsonTag, *o.FieldJsonTag)
	}

	return m
}

func ParseAsTerrajetOption(opts *TerrajetOptions, line string) bool {
	parsed := false
	ln := strings.TrimSpace(line)
	if strings.HasPrefix(ln, markerPrefixCRDTFTag) {
		t := strings.TrimPrefix(ln, markerPrefixCRDTFTag)
		opts.FieldTFTag = &t
		parsed = true
	}
	if strings.HasPrefix(ln, markerPrefixCRDJsonTag) {
		t := strings.TrimPrefix(ln, markerPrefixCRDJsonTag)
		opts.FieldJsonTag = &t
		parsed = true
	}
	return parsed
}
