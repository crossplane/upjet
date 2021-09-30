package json

import (
	"fmt"
	"strconv"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
)

const (
	errCannotExpandWildcards = "cannot expand wildcards"

	errFmtUnexpectedTypeForValue  = "unexpected type %v for value, expecting a bool, string or number"
	errFmtCannotGetValueForPath   = "could not get a value for path %v"
	errFmtUnexpectedWildcardUsage = "unexpected wildcard usage for field type %v"
	errFmtCannotExpandForArray    = "cannot expand wildcard for array with paths %v"
	errFmtCannotExpandForObject   = "cannot expand wildcard for object with paths %v"
)

// ValuesMatchingPaths returns values matching provided field paths in the input
// data. Field paths are dot separated strings where numbers representing
// indexes in arrays, strings representing key for maps and "*" will act as a
// wildcard mapping to each element of array or each key of map.
// See the unit tests for examples.
func ValuesMatchingPaths(data []byte, fieldPaths []string) (map[string][]byte, error) {
	vals := make(map[string][]byte)
	for _, fp := range fieldPaths {
		vs, err := valuesMatchingPath(data, fp)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot get values matching path: \"%s\"", fp)
		}
		for k, v := range vs {
			vals[k] = v
		}
	}
	return vals, nil
}

func valuesMatchingPath(data []byte, fieldPath string) (map[string][]byte, error) {
	keys, err := keysForFieldPath(data, fieldPath)
	if err != nil {
		return nil, errors.Wrap(err, errCannotExpandWildcards)
	}

	res := make(map[string][]byte, len(keys))
	for _, k := range keys {
		v, err := value(jsoniter.Get(data, k...))
		if err != nil {
			return nil, errors.Wrapf(err, errFmtCannotGetValueForPath, k)
		}
		if v == nil {
			continue
		}
		keyParts := make([]string, len(k))
		for i, kp := range k {
			switch vp := kp.(type) {
			case int:
				keyParts[i] = strconv.Itoa(vp)
			case string:
				keyParts[i] = vp
			default:
				return nil, errors.Errorf("unexpected type in fieldpath for path %v, expecting int or string", kp)
			}

		}
		res[strings.Join(keyParts, ".")] = v
	}
	return res, nil
}

func keysForFieldPath(data []byte, fieldPath string) ([][]interface{}, error) {
	d := jsoniter.Get(data)
	fps := strings.Split(fieldPath, ".")

	fpi := make([]interface{}, len(fps))
	for i, f := range fps {
		ix, err := strconv.Atoi(f)
		if err == nil {
			fpi[i] = ix
			continue
		}
		fpi[i] = f
	}
	return expandWildcards(d, fpi)
}

func expandWildcards(a jsoniter.Any, paths []interface{}) ([][]interface{}, error) { // nolint:gocyclo
	var res [][]interface{}

	for i, v := range paths {
		if v == "*" {
			b := a.Get(paths[:i]...)
			switch b.ValueType() {
			case jsoniter.ArrayValue:
				for j := 0; j < b.Size(); j++ {
					np := make([]interface{}, len(paths))
					copy(np, paths)
					np = append(append(np[:i], j), np[i+1:]...)
					r, err := expandWildcards(a, np)
					if err != nil {
						return nil, errors.Wrapf(err, errFmtCannotExpandForArray, np)
					}
					res = append(res, r...)
				}
			case jsoniter.ObjectValue:
				for _, k := range b.Keys() {
					np := make([]interface{}, len(paths))
					copy(np, paths)
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
	return append(res, paths), nil
}

func value(a jsoniter.Any) ([]byte, error) {
	switch a.ValueType() {
	case jsoniter.BoolValue:
		return []byte(strconv.FormatBool(a.ToBool())), nil
	case jsoniter.StringValue:
		return []byte(a.ToString()), nil
	case jsoniter.NumberValue:
		return []byte(strconv.Itoa(a.ToInt())), nil
	case jsoniter.NilValue, jsoniter.ArrayValue, jsoniter.ObjectValue:
		return nil, fmt.Errorf(errFmtUnexpectedTypeForValue, a.ValueType())
	case jsoniter.InvalidValue:
		// This means that there is no value for this key, so we return no data
		// and do not error since this is an expected case, e.g. a field is not
		// available in data.
		return nil, nil
	default:
		return nil, fmt.Errorf(errFmtUnexpectedTypeForValue, a.ValueType())
	}
}
