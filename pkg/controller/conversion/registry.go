// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/config/conversion"
	"github.com/crossplane/upjet/v2/pkg/resource"
)

const (
	errAlreadyRegistered = "conversion functions are already registered"
)

var instance *registry

// registry represents the conversion hook registry for a provider.
type registry struct {
	providerCluster    *config.Provider
	providerNamespaced *config.Provider
	scheme             *runtime.Scheme
	logger             logging.Logger
}

// RegistryOption sets an option for the conversion registry.
type RegistryOption func(*registry)

// WithLogger configures the logger to be used with the conversion registry.
func WithLogger(logger logging.Logger) RegistryOption {
	return func(r *registry) {
		r.logger = logger
	}
}

// RegisterConversions registers the API version conversions from the specified
// provider configuration with this registry.
func (r *registry) RegisterConversions(providerCluster, providerNamespaced *config.Provider) error {
	if r.providerCluster != nil || r.providerNamespaced != nil {
		return errors.New(errAlreadyRegistered)
	}
	r.providerCluster = providerCluster
	r.providerNamespaced = providerNamespaced
	return nil
}

// GetConversions returns the conversion.Conversions registered in this
// registry for the specified Terraformed resource.
func (r *registry) GetConversions(tr resource.Terraformed) []conversion.Conversion {
	t := tr.GetTerraformResourceType()

	p := r.providerCluster
	if tr.GetNamespace() != "" {
		p = r.providerNamespaced
	}

	if p == nil || p.Resources[t] == nil {
		return nil
	}

	return p.Resources[t].Conversions
}

// GetConversions returns the conversion.Conversions registered for the
// specified Terraformed resource.
func GetConversions(tr resource.Terraformed) []conversion.Conversion {
	return instance.GetConversions(tr)
}

// RegisterConversions registers the API version conversions from the specified
// provider configuration. The specified scheme should contain the registrations
// for the types whose versions are to be converted. If a registration for a
// Go schema is not found in the specified registry, RoundTrip does not error
// but only wildcard conversions must be used with the registry.
func RegisterConversions(providerCluster, providerNamespaced *config.Provider, scheme *runtime.Scheme, opts ...RegistryOption) error {
	if instance != nil {
		return errors.New(errAlreadyRegistered)
	}
	instance = &registry{
		scheme: scheme,
		logger: logging.NewNopLogger(),
	}
	for _, o := range opts {
		o(instance)
	}
	return instance.RegisterConversions(providerCluster, providerNamespaced)
}
