// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/config/conversion"
	"github.com/crossplane/upjet/pkg/resource"
)

const (
	errAlreadyRegistered = "conversion functions are already registered"
)

var instance *registry

// registry represents the conversion hook registry for a provider.
type registry struct {
	provider *config.Provider
}

// RegisterConversions registers the API version conversions from the specified
// provider configuration.
func RegisterConversions(provider *config.Provider) error {
	if instance != nil {
		return errors.New(errAlreadyRegistered)
	}
	instance = &registry{
		provider: provider,
	}
	return nil
}

// GetConversions returns the conversion.Conversions registered for the
// Terraformed resource.
func GetConversions(tr resource.Terraformed) []conversion.Conversion {
	t := tr.GetTerraformResourceType()
	if instance == nil || instance.provider == nil || instance.provider.Resources[t] == nil {
		return nil
	}
	return instance.provider.Resources[t].Conversions
}
