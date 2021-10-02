package json

import (
	"strconv"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
)

const (
	errCannotExpandWildcards = "cannot expand wildcards"

	errFmtUnexpectedTypeForValue       = "unexpected type %v for value, expecting a bool, string or number"
	errFmtCannotGetStringsForParts     = "cannot not get a string for parts %v"
	errFmtCannotGetStringsForFieldPath = "cannot get strings matching field fieldpath: \"%s\""
	errFmtUnexpectedWildcardUsage      = "unexpected wildcard usage for field type %v"
	errFmtCannotExpandForArray         = "cannot expand wildcard for array with parts %v"
	errFmtCannotExpandForObject        = "cannot expand wildcard for object with parts %v"
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
		vs, err := stringsMatchingFieldPath(data, pathsForSegments(s))
		if err != nil {
			return nil, errors.Wrapf(err, errFmtCannotGetStringsForFieldPath, fp)
		}
		for k, v := range vs {
			vals[k] = v
		}
	}
	return vals, nil
}

func stringsMatchingFieldPath(data []byte, parts []interface{}) (map[string][]byte, error) {
	keys, err := expandWildcards(jsoniter.Get(data), parts)
	if err != nil {
		return nil, errors.Wrap(err, errCannotExpandWildcards)
	}

	res := make(map[string][]byte, len(keys))

	dataMap := map[string]interface{}{}
	JSParser.Unmarshal(data, &dataMap)

	for _, k := range keys {
		seg := make(fieldpath.Segments, len(k))
		for i, kp := range k {
			switch vp := kp.(type) {
			case int:
				seg[i] = fieldpath.FieldOrIndex(strconv.Itoa(vp))
			case string:
				seg[i] = fieldpath.Field(vp)
			default:
				return nil, errors.Errorf("unexpected type in fieldpath for path %v, expecting int or string", kp)
			}
		}
		pave := fieldpath.Pave(dataMap)
		v, err := pave.GetString(seg.String())
		if err != nil {
			return nil, errors.Wrapf(err, errFmtCannotGetStringsForParts, k)
		}
		res[seg.String()] = []byte(v)
	}

	return res, nil
}

func expandWildcards(a jsoniter.Any, parts []interface{}) ([][]interface{}, error) { // nolint:gocyclo
	var res [][]interface{}

	for i, v := range parts {
		if v == '*' {
			b := a.Get(parts[:i]...)
			switch b.ValueType() {
			case jsoniter.ArrayValue:
				for j := 0; j < b.Size(); j++ {
					np := make([]interface{}, len(parts))
					copy(np, parts)
					np = append(append(np[:i], j), np[i+1:]...)
					r, err := expandWildcards(a, np)
					if err != nil {
						return nil, errors.Wrapf(err, errFmtCannotExpandForArray, np)
					}
					res = append(res, r...)
				}
			case jsoniter.ObjectValue:
				for _, k := range b.Keys() {
					np := make([]interface{}, len(parts))
					copy(np, parts)
					np = append(append(np[:i], k), np[i+1:]...)
					r, err := expandWildcards(a, np)
					if err != nil {
						return nil, errors.Wrapf(err, errFmtCannotExpandForObject, np)
					}
					res = append(res, r...)
				}
			case jsoniter.NilValue, jsoniter.BoolValue, jsoniter.NumberValue, jsoniter.StringValue:
				// We don't expect wildcard after these value types.
				return res, errors.Errorf(errFmtUnexpectedWildcardUsage, b.ValueType())
			case jsoniter.InvalidValue:
				// If we are getting an invalid value, this means we reached
				// to the end of our traversal for this wildcard and should return
				// no results.
				return nil, nil
			}
			return res, nil
		}

	}
	return append(res, parts), nil
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
