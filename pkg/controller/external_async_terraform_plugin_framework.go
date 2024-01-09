// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/controller/handler"
	"github.com/crossplane/upjet/pkg/metrics"
	"github.com/crossplane/upjet/pkg/resource"
	"github.com/crossplane/upjet/pkg/terraform"
	tferrors "github.com/crossplane/upjet/pkg/terraform/errors"
)

type TerraformPluginFrameworkAsyncConnector struct {
	*TerraformPluginFrameworkConnector
	callback     CallbackProvider
	eventHandler *handler.EventHandler
}

type TerraformPluginFrameworkAsyncOption func(connector *TerraformPluginFrameworkAsyncConnector)

func NewTerraformPluginFrameworkAsyncConnector(kube client.Client,
	ots *OperationTrackerStore,
	sf terraform.SetupFn,
	cfg *config.Resource,
	provider provider.Provider,
	opts ...TerraformPluginFrameworkAsyncOption) *TerraformPluginFrameworkAsyncConnector {
	nfac := &TerraformPluginFrameworkAsyncConnector{
		TerraformPluginFrameworkConnector: NewTerraformPluginFrameworkConnector(kube, sf, cfg, ots, provider),
	}
	for _, f := range opts {
		f(nfac)
	}
	return nfac
}

func (c *TerraformPluginFrameworkAsyncConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	ec, err := c.TerraformPluginFrameworkConnector.Connect(ctx, mg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot initialize the no-fork async external client")
	}

	return &terraformPluginFrameworkAsyncExternalClient{
		terraformPluginFrameworkExternalClient: ec.(*terraformPluginFrameworkExternalClient),
		callback:                               c.callback,
		eventHandler:                           c.eventHandler,
	}, nil
}

// WithTerraformPluginFrameworkAsyncConnectorEventHandler configures the EventHandler so that
// the no-fork external clients can requeue reconciliation requests.
func WithTerraformPluginFrameworkAsyncConnectorEventHandler(e *handler.EventHandler) TerraformPluginFrameworkAsyncOption {
	return func(c *TerraformPluginFrameworkAsyncConnector) {
		c.eventHandler = e
	}
}

// WithTerraformPluginFrameworkAsyncCallbackProvider configures the controller to use async variant of the functions
// of the Terraform client and run given callbacks once those operations are
// completed.
func WithTerraformPluginFrameworkAsyncCallbackProvider(ac CallbackProvider) TerraformPluginFrameworkAsyncOption {
	return func(c *TerraformPluginFrameworkAsyncConnector) {
		c.callback = ac
	}
}

// WithTerraformPluginFrameworkAsyncLogger configures a logger for the TerraformPluginFrameworkAsyncConnector.
func WithTerraformPluginFrameworkAsyncLogger(l logging.Logger) TerraformPluginFrameworkAsyncOption {
	return func(c *TerraformPluginFrameworkAsyncConnector) {
		c.logger = l
	}
}

// WithTerraformPluginFrameworkAsyncMetricRecorder configures a metrics.MetricRecorder for the
// TerraformPluginFrameworkAsyncConnector.
func WithTerraformPluginFrameworkAsyncMetricRecorder(r *metrics.MetricRecorder) TerraformPluginFrameworkAsyncOption {
	return func(c *TerraformPluginFrameworkAsyncConnector) {
		c.metricRecorder = r
	}
}

// WithTerraformPluginFrameworkAsyncManagementPolicies configures whether the client should
// handle management policies.
func WithTerraformPluginFrameworkAsyncManagementPolicies(isManagementPoliciesEnabled bool) TerraformPluginFrameworkAsyncOption {
	return func(c *TerraformPluginFrameworkAsyncConnector) {
		c.isManagementPoliciesEnabled = isManagementPoliciesEnabled
	}
}

type terraformPluginFrameworkAsyncExternalClient struct {
	*terraformPluginFrameworkExternalClient
	callback     CallbackProvider
	eventHandler *handler.EventHandler
}

func (n *terraformPluginFrameworkAsyncExternalClient) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	if n.opTracker.LastOperation.IsRunning() {
		n.logger.WithValues("opType", n.opTracker.LastOperation.Type).Debug("ongoing async operation")
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}
	n.opTracker.LastOperation.Flush()

	o, err := n.terraformPluginFrameworkExternalClient.Observe(ctx, mg)
	// clear any previously reported LastAsyncOperation error condition here,
	// because there are no pending updates on the existing resource and it's
	// not scheduled to be deleted.
	if err == nil && o.ResourceExists && o.ResourceUpToDate && !meta.WasDeleted(mg) {
		mg.(resource.Terraformed).SetConditions(resource.LastAsyncOperationCondition(nil))
	}
	return o, err
}

func (n *terraformPluginFrameworkAsyncExternalClient) Create(_ context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	if !n.opTracker.LastOperation.MarkStart("create") {
		return managed.ExternalCreation{}, errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}

	ctx, cancel := context.WithDeadline(context.Background(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()

		n.opTracker.logger.Debug("Async create starting...")
		_, err := n.terraformPluginFrameworkExternalClient.Create(ctx, mg)
		err = tferrors.NewAsyncCreateFailed(err)
		n.opTracker.LastOperation.SetError(err)
		n.opTracker.logger.Debug("Async create ended.", "error", err)

		n.opTracker.LastOperation.MarkEnd()
		if cErr := n.callback.Create(mg.GetName())(err, ctx); cErr != nil {
			n.opTracker.logger.Info("Async create callback failed", "error", cErr.Error())
		}
	}()

	return managed.ExternalCreation{}, nil
}

func (n *terraformPluginFrameworkAsyncExternalClient) Update(_ context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	if !n.opTracker.LastOperation.MarkStart("update") {
		return managed.ExternalUpdate{}, errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}

	ctx, cancel := context.WithDeadline(context.Background(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()

		n.opTracker.logger.Debug("Async update starting...")
		_, err := n.terraformPluginFrameworkExternalClient.Update(ctx, mg)
		err = tferrors.NewAsyncUpdateFailed(err)
		n.opTracker.LastOperation.SetError(err)
		n.opTracker.logger.Debug("Async update ended.", "error", err)

		n.opTracker.LastOperation.MarkEnd()
		if cErr := n.callback.Update(mg.GetName())(err, ctx); cErr != nil {
			n.opTracker.logger.Info("Async update callback failed", "error", cErr.Error())
		}
	}()

	return managed.ExternalUpdate{}, nil
}

func (n *terraformPluginFrameworkAsyncExternalClient) Delete(_ context.Context, mg xpresource.Managed) error {
	switch {
	case n.opTracker.LastOperation.Type == "delete":
		n.opTracker.logger.Debug("The previous delete operation is still ongoing")
		return nil
	case !n.opTracker.LastOperation.MarkStart("delete"):
		return errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}

	ctx, cancel := context.WithDeadline(context.Background(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()

		n.opTracker.logger.Debug("Async delete starting...")
		err := tferrors.NewAsyncDeleteFailed(n.terraformPluginFrameworkExternalClient.Delete(ctx, mg))
		n.opTracker.LastOperation.SetError(err)
		n.opTracker.logger.Debug("Async delete ended.", "error", err)

		n.opTracker.LastOperation.MarkEnd()
		if cErr := n.callback.Destroy(mg.GetName())(err, ctx); cErr != nil {
			n.opTracker.logger.Info("Async delete callback failed", "error", cErr.Error())
		}
	}()

	return nil
}
