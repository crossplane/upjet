package markers

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

const (
	markerPrefixCrossplane = "+crossplane:"

	errFmtCannotParseAsCrossplane = "cannot parse as a crossplane marker: %s"
)

var (
	markerPrefixRefType         = fmt.Sprintf("%sgenerate:reference:type=", markerPrefixCrossplane)
	markerPrefixRefExtractor    = fmt.Sprintf("%sgenerate:reference:extractor=", markerPrefixCrossplane)
	markerPrefixRefFieldName    = fmt.Sprintf("%sgenerate:reference:refFieldName=", markerPrefixCrossplane)
	markerPrefixRefSelectorName = fmt.Sprintf("%sgenerate:reference:selectorFieldName=", markerPrefixCrossplane)
)

// CrossplaneOptions represents the Crossplane marker options that terrajet
// would need to interact
type CrossplaneOptions struct {
	ReferenceToType            string
	ReferenceExtractor         string
	ReferenceFieldName         string
	ReferenceSelectorFieldName string
}

func (o CrossplaneOptions) String() string {
	m := ""

	if o.ReferenceToType != "" {
		m += fmt.Sprintf("%s%s\n", markerPrefixRefType, o.ReferenceToType)
	}
	if o.ReferenceExtractor != "" {
		m += fmt.Sprintf("%s%s\n", markerPrefixRefExtractor, o.ReferenceExtractor)
	}
	if o.ReferenceFieldName != "" {
		m += fmt.Sprintf("%s%s\n", markerPrefixRefFieldName, o.ReferenceFieldName)
	}
	if o.ReferenceSelectorFieldName != "" {
		m += fmt.Sprintf("%s%s\n", markerPrefixRefSelectorName, o.ReferenceSelectorFieldName)
	}

	return m
}

// ParseAsCrossplaneOption parses input line as a crossplane option, if it is a
// valid Crossplane Option. Returns whether it is parsed or not.
func ParseAsCrossplaneOption(opts *CrossplaneOptions, line string) (bool, error) {
	if !strings.HasPrefix(line, markerPrefixCrossplane) {
		return false, nil
	}
	ln := strings.TrimSpace(line)
	if strings.HasPrefix(ln, markerPrefixRefType) {
		t := strings.TrimPrefix(ln, markerPrefixRefType)
		opts.ReferenceToType = t
		return true, nil
	}
	if strings.HasPrefix(ln, markerPrefixRefExtractor) {
		t := strings.TrimPrefix(ln, markerPrefixRefExtractor)
		opts.ReferenceExtractor = t
		return true, nil
	}
	if strings.HasPrefix(ln, markerPrefixRefFieldName) {
		t := strings.TrimPrefix(ln, markerPrefixRefFieldName)
		opts.ReferenceFieldName = t
		return true, nil
	}
	if strings.HasPrefix(ln, markerPrefixRefSelectorName) {
		t := strings.TrimPrefix(ln, markerPrefixRefSelectorName)
		opts.ReferenceSelectorFieldName = t
		return true, nil
	}
	return false, errors.Errorf(errFmtCannotParseAsCrossplane, line)
}
