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
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/controller/handler"
	"github.com/upbound/upjet/pkg/resource/json"
	"github.com/upbound/upjet/pkg/terraform"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/pkg/errors"
	"github.com/upbound/upjet/pkg/metrics"
	"github.com/upbound/upjet/pkg/resource"
	corev1 "k8s.io/api/core/v1"
)

var defaultAsyncTimeout = 1 * time.Hour

type NoForkAsyncConnector struct {
	*NoForkConnector
	operationTrackerStore *OperationTrackerStore
	callback              CallbackProvider
	eventHandler          *handler.EventHandler
}

type NoForkAsyncOption func(connector *NoForkAsyncConnector)

func NewNoForkAsyncConnector(kube client.Client, ots *OperationTrackerStore, sf terraform.SetupFn, cfg *config.Resource, opts ...NoForkAsyncOption) *NoForkAsyncConnector {
	nfac := &NoForkAsyncConnector{
		NoForkConnector: &NoForkConnector{
			kube:              kube,
			getTerraformSetup: sf,
			config:            cfg,
		},
		operationTrackerStore: ots,
	}
	for _, f := range opts {
		f(nfac)
	}
	return nfac
}

func (c *NoForkAsyncConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	asyncTracker := c.operationTrackerStore.Tracker(mg.(resource.Terraformed))
	c.metricRecorder.ObserveReconcileDelay(mg.GetObjectKind().GroupVersionKind(), mg.GetName())
	start := time.Now()
	ts, err := c.getTerraformSetup(ctx, c.kube, mg)
	metrics.ExternalAPITime.WithLabelValues("connect").Observe(time.Since(start).Seconds())
	if err != nil {
		return nil, errors.Wrap(err, errGetTerraformSetup)
	}

	// To Compute the ResourceDiff: n.resourceSchema.Diff(...)
	tr := mg.(resource.Terraformed)
	params, err := tr.GetParameters()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get parameters")
	}
	if err = resource.GetSensitiveParameters(ctx, &APISecretClient{kube: c.kube}, tr, params, tr.GetConnectionDetailsMapping()); err != nil {
		return nil, errors.Wrap(err, "cannot store sensitive parameters into params")
	}

	externalName := meta.GetExternalName(tr)
	if externalName == "" {
		externalName = asyncTracker.GetTfID()
	}

	c.config.ExternalName.SetIdentifierArgumentFn(params, externalName)

	tfID, err := c.config.ExternalName.GetIDFn(ctx, externalName, params, ts.Map())
	if err != nil {
		return nil, errors.Wrap(err, "cannot get ID")
	}
	params["id"] = tfID
	// we need to parameterize the following for a provider
	// not all providers may have this attribute
	// TODO: tags_all handling
	attrs := c.config.TerraformResource.CoreConfigSchema().Attributes
	if _, ok := attrs["tags_all"]; ok {
		params["tags_all"] = params["tags"]
	}
	// construct TF
	if asyncTracker.GetTfState() == nil || asyncTracker.GetTfState().Attributes == nil {
		tfState, err := tr.GetObservation()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get the observation")
		}
		copyParams := len(tfState) == 0
		if err = resource.GetSensitiveParameters(ctx, &APISecretClient{kube: c.kube}, tr, tfState, tr.GetConnectionDetailsMapping()); err != nil {
			return nil, errors.Wrap(err, "cannot store sensitive parameters into tfState")
		}
		c.config.ExternalName.SetIdentifierArgumentFn(tfState, meta.GetExternalName(tr))
		tfState["id"] = tfID
		if copyParams {
			tfState = copyParameters(tfState, params)
		}

		tfStateCtyValue, err := schema.JSONMapToStateValue(tfState, c.config.TerraformResource.CoreConfigSchema())
		if err != nil {
			return nil, errors.Wrap(err, "cannot convert JSON map to state cty.Value")
		}
		s, err := c.config.TerraformResource.ShimInstanceStateFromValue(tfStateCtyValue)
		if err != nil {
			return nil, errors.Wrap(err, "failed to convert cty.Value to terraform.InstanceState")
		}
		s.RawPlan = tfStateCtyValue
		asyncTracker.SetTfState(s)
	}

	return &noForkAsyncExternal{
		noForkExternal: &noForkExternal{
			ts:             ts,
			resourceSchema: c.config.TerraformResource,
			config:         c.config,
			kube:           c.kube,
			params:         params,
			logger:         c.logger.WithValues("uid", mg.GetUID(), "name", mg.GetName(), "gvk", mg.GetObjectKind().GroupVersionKind().String()),
			metricRecorder: c.metricRecorder,
		},
		opTracker:    asyncTracker,
		callback:     c.callback,
		eventHandler: c.eventHandler,
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
	opTracker    *AsyncTracker
}

type CallbackFn func(error, context.Context) error

func (n *noForkAsyncExternal) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	if n.opTracker.LastOperation.IsRunning() {
		n.logger.WithValues("opType", n.opTracker.LastOperation.Type).Debug("ongoing async operation")
		mg.SetConditions(resource.AsyncOperationOngoingCondition())
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}
	n.opTracker.LastOperation.Flush()

	start := time.Now()
	newState, diag := n.resourceSchema.RefreshWithoutUpgrade(ctx, n.opTracker.GetTfState(), n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("read").Observe(time.Since(start).Seconds())
	if diag != nil && diag.HasError() {
		return managed.ExternalObservation{}, errors.Errorf("failed to observe the resource: %v", diag)
	}
	n.opTracker.SetTfState(newState)
	noDiff := false
	var connDetails managed.ConnectionDetails
	resourceExists := newState != nil && newState.ID != ""
	if !resourceExists && mg.GetDeletionTimestamp() != nil {
		gvk := mg.GetObjectKind().GroupVersionKind()
		metrics.DeletionTime.WithLabelValues(gvk.Group, gvk.Version, gvk.Kind).Observe(time.Since(mg.GetDeletionTimestamp().Time).Seconds())
	}
	lateInitialized := false
	if resourceExists {
		if mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionUnknown ||
			mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionFalse {
			addTTR(mg)
		}
		// Set external name
		en, err := n.config.ExternalName.GetExternalNameFn(map[string]any{
			"id": n.opTracker.GetTfID(),
		})
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrapf(err, "failed to get the external-name from ID: %s", n.opTracker.GetTfID())
		}
		// if external name is set for the first time or if it has changed, this is a spec update
		// therefore managed reconciler needs to be informed to trigger a spec update
		externalNameChanged := en != "" && mg.GetAnnotations()[meta.AnnotationKeyExternalName] != en
		meta.SetExternalName(mg, en)
		mg.SetConditions(xpv1.Available())
		stateValueMap, err := n.fromInstanceStateToJSONMap(newState)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot convert instance state to JSON map")
		}
		buff, err := json.TFParser.Marshal(stateValueMap)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot marshal the attributes of the new state for late-initialization")
		}
		lateInitialized, err = mg.(resource.Terraformed).LateInitialize(buff)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot late-initialize the managed resource")
		}
		// external name updates are considered as lateInitialized
		lateInitialized = lateInitialized || externalNameChanged
		err = mg.(resource.Terraformed).SetObservation(stateValueMap)
		if err != nil {
			return managed.ExternalObservation{}, errors.Errorf("could not set observation: %v", err)
		}
		connDetails, err = resource.GetConnectionDetails(stateValueMap, mg.(resource.Terraformed), n.config)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot get connection details")
		}
		instanceDiff, err := n.getResourceDataDiff(ctx, n.opTracker.GetTfState())
		if err != nil {
			return managed.ExternalObservation{}, err
		}
		noDiff = instanceDiff.Empty()

		if noDiff {
			n.metricRecorder.SetReconcileTime(mg.GetName())
		}
		if !lateInitialized {
			resource.SetUpToDateCondition(mg, noDiff)
		}
	}

	return managed.ExternalObservation{
		ResourceExists:          resourceExists,
		ResourceUpToDate:        noDiff,
		ConnectionDetails:       connDetails,
		ResourceLateInitialized: lateInitialized,
	}, nil

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
