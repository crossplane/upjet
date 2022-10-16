/*
Copyright 2022 Upbound Inc.
*/

package controller

import (
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/terraform"
)

// Options contains incriminating options for a given Upjet controller instance.
type Options struct {
	controller.Options

	// Provider contains all resource configurations of the provider which can
	// be used to pick the related one. Since the selection is done in runtime,
	// we need to pass everything and generated code will pick the one.
	Provider *config.Provider

	// WorkspaceStore will be used to pick/initialize the workspace the specific CR
	// instance should use.
	WorkspaceStore *terraform.WorkspaceStore

	// SetupFn contains the provider-specific initialization logic, such as
	// preparing the auth token for Terraform CLI.
	SetupFn terraform.SetupFn

	// SecretStoreConfigGVK is the GroupVersionKind for the Secret StoreConfig
	// resource. Setting this enables External Secret Stores for the controller
	// by adding connection.DetailsManager as a ConnectionPublisher.
	SecretStoreConfigGVK *schema.GroupVersionKind
}
