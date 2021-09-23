package markers

import (
	"fmt"

	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
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

// CrossplaneOptions represents the Crossplane marker options that terrajet
// would need to interact
type CrossplaneOptions struct {
	resource.FieldReferenceConfiguration
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
