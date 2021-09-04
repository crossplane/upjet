package markers

import (
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-tools/pkg/markers"
)

const (
	// Prefix is the comment marker prefix
	Prefix = "+"

	markerPrefixCRDTag = "terrajet:crdschema:Tag"

	errFmtCannotParseMarkerLine = "cannot parse marker line: %s"
	errFmtUnknownTerrajetMarker = "unknown terrajet marker %q"
)

// CRDTag is a marker option to set tag for fields in CRD schema
type CRDTag struct {
	TF   *string `marker:"tf,optional,omitempty"`
	JSON *string `marker:"json,optional,omitempty"`
}

func (c CRDTag) getMarkerPrefix() string {
	return markerPrefixCRDTag
}

var terrajetMarkers = []*markers.Definition{
	markers.Must(markers.MakeDefinition(markerPrefixCRDTag, markers.DescribesField, CRDTag{})),
}

// Options represents the whole terrajet options that could be controlled with
// markers.
type Options struct {
	CRDTag CRDTag
}

var markersRegistry = &markers.Registry{}

func init() {
	if err := markers.RegisterAll(markersRegistry, terrajetMarkers...); err != nil {
		panic(errors.Wrap(err, "failed to register terrajet markers"))
	}
}

// ParseIfMarkerForField parses input line as a terrajet marker if it is a
// valid marker describing a field.
func ParseIfMarkerForField(cfg *Options, line string) error {
	md := markersRegistry.Lookup(line, markers.DescribesField)
	if md == nil {
		return nil
	}
	val, err := md.Parse(line)
	if err != nil {
		return errors.Wrapf(err, errFmtCannotParseMarkerLine, line)
	}

	switch val := val.(type) {
	case CRDTag:
		cfg.CRDTag = val
	default:
		return errors.Errorf(errFmtUnknownTerrajetMarker, md.Name)
	}

	return nil
}
