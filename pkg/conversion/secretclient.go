package conversion

import (
	"context"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type APISecretClientOption func(*APISecretClient)

func WithDefaultNamespace(n string) APISecretClientOption {
	return func(a *APISecretClient) {
		a.defaultNamespace = n
	}
}

type APISecretClient struct {
	kube             client.Client
	defaultNamespace string
}

func NewAPISecretClient(k client.Client, opts ...APISecretClientOption) *APISecretClient {
	a := &APISecretClient{
		kube:             k,
		defaultNamespace: "crossplane-system",
	}

	for _, o := range opts {
		o(a)
	}

	return a
}

func (a *APISecretClient) GetSecretData(ctx context.Context, s xpv1.SecretReference) (map[string][]byte, error) {
	if s.Namespace == "" {
		s.Namespace = a.defaultNamespace
	}
	secret := &v1.Secret{}
	if err := a.kube.Get(ctx, types.NamespacedName{Namespace: s.Namespace, Name: s.Name}, secret); err != nil {
		return nil, err
	}
	return secret.Data, nil
}

func (a *APISecretClient) GetSecretValue(ctx context.Context, sel xpv1.SecretKeySelector) ([]byte, error) {
	if sel.Namespace == "" {
		sel.Namespace = a.defaultNamespace
	}
	d, err := a.GetSecretData(ctx, sel.SecretReference)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get secret data")
	}
	return d[sel.Key], err
}
