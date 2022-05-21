/*
Copyright 2022 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/terraform"
)

// Options contains incriminating options for a given Terrajet controller instance.
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
