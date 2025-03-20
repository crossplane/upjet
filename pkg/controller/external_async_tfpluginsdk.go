// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/controller/handler"
	"github.com/crossplane/upjet/pkg/metrics"
	"github.com/crossplane/upjet/pkg/resource"
	"github.com/crossplane/upjet/pkg/terraform"
	tferrors "github.com/crossplane/upjet/pkg/terraform/errors"
)

var defaultAsyncTimeout = 1 * time.Hour

// TerraformPluginSDKAsyncConnector is a managed reconciler Connecter
// implementation for reconciling Terraform plugin SDK v2 based
// resources.
type TerraformPluginSDKAsyncConnector struct {
	*TerraformPluginSDKConnector
	callback     CallbackProvider
	eventHandler *handler.EventHandler
}

// TerraformPluginSDKAsyncOption represents a configuration option for
// a TerraformPluginSDKAsyncConnector object.
type TerraformPluginSDKAsyncOption func(connector *TerraformPluginSDKAsyncConnector)

// NewTerraformPluginSDKAsyncConnector initializes a new
// TerraformPluginSDKAsyncConnector.
func NewTerraformPluginSDKAsyncConnector(kube client.Client, ots *OperationTrackerStore, sf terraform.SetupFn, cfg *config.Resource, opts ...TerraformPluginSDKAsyncOption) *TerraformPluginSDKAsyncConnector {
	nfac := &TerraformPluginSDKAsyncConnector{
		TerraformPluginSDKConnector: NewTerraformPluginSDKConnector(kube, sf, cfg, ots),
	}
	for _, f := range opts {
		f(nfac)
	}
	return nfac
}

func (c *TerraformPluginSDKAsyncConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	ec, err := c.TerraformPluginSDKConnector.Connect(ctx, mg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot initialize the Terraform plugin SDK async external client")
	}

	return &terraformPluginSDKAsyncExternal{
		terraformPluginSDKExternal: ec.(*terraformPluginSDKExternal),
		callback:                   c.callback,
		eventHandler:               c.eventHandler,
	}, nil
}

// WithTerraformPluginSDKAsyncConnectorEventHandler configures the
// EventHandler so that the Terraform plugin SDK external clients can requeue
// reconciliation requests.
func WithTerraformPluginSDKAsyncConnectorEventHandler(e *handler.EventHandler) TerraformPluginSDKAsyncOption {
	return func(c *TerraformPluginSDKAsyncConnector) {
		c.eventHandler = e
	}
}

// WithTerraformPluginSDKAsyncCallbackProvider configures the controller to use
// async variant of the functions of the Terraform client and run given
// callbacks once those operations are completed.
func WithTerraformPluginSDKAsyncCallbackProvider(ac CallbackProvider) TerraformPluginSDKAsyncOption {
	return func(c *TerraformPluginSDKAsyncConnector) {
		c.callback = ac
	}
}

// WithTerraformPluginSDKAsyncLogger configures a logger for the
// TerraformPluginSDKAsyncConnector.
func WithTerraformPluginSDKAsyncLogger(l logging.Logger) TerraformPluginSDKAsyncOption {
	return func(c *TerraformPluginSDKAsyncConnector) {
		c.logger = l
	}
}

// WithTerraformPluginSDKAsyncMetricRecorder configures a
// metrics.MetricRecorder for the TerraformPluginSDKAsyncConnector.
func WithTerraformPluginSDKAsyncMetricRecorder(r *metrics.MetricRecorder) TerraformPluginSDKAsyncOption {
	return func(c *TerraformPluginSDKAsyncConnector) {
		c.metricRecorder = r
	}
}

// WithTerraformPluginSDKAsyncManagementPolicies configures whether the client
// should handle management policies.
func WithTerraformPluginSDKAsyncManagementPolicies(isManagementPoliciesEnabled bool) TerraformPluginSDKAsyncOption {
	return func(c *TerraformPluginSDKAsyncConnector) {
		c.isManagementPoliciesEnabled = isManagementPoliciesEnabled
	}
}

type terraformPluginSDKAsyncExternal struct {
	*terraformPluginSDKExternal
	callback     CallbackProvider
	eventHandler *handler.EventHandler
}

type CallbackFn func(error, context.Context) error

func (n *terraformPluginSDKAsyncExternal) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	if n.opTracker.LastOperation.IsRunning() {
		n.logger.WithValues("opType", n.opTracker.LastOperation.Type).Debug("ongoing async operation")
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}
	n.opTracker.LastOperation.Clear(true)

	o, err := n.terraformPluginSDKExternal.Observe(ctx, mg)
	// clear any previously reported LastAsyncOperation error condition here,
	// because there are no pending updates on the existing resource and it's
	// not scheduled to be deleted.
	if err == nil && o.ResourceExists && o.ResourceUpToDate && !meta.WasDeleted(mg) {
		mg.(resource.Terraformed).SetConditions(resource.LastAsyncOperationCondition(nil))
		mg.(resource.Terraformed).SetConditions(xpv1.ReconcileSuccess())
		n.opTracker.LastOperation.Clear(false)
	}
	return o, err
}

func (n *terraformPluginSDKAsyncExternal) Create(_ context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) { //nolint:contextcheck // we intentionally use a fresh context for the async operation
	if !n.opTracker.LastOperation.MarkStart("create") {
		return managed.ExternalCreation{}, errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}

	ctx, cancel := context.WithDeadline(context.Background(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		// The order of deferred functions, executed last-in-first-out, is
		// significant. The context should be canceled last, because it is
		// used by the finishing operations. Panic recovery should execute
		// first, because the finishing operations report the panic error,
		// if any.
		var ph panicHandler
		defer cancel()
		defer func() { // Finishing operations
			err := tferrors.NewAsyncCreateFailed(ph.err)
			n.opTracker.LastOperation.SetError(err)
			n.opTracker.logger.Debug("Async create ended.", "error", err, "tfID", n.opTracker.GetTfID())

			n.opTracker.LastOperation.MarkEnd()
			name := types.NamespacedName{
				Namespace: mg.GetNamespace(),
				Name:      mg.GetName(),
			}
			if cErr := n.callback.Create(name)(err, ctx); cErr != nil {
				n.opTracker.logger.Info("Async create callback failed", "error", cErr.Error())
			}
		}()
		defer ph.recoverIfPanic(ctx)

		n.opTracker.logger.Debug("Async create starting...", "tfID", n.opTracker.GetTfID())
		_, ph.err = n.terraformPluginSDKExternal.Create(ctx, mg)
	}()

	return managed.ExternalCreation{}, n.opTracker.LastOperation.Error()
}

func (n *terraformPluginSDKAsyncExternal) Update(_ context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) { //nolint:contextcheck // we intentionally use a fresh context for the async operation
	if !n.opTracker.LastOperation.MarkStart("update") {
		return managed.ExternalUpdate{}, errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}

	ctx, cancel := context.WithDeadline(context.Background(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		// The order of deferred functions, executed last-in-first-out, is
		// significant. The context should be canceled last, because it is
		// used by the finishing operations. Panic recovery should execute
		// first, because the finishing operations report the panic error,
		// if any.
		var ph panicHandler
		defer cancel()
		defer func() { // Finishing operations
			err := tferrors.NewAsyncUpdateFailed(ph.err)
			n.opTracker.LastOperation.SetError(err)
			n.opTracker.logger.Debug("Async update ended.", "error", err, "tfID", n.opTracker.GetTfID())

			n.opTracker.LastOperation.MarkEnd()
			name := types.NamespacedName{
				Namespace: mg.GetNamespace(),
				Name:      mg.GetName(),
			}
			if cErr := n.callback.Update(name)(err, ctx); cErr != nil {
				n.opTracker.logger.Info("Async update callback failed", "error", cErr.Error())
			}
		}()
		defer ph.recoverIfPanic(ctx)

		n.opTracker.logger.Debug("Async update starting...", "tfID", n.opTracker.GetTfID())
		_, ph.err = n.terraformPluginSDKExternal.Update(ctx, mg)
	}()

	return managed.ExternalUpdate{}, n.opTracker.LastOperation.Error()
}

func (n *terraformPluginSDKAsyncExternal) Delete(_ context.Context, mg xpresource.Managed) (managed.ExternalDelete, error) { //nolint:contextcheck // we intentionally use a fresh context for the async operation
	switch {
	case n.opTracker.LastOperation.Type == "delete":
		n.opTracker.logger.Debug("The previous delete operation is still ongoing", "tfID", n.opTracker.GetTfID())
		return managed.ExternalDelete{}, nil
	case !n.opTracker.LastOperation.MarkStart("delete"):
		return managed.ExternalDelete{}, errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}

	ctx, cancel := context.WithDeadline(context.Background(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		// The order of deferred functions, executed last-in-first-out, is
		// significant. The context should be canceled last, because it is
		// used by the finishing operations. Panic recovery should execute
		// first, because the finishing operations report the panic error,
		// if any.
		var ph panicHandler
		defer cancel()
		defer func() { // Finishing operations
			err := tferrors.NewAsyncDeleteFailed(ph.err)
			n.opTracker.LastOperation.SetError(err)
			n.opTracker.logger.Debug("Async delete ended.", "error", err, "tfID", n.opTracker.GetTfID())

			n.opTracker.LastOperation.MarkEnd()
			name := types.NamespacedName{
				Namespace: mg.GetNamespace(),
				Name:      mg.GetName(),
			}
			if cErr := n.callback.Destroy(name)(err, ctx); cErr != nil {
				n.opTracker.logger.Info("Async delete callback failed", "error", cErr.Error())
			}
		}()
		defer ph.recoverIfPanic(ctx)

		n.opTracker.logger.Debug("Async delete starting...", "tfID", n.opTracker.GetTfID())
		_, ph.err = n.terraformPluginSDKExternal.Delete(ctx, mg)
	}()

	return managed.ExternalDelete{}, n.opTracker.LastOperation.Error()
}

func (n *terraformPluginSDKAsyncExternal) Disconnect(_ context.Context) error {
	return nil
}
