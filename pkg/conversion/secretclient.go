package conversion

import (
	"context"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// APISecretClient is a client for getting k8s secrets
type APISecretClient struct {
	kube client.Client
}

// NewAPISecretClient builds and returns a new APISecretClient
func NewAPISecretClient(k client.Client) *APISecretClient {
	return &APISecretClient{kube: k}
}

// GetSecretData gets and returns data for the referenced secret
func (a *APISecretClient) GetSecretData(ctx context.Context, ref xpv1.SecretReference) (map[string][]byte, error) {
	secret := &v1.Secret{}
	if err := a.kube.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, secret); err != nil {
		return nil, err
	}
	return secret.Data, nil
}

// GetSecretValue gets and returns value for key of the referenced secret
func (a *APISecretClient) GetSecretValue(ctx context.Context, sel xpv1.SecretKeySelector) ([]byte, error) {
	d, err := a.GetSecretData(ctx, sel.SecretReference)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get secret data")
	}
	return d[sel.Key], err
}
