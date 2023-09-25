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
	"github.com/upbound/upjet/pkg/controller/handler"
	"time"

	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	corev1 "k8s.io/api/core/v1"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/metrics"
	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/terraform"
)

type NoForkConnector struct {
	getTerraformSetup terraform.SetupFn
	kube              client.Client
	config            *config.Resource
	logger            logging.Logger
	eventHandler      *handler.EventHandler
	metricRecorder    *metrics.MetricRecorder
}

// NoForkOption allows you to configure NoForkConnector.
type NoForkOption func(connector *NoForkConnector)

// WithNoForkLogger configures a logger for the NoForkConnector.
func WithNoForkLogger(l logging.Logger) NoForkOption {
	return func(c *NoForkConnector) {
		c.logger = l
	}
}

// WithNoForkMetricRecorder configures a metrics.MetricRecorder for the
// NoForkConnector.
func WithNoForkMetricRecorder(r *metrics.MetricRecorder) NoForkOption {
	return func(c *NoForkConnector) {
		c.metricRecorder = r
	}
}

// WithNoForkConnectorEventHandler configures the EventHandler so that
// the no-fork external clients can requeue reconciliation requests.
func WithNoForkConnectorEventHandler(e *handler.EventHandler) NoForkOption {
	return func(c *NoForkConnector) {
		c.eventHandler = e
	}
}

func NewNoForkConnector(kube client.Client, sf terraform.SetupFn, cfg *config.Resource, opts ...NoForkOption) *NoForkConnector {
	nfc := &NoForkConnector{
		kube:              kube,
		getTerraformSetup: sf,
		config:            cfg,
	}
	for _, f := range opts {
		f(nfc)
	}
	return nfc
}

func copyParameters(tfState, params map[string]any) map[string]any {
	targetState := make(map[string]any, len(params))
	for k, v := range params {
		targetState[k] = v
	}
	for k, v := range tfState {
		targetState[k] = v
	}
	return targetState
}

func (c *NoForkConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
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
	c.config.ExternalName.SetIdentifierArgumentFn(params, meta.GetExternalName(tr))

	tfID, err := c.config.ExternalName.GetIDFn(ctx, meta.GetExternalName(mg), params, ts.Map())
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
		return nil, err
	}
	s, err := c.config.TerraformResource.ShimInstanceStateFromValue(tfStateCtyValue)

	if err != nil {
		return nil, errors.Wrap(err, "failed to convert cty.Value to terraform.InstanceState")
	}

	return &noForkExternal{
		ts:             ts,
		resourceSchema: c.config.TerraformResource,
		config:         c.config,
		kube:           c.kube,
		instanceState:  s,
		params:         params,
		logger:         c.logger.WithValues("uid", mg.GetUID(), "name", mg.GetName(), "gvk", mg.GetObjectKind().GroupVersionKind().String()),
		metricRecorder: c.metricRecorder,
	}, nil
}

type noForkExternal struct {
	ts             terraform.Setup
	resourceSchema *schema.Resource
	config         *config.Resource
	kube           client.Client
	instanceState  *tf.InstanceState
	params         map[string]any
	logger         logging.Logger
	metricRecorder *metrics.MetricRecorder
}

func (n *noForkExternal) getResourceDataDiff(ctx context.Context, s *tf.InstanceState) (*tf.InstanceDiff, error) {
	instanceDiff, err := schema.InternalMap(n.resourceSchema.Schema).Diff(ctx, s, &tf.ResourceConfig{
		Raw:    n.params,
		Config: n.params,
	}, nil, n.ts.Meta, false)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get *terraform.InstanceDiff")
	}

	return instanceDiff, nil
}

func (n *noForkExternal) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	start := time.Now()
	newState, diag := n.resourceSchema.RefreshWithoutUpgrade(ctx, n.instanceState, n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("read").Observe(time.Since(start).Seconds())
	if diag != nil && diag.HasError() {
		return managed.ExternalObservation{}, errors.Errorf("failed to observe the resource: %v", diag)
	}
	n.instanceState = newState
	noDiff := false
	var connDetails managed.ConnectionDetails
	resourceExists := newState != nil && newState.ID != ""
	if !resourceExists && mg.GetDeletionTimestamp() != nil {
		gvk := mg.GetObjectKind().GroupVersionKind()
		metrics.DeletionTime.WithLabelValues(gvk.Group, gvk.Version, gvk.Kind).Observe(time.Since(mg.GetDeletionTimestamp().Time).Seconds())
	}
	if resourceExists {
		if mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionUnknown ||
			mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionFalse {
			addTTR(mg)
		}
		mg.SetConditions(xpv1.Available())
		stateValueMap, err := n.fromInstanceStateToJSONMap(newState)
		if err != nil {
			return managed.ExternalObservation{}, err
		}
		err = mg.(resource.Terraformed).SetObservation(stateValueMap)
		if err != nil {
			return managed.ExternalObservation{}, errors.Errorf("could not set observation: %v", err)
		}
		connDetails, err = resource.GetConnectionDetails(stateValueMap, mg.(resource.Terraformed), n.config)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot get connection details")
		}
		instanceDiff, err := n.getResourceDataDiff(ctx, n.instanceState)
		if err != nil {
			return managed.ExternalObservation{}, err
		}
		noDiff = instanceDiff.Empty()

		if noDiff {
			n.metricRecorder.SetReconcileTime(mg.GetName())
		}
	}

	return managed.ExternalObservation{
		ResourceExists:    resourceExists,
		ResourceUpToDate:  noDiff,
		ConnectionDetails: connDetails,
	}, nil
}

func (n *noForkExternal) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	instanceDiff, err := n.getResourceDataDiff(ctx, n.instanceState)
	if err != nil {
		return managed.ExternalCreation{}, err
	}
	start := time.Now()
	newState, diag := n.resourceSchema.Apply(ctx, n.instanceState, instanceDiff, n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("create").Observe(time.Since(start).Seconds())
	// diag := n.resourceSchema.CreateWithoutTimeout(ctx, n.resourceData, n.ts.Meta)
	if diag != nil && diag.HasError() {
		return managed.ExternalCreation{}, errors.Errorf("failed to create the resource: %v", diag)
	}

	if newState == nil || newState.ID == "" {
		return managed.ExternalCreation{}, errors.New("failed to read the ID of the new resource")
	}

	en, err := n.config.ExternalName.GetExternalNameFn(map[string]any{
		"id": newState.ID,
	})
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrapf(err, "failed to get the external-name from ID: %s", newState.ID)
	}
	// we have to make sure the newly set externa-name is recorded
	meta.SetExternalName(mg, en)
	stateValueMap, err := n.fromInstanceStateToJSONMap(newState)
	if err != nil {
		return managed.ExternalCreation{}, err
	}
	err = mg.(resource.Terraformed).SetObservation(stateValueMap)
	if err != nil {
		return managed.ExternalCreation{}, errors.Errorf("could not set observation: %v", err)
	}
	conn, err := resource.GetConnectionDetails(stateValueMap, mg.(resource.Terraformed), n.config)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot get connection details")
	}

	return managed.ExternalCreation{ConnectionDetails: conn}, nil
}

func (n *noForkExternal) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	instanceDiff, err := n.getResourceDataDiff(ctx, n.instanceState)
	if err != nil {
		return managed.ExternalUpdate{}, err
	}
	start := time.Now()
	newState, diag := n.resourceSchema.Apply(ctx, n.instanceState, instanceDiff, n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("update").Observe(time.Since(start).Seconds())
	if diag != nil && diag.HasError() {
		return managed.ExternalUpdate{}, errors.Errorf("failed to update the resource: %v", diag)
	}

	stateValueMap, err := n.fromInstanceStateToJSONMap(newState)
	if err != nil {
		return managed.ExternalUpdate{}, err
	}

	err = mg.(resource.Terraformed).SetObservation(stateValueMap)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Errorf("failed to set observation: %v", err)
	}
	return managed.ExternalUpdate{}, nil
}

func (n *noForkExternal) Delete(ctx context.Context, _ xpresource.Managed) error {
	instanceDiff, err := n.getResourceDataDiff(ctx, n.instanceState)
	if err != nil {
		return err
	}
	if instanceDiff == nil {
		instanceDiff = tf.NewInstanceDiff()
	}

	instanceDiff.Destroy = true
	start := time.Now()
	_, diag := n.resourceSchema.Apply(ctx, n.instanceState, instanceDiff, n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	if diag != nil && diag.HasError() {
		return errors.Errorf("failed to delete the resource: %v", diag)
	}
	return nil
}

func (n *noForkExternal) fromInstanceStateToJSONMap(newState *tf.InstanceState) (map[string]interface{}, error) {
	impliedType := n.resourceSchema.CoreConfigSchema().ImpliedType()
	attrsAsCtyValue, err := newState.AttrsAsObjectValue(impliedType)
	if err != nil {
		return nil, errors.Wrap(err, "could not convert attrs to cty value")
	}
	stateValueMap, err := schema.StateValueToJSONMap(attrsAsCtyValue, impliedType)
	if err != nil {
		return nil, errors.Wrap(err, "could not convert instance state value to JSON")
	}
	return stateValueMap, nil
}
