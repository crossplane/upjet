// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/controller"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/terraform"
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

	OperationTrackerStore *OperationTrackerStore

	// SetupFn contains the provider-specific initialization logic, such as
	// preparing the auth token for Terraform CLI.
	SetupFn terraform.SetupFn

	// PollJitter adds the specified jitter to the configured reconcile period
	// of the up-to-date resources in managed.Reconciler.
	PollJitter time.Duration

	// StartWebhooks enables starting of the conversion webhooks by the
	// provider's controllerruntime.Manager.
	StartWebhooks bool
}
