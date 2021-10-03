package resource

import (
	"context"

	"github.com/pkg/errors"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	errCannotExpandWildcards = "cannot expand wildcards"

	errFmtCannotGetStringForFieldPath  = "cannot not get a string for fieldpath %q"
	errFmtCannotGetStringsForFieldPath = "cannot get strings matching field fieldpath: %q"
)

// GetConnectionDetails returns strings matching provided field paths in the
// input data.
// See the unit tests for examples.
func GetConnectionDetails(from map[string]interface{}, at map[string]string) (map[string][]byte, error) {
	vals := make(map[string][]byte)
	for tf := range at {
		vs, err := stringsMatchingFieldPath(from, tf)
		if err != nil {
			return nil, errors.Wrapf(err, errFmtCannotGetStringsForFieldPath, tf)
		}
		for k, v := range vs {
			vals[k] = v
		}
	}
	return vals, nil
}

func stringsMatchingFieldPath(from map[string]interface{}, path string) (map[string][]byte, error) {
	paved := fieldpath.Pave(from)
	segments, err := paved.ExpandWildcards(path)
	if err != nil {
		return nil, errors.Wrap(err, errCannotExpandWildcards)
	}

	res := make(map[string][]byte, len(segments))
	for _, s := range segments {
		v, err := paved.GetString(s)
		if err != nil {
			return nil, errors.Wrapf(err, errFmtCannotGetStringForFieldPath, s)
		}
		res[s] = []byte(v)
	}
	return res, nil
}

// GetSensitiveParameters will collect sensitive information as terraform state
// attributes by following secret references in the spec.
func GetSensitiveParameters(ctx context.Context, client SecretClient, from runtime.Object, into map[string]interface{}, at map[string]string) error {
	pv, err := fieldpath.PaveObject(from)
	if err != nil {
		return err
	}
	//paveXP, err := pv.GetValue("spec.forProvider")
	if err != nil {
		return err
	}
	paveTF := fieldpath.Pave(into)

	/*	for tf, xp := range at {

		}*/

	for k, v := range at {
		sel := v1.SecretKeySelector{}
		sel.Name, err = pv.GetString("spec.forProvider." + v + ".name")
		if err != nil {
			return err
		}
		sel.Key, err = pv.GetString("spec.forProvider." + v + ".key")
		if err != nil {
			return err
		}
		val, err := client.GetSecretValue(ctx, sel)
		if err != nil {
			return err
		}
		paveTF.SetString(k, string(val))
	}
	return nil
}

// GetSensitiveObservation will return sensitive information as terraform state
// attributes by reading them from connection details.
func GetSensitiveObservation(from runtime.Object, into map[string]interface{}, at map[string]string) error {
	_, err := fieldpath.PaveObject(from)
	if err != nil {
		return err
	}
	return nil
}
