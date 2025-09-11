// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/controller/handler"
	"github.com/crossplane/upjet/v2/pkg/metrics"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	tferrors "github.com/crossplane/upjet/v2/pkg/terraform/errors"
)

// TerraformPluginFrameworkAsyncConnector is a managed reconciler Connecter
// implementation for reconciling Terraform plugin framework based
// resources.
type TerraformPluginFrameworkAsyncConnector struct {
	*TerraformPluginFrameworkConnector
	callback     CallbackProvider
	eventHandler *handler.EventHandler
}

// TerraformPluginFrameworkAsyncOption represents a configuration option for
// a TerraformPluginFrameworkAsyncConnector object.
type TerraformPluginFrameworkAsyncOption func(connector *TerraformPluginFrameworkAsyncConnector)

func NewTerraformPluginFrameworkAsyncConnector(kube client.Client,
	ots *OperationTrackerStore,
	sf terraform.SetupFn,
	cfg *config.Resource,
	opts ...TerraformPluginFrameworkAsyncOption,
) *TerraformPluginFrameworkAsyncConnector {
	nfac := &TerraformPluginFrameworkAsyncConnector{
		TerraformPluginFrameworkConnector: NewTerraformPluginFrameworkConnector(kube, sf, cfg, ots),
	}
	for _, f := range opts {
		f(nfac)
	}
	return nfac
}

func (c *TerraformPluginFrameworkAsyncConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	ec, err := c.TerraformPluginFrameworkConnector.Connect(ctx, mg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot initialize the Terraform Plugin Framework async external client")
	}

	return &terraformPluginFrameworkAsyncExternalClient{
		terraformPluginFrameworkExternalClient: ec.(*terraformPluginFrameworkExternalClient),
		callback:                               c.callback,
		eventHandler:                           c.eventHandler,
	}, nil
}

// WithTerraformPluginFrameworkAsyncConnectorEventHandler configures the EventHandler so that
// the Terraform Plugin Framework external clients can requeue reconciliation requests.
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


	// When observing for the first time, just after an async create
	// operation has finished, we check if the provider
	// returns a partial state and try to extract the external name out of it.
	// Then return early without making any external API calls, and requeue.
	//
	// Some TF resources might return a partial state even if their creation
	// fails. The partial state potentially can include the non-deterministic
	// identifier(s) of the resource. We try to record it as early as possible.
	//
	// An example scenario that we try to prevent is:
	// - In the TF provider, resource involves multiple steps to create.
	// - MR reconciler, calls async Create, which invokes TF ApplyResource RPC.
	//   - External resource is created, but some steps fail for whatever reason
	//   - ApplyResource returns some error diags, but also a partial state
	//   - Partial state is saved to in-memory cache (in opTracker)
	//   - Async Create ends, triggering a reconcile
	// - MR reconciler observes the resource using the partial state as prior state
	//   - in the happy path, Reading the resource succeeds. 
	// - TF resource Read might return error diagnostics and an empty state
	//   even if they exist.
	// - We never get to record the external name, from the previous
	//   async create.
	//
	if lastErr := n.opTracker.LastOperation.Error(); lastErr != nil && n.opTracker.LastOperation.IsEnded() && n.opTracker.LastOperation.Type == "create"{
		if n.recoverExternalName(mg) {
			defer n.opTracker.LastOperation.Clear(true)
			n.logger.Debug("recovered external name from last failed async operation", "external-name", meta.GetExternalName(mg))
			// TODO(erhan): ideally, the external-name update should be handled
			// by a dedicated observation response at crossplane-runtime
			// managed reconciler.
			// We try to signal for an annotation update (not a status update)
			// then requeue again (which will implicitly happen).
			// so, we hackily return "late-initialized" to true.
			// This might not work if the late initialization management policy
			// is disabled.
			return managed.ExternalObservation{
				ResourceExists:          true,
				ResourceUpToDate:        true,
				ResourceLateInitialized: true,
			}, nil
		}
	}
	n.opTracker.LastOperation.Clear(true)

	o, err := n.terraformPluginFrameworkExternalClient.Observe(ctx, mg)
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

// panicHandler wraps an error, so that deferred functions that will
// be executed on a panic can access the error more conveniently.
type panicHandler struct {
	err error
}

// recoverIfPanic recovers from panics, if any. Upon recovery, the
// error is set to a recovery message. Otherwise, the error is left
// unmodified. Calls to this function should be defferred directly:
// `defer ph.recoverIfPanic()`. Panic recovery won't work if the call
// is wrapped in another function call, such as `defer func() {
// ph.recoverIfPanic() }()`. On recovery, API machinery panic handlers
// run. The implementation follows the outline of panic recovery
// mechanism in controller-runtime:
// https://github.com/kubernetes-sigs/controller-runtime/blob/v0.17.3/pkg/internal/controller/controller.go#L105-L112
func (ph *panicHandler) recoverIfPanic(ctx context.Context) {
	if r := recover(); r != nil {
		for _, fn := range utilruntime.PanicHandlers {
			fn(ctx, r)
		}

		ph.err = fmt.Errorf("recovered from panic: %v", r)
	}
}

func (n *terraformPluginFrameworkAsyncExternalClient) Create(_ context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) { //nolint:contextcheck // we intentionally use a fresh context for the async operation
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
			n.opTracker.logger.Debug("Async create ended.", "error", err)

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

		n.opTracker.logger.Debug("Async create starting...")
		_, ph.err = n.terraformPluginFrameworkExternalClient.Create(ctx, mg)
	}()

	return managed.ExternalCreation{}, n.opTracker.LastOperation.Error()
}

func (n *terraformPluginFrameworkAsyncExternalClient) Update(_ context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) { //nolint:contextcheck // we intentionally use a fresh context for the async operation
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
			n.opTracker.logger.Debug("Async update ended.", "error", err)

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

		n.opTracker.logger.Debug("Async update starting...")
		_, ph.err = n.terraformPluginFrameworkExternalClient.Update(ctx, mg)
	}()

	return managed.ExternalUpdate{}, n.opTracker.LastOperation.Error()
}

func (n *terraformPluginFrameworkAsyncExternalClient) Delete(_ context.Context, mg xpresource.Managed) (managed.ExternalDelete, error) { //nolint:contextcheck // we intentionally use a fresh context for the async operation
	switch {
	case n.opTracker.LastOperation.Type == "delete":
		n.opTracker.logger.Debug("The previous delete operation is still ongoing")
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
			n.opTracker.logger.Debug("Async delete ended.", "error", err)

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

		n.opTracker.logger.Debug("Async delete starting...")
		_, ph.err = n.terraformPluginFrameworkExternalClient.Delete(ctx, mg)
	}()

	return managed.ExternalDelete{}, n.opTracker.LastOperation.Error()
}

func (n *terraformPluginFrameworkAsyncExternalClient) Disconnect(_ context.Context) error {
	return nil
}
