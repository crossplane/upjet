// Copyright 2023 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/controller/handler"
	"github.com/upbound/upjet/pkg/metrics"
	"github.com/upbound/upjet/pkg/terraform"
)

var defaultAsyncTimeout = 1 * time.Hour

type NoForkAsyncConnector struct {
	*NoForkConnector
	callback     CallbackProvider
	eventHandler *handler.EventHandler
}

type NoForkAsyncOption func(connector *NoForkAsyncConnector)

func NewNoForkAsyncConnector(kube client.Client, ots *OperationTrackerStore, sf terraform.SetupFn, cfg *config.Resource, opts ...NoForkAsyncOption) *NoForkAsyncConnector {
	nfac := &NoForkAsyncConnector{
		NoForkConnector: NewNoForkConnector(kube, sf, cfg, ots),
	}
	for _, f := range opts {
		f(nfac)
	}
	return nfac
}

func (c *NoForkAsyncConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	ec, err := c.NoForkConnector.Connect(ctx, mg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot initialize the no-fork async external client")
	}

	return &noForkAsyncExternal{
		noForkExternal: ec.(*noForkExternal),
		callback:       c.callback,
		eventHandler:   c.eventHandler,
	}, nil
}

// WithNoForkAsyncConnectorEventHandler configures the EventHandler so that
// the no-fork external clients can requeue reconciliation requests.
func WithNoForkAsyncConnectorEventHandler(e *handler.EventHandler) NoForkAsyncOption {
	return func(c *NoForkAsyncConnector) {
		c.eventHandler = e
	}
}

// WithNoForkAsyncCallbackProvider configures the controller to use async variant of the functions
// of the Terraform client and run given callbacks once those operations are
// completed.
func WithNoForkAsyncCallbackProvider(ac CallbackProvider) NoForkAsyncOption {
	return func(c *NoForkAsyncConnector) {
		c.callback = ac
	}
}

// WithNoForkAsyncLogger configures a logger for the NoForkAsyncConnector.
func WithNoForkAsyncLogger(l logging.Logger) NoForkAsyncOption {
	return func(c *NoForkAsyncConnector) {
		c.logger = l
	}
}

// WithNoForkAsyncMetricRecorder configures a metrics.MetricRecorder for the
// NoForkAsyncConnector.
func WithNoForkAsyncMetricRecorder(r *metrics.MetricRecorder) NoForkAsyncOption {
	return func(c *NoForkAsyncConnector) {
		c.metricRecorder = r
	}
}

type noForkAsyncExternal struct {
	*noForkExternal
	callback     CallbackProvider
	eventHandler *handler.EventHandler
}

type CallbackFn func(error, context.Context) error

func (n *noForkAsyncExternal) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	if n.opTracker.LastOperation.IsRunning() {
		n.logger.WithValues("opType", n.opTracker.LastOperation.Type).Debug("ongoing async operation")
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}
	n.opTracker.LastOperation.Flush()

	return n.noForkExternal.Observe(ctx, mg)
}

func (n *noForkAsyncExternal) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	if !n.opTracker.LastOperation.MarkStart("create") {
		return managed.ExternalCreation{}, errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}
	instanceDiff, err := n.getResourceDataDiff(ctx, n.opTracker.GetTfState())
	if err != nil {
		return managed.ExternalCreation{}, err
	}

	ctx, cancel := context.WithDeadline(context.TODO(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()
		defer n.opTracker.LastOperation.MarkEnd()
		start := time.Now()
		newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), instanceDiff, n.ts.Meta)
		metrics.ExternalAPITime.WithLabelValues("create_async").Observe(time.Since(start).Seconds())
		var tfErr error
		if diag != nil && diag.HasError() {
			tfErr = errors.Errorf("failed to create the resource: %v", diag)
			n.opTracker.LastOperation.SetError(tfErr)
		}
		n.opTracker.SetTfState(newState)
		n.opTracker.logger.Debug("create async ended", "tfID", n.opTracker.GetTfID())

		defer func() {
			if cErr := n.callback.Create(mg.GetName())(tfErr, ctx); cErr != nil {
				n.opTracker.logger.Info("create callback failed", "error", cErr.Error())
			}
		}()
	}()
	return managed.ExternalCreation{}, nil
}

func (n *noForkAsyncExternal) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	if !n.opTracker.LastOperation.MarkStart("update") {
		return managed.ExternalUpdate{}, errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}
	instanceDiff, err := n.getResourceDataDiff(ctx, n.opTracker.GetTfState())
	if err != nil {
		return managed.ExternalUpdate{}, err
	}
	ctx, cancel := context.WithDeadline(context.TODO(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()
		defer n.opTracker.LastOperation.MarkEnd()
		start := time.Now()
		newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), instanceDiff, n.ts.Meta)
		metrics.ExternalAPITime.WithLabelValues("update_async").Observe(time.Since(start).Seconds())
		var tfErr error
		if diag != nil && diag.HasError() {
			tfErr = errors.Errorf("failed to update the resource: %v", diag)
			n.opTracker.LastOperation.SetError(tfErr)
		}
		n.opTracker.SetTfState(newState)
		n.opTracker.logger.Debug("update async ended", "tfID", n.opTracker.GetTfID())

		defer func() {
			if cErr := n.callback.Update(mg.GetName())(tfErr, ctx); cErr != nil {
				n.opTracker.logger.Info("update callback failed", "error", cErr.Error())
			}
		}()
	}()

	return managed.ExternalUpdate{}, nil
}

func (n *noForkAsyncExternal) Delete(ctx context.Context, mg xpresource.Managed) error {
	if !n.opTracker.LastOperation.MarkStart("destroy") {
		return errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}
	instanceDiff, err := n.getResourceDataDiff(ctx, n.opTracker.GetTfState())
	if err != nil {
		return err
	}
	if instanceDiff == nil {
		instanceDiff = tf.NewInstanceDiff()
	}

	instanceDiff.Destroy = true
	ctx, cancel := context.WithDeadline(context.TODO(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()
		defer n.opTracker.LastOperation.MarkEnd()
		start := time.Now()
		tfID := n.opTracker.GetTfID()
		newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), instanceDiff, n.ts.Meta)
		metrics.ExternalAPITime.WithLabelValues("destroy_async").Observe(time.Since(start).Seconds())
		var tfErr error
		if diag != nil && diag.HasError() {
			tfErr = errors.Errorf("failed to destroy the resource: %v", diag)
			n.opTracker.LastOperation.SetError(tfErr)
		}
		n.opTracker.SetTfState(newState)
		n.opTracker.logger.Debug("destroy async ended", "tfID", tfID)
		defer func() {
			if cErr := n.callback.Destroy(mg.GetName())(tfErr, ctx); cErr != nil {
				n.opTracker.logger.Info("destroy callback failed", "error", cErr.Error())
			}
		}()
	}()
	return nil
}
