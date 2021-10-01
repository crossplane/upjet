package resource

import (
	"context"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"k8s.io/apimachinery/pkg/runtime"
)

// GetSensitiveParameters will collect sensitive information as terraform state
// attributes by following secret references in the spec.
func GetSensitiveParameters(ctx context.Context, client SecretClient, from runtime.Object, into map[string]interface{}, at map[string]string) error {
	pv, err := fieldpath.PaveObject(from)
	if err != nil {
		return err
	}

	tpv := fieldpath.Pave(into)
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
		tpv.SetString(k, string(val))
	}
	return nil
}
