package markers

import (
	"fmt"

	"github.com/upbound/upjet/pkg/config"
)

const (
	markerPrefixCrossplane = "+crossplane:"
)

var (
	markerPrefixRefType         = fmt.Sprintf("%sgenerate:reference:type=", markerPrefixCrossplane)
	markerPrefixRefExtractor    = fmt.Sprintf("%sgenerate:reference:extractor=", markerPrefixCrossplane)
	markerPrefixRefFieldName    = fmt.Sprintf("%sgenerate:reference:refFieldName=", markerPrefixCrossplane)
	markerPrefixRefSelectorName = fmt.Sprintf("%sgenerate:reference:selectorFieldName=", markerPrefixCrossplane)
)

// CrossplaneOptions represents the Crossplane marker options that upjet
// would need to interact
type CrossplaneOptions struct {
	config.Reference
}

func (o CrossplaneOptions) String() string {
	m := ""

	if o.Type != "" {
		m += fmt.Sprintf("%s%s\n", markerPrefixRefType, o.Type)
	}
	if o.Extractor != "" {
		m += fmt.Sprintf("%s%s\n", markerPrefixRefExtractor, o.Extractor)
	}
	if o.RefFieldName != "" {
		m += fmt.Sprintf("%s%s\n", markerPrefixRefFieldName, o.RefFieldName)
	}
	if o.SelectorFieldName != "" {
		m += fmt.Sprintf("%s%s\n", markerPrefixRefSelectorName, o.SelectorFieldName)
	}

	return m
}
