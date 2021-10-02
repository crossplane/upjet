package json

import (
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
)

const (
	errCannotExpandWildcards = "cannot expand wildcards"

	errFmtCannotGetStringsForParts     = "cannot not get a string for parts %v"
	errFmtCannotGetStringsForFieldPath = "cannot get strings matching field fieldpath: \"%s\""
)

// StringsMatchingFieldPaths returns strings matching provided field paths in the
// input data.
// See the unit tests for examples.
func StringsMatchingFieldPaths(data []byte, fieldPaths []string) (map[string][]byte, error) {
	vals := make(map[string][]byte)
	for _, fp := range fieldPaths {
		s, err := fieldpath.Parse(fp)
		if err != nil {
			return nil, err
		}
		vs, err := stringsMatchingFieldPath(data, s)
		if err != nil {
			return nil, errors.Wrapf(err, errFmtCannotGetStringsForFieldPath, fp)
		}
		for k, v := range vs {
			vals[k] = v
		}
	}
	return vals, nil
}

func stringsMatchingFieldPath(data []byte, seg fieldpath.Segments) (map[string][]byte, error) {
	dataMap := map[string]interface{}{}
	JSParser.Unmarshal(data, &dataMap)

	paved := fieldpath.Pave(dataMap)
	segs, err := paved.ExpandWildcards(seg)
	if err != nil {
		return nil, errors.Wrap(err, errCannotExpandWildcards)
	}

	res := make(map[string][]byte, len(segs))
	for _, s := range segs {
		v, err := paved.GetString(s.String())
		if err != nil {
			return nil, errors.Wrapf(err, errFmtCannotGetStringsForParts, s)
		}
		res[s.String()] = []byte(v)
	}
	return res, nil
}

func pathsForSegments(seg fieldpath.Segments) []interface{} {
	res := make([]interface{}, len(seg))

	for i, s := range seg {
		switch s.Type {
		case fieldpath.SegmentField:
			if s.Field == "*" {
				res[i] = '*'
				continue
			}
			res[i] = s.Field
		case fieldpath.SegmentIndex:
			res[i] = int(s.Index)
		}
	}

	return res
}
