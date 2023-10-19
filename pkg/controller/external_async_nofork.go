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

func (n *noForkAsyncExternal) Create(_ context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	if !n.opTracker.LastOperation.MarkStart("create") {
		return managed.ExternalCreation{}, errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}

	ctx, cancel := context.WithDeadline(context.Background(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()
		defer n.opTracker.LastOperation.MarkEnd()

		n.opTracker.logger.Debug("Async create starting...", "tfID", n.opTracker.GetTfID())
		_, err := n.noForkExternal.Create(ctx, mg)
		n.opTracker.LastOperation.SetError(errors.Wrap(err, "async create failed"))
		n.opTracker.logger.Debug("Async create ended.", "error", err, "tfID", n.opTracker.GetTfID())

		if cErr := n.callback.Create(mg.GetName())(err, ctx); cErr != nil {
			n.opTracker.logger.Info("Async create callback failed", "error", cErr.Error())
		}
	}()

	return managed.ExternalCreation{}, nil
}

func (n *noForkAsyncExternal) Update(_ context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	if !n.opTracker.LastOperation.MarkStart("update") {
		return managed.ExternalUpdate{}, errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}

	ctx, cancel := context.WithDeadline(context.Background(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()
		defer n.opTracker.LastOperation.MarkEnd()

		n.opTracker.logger.Debug("Async update starting...", "tfID", n.opTracker.GetTfID())
		_, err := n.noForkExternal.Update(ctx, mg)
		n.opTracker.LastOperation.SetError(errors.Wrap(err, "async update failed"))
		n.opTracker.logger.Debug("Async update ended.", "error", err, "tfID", n.opTracker.GetTfID())

		if cErr := n.callback.Update(mg.GetName())(err, ctx); cErr != nil {
			n.opTracker.logger.Info("Async update callback failed", "error", cErr.Error())
		}
	}()

	return managed.ExternalUpdate{}, nil
}

func (n *noForkAsyncExternal) Delete(ctx context.Context, mg xpresource.Managed) error {
	if !n.opTracker.LastOperation.MarkStart("delete") {
		return errors.Errorf("%s operation that started at %s is still running", n.opTracker.LastOperation.Type, n.opTracker.LastOperation.StartTime().String())
	}

	ctx, cancel := context.WithDeadline(context.Background(), n.opTracker.LastOperation.StartTime().Add(defaultAsyncTimeout))
	go func() {
		defer cancel()
		defer n.opTracker.LastOperation.MarkEnd()

		n.opTracker.logger.Debug("Async delete starting...", "tfID", n.opTracker.GetTfID())
		err := n.noForkExternal.Delete(ctx, mg)
		n.opTracker.LastOperation.SetError(errors.Wrap(err, "async delete failed"))
		n.opTracker.logger.Debug("Async delete ended.", "error", err, "tfID", n.opTracker.GetTfID())

		if cErr := n.callback.Destroy(mg.GetName())(err, ctx); cErr != nil {
			n.opTracker.logger.Info("Async delete callback failed", "error", cErr.Error())
		}
	}()

	return nil
}
