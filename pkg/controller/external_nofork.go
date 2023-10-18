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

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/metrics"
	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/resource/json"
	"github.com/upbound/upjet/pkg/terraform"
)

type NoForkConnector struct {
	getTerraformSetup     terraform.SetupFn
	kube                  client.Client
	config                *config.Resource
	logger                logging.Logger
	metricRecorder        *metrics.MetricRecorder
	operationTrackerStore *OperationTrackerStore
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

func NewNoForkConnector(kube client.Client, sf terraform.SetupFn, cfg *config.Resource, ots *OperationTrackerStore, opts ...NoForkOption) *NoForkConnector {
	nfc := &NoForkConnector{
		kube:                  kube,
		getTerraformSetup:     sf,
		config:                cfg,
		operationTrackerStore: ots,
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

func getJSONMap(mg xpresource.Managed) (map[string]any, error) {
	pv, err := fieldpath.PaveObject(mg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot pave the managed resource")
	}
	v, err := pv.GetValue("spec.forProvider")
	if err != nil {
		return nil, errors.Wrap(err, "cannot get spec.forProvider value from paved object")
	}
	return v.(map[string]any), nil
}

type noForkExternal struct {
	ts             terraform.Setup
	resourceSchema *schema.Resource
	config         *config.Resource
	kube           client.Client
	instanceDiff   *tf.InstanceDiff
	params         map[string]any
	rawConfig      cty.Value
	logger         logging.Logger
	metricRecorder *metrics.MetricRecorder
	opTracker      *AsyncTracker
}

func (c *NoForkConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	c.metricRecorder.ObserveReconcileDelay(mg.GetObjectKind().GroupVersionKind(), mg.GetName())
	logger := c.logger.WithValues("uid", mg.GetUID(), "name", mg.GetName(), "gvk", mg.GetObjectKind().GroupVersionKind().String())
	logger.Debug("Connecting to the service provider")
	start := time.Now()
	ts, err := c.getTerraformSetup(ctx, c.kube, mg)
	metrics.ExternalAPITime.WithLabelValues("connect").Observe(time.Since(start).Seconds())
	if err != nil {
		return nil, errors.Wrap(err, errGetTerraformSetup)
	}

	// To Compute the ResourceDiff: n.resourceSchema.Diff(...)
	tr := mg.(resource.Terraformed)
	opTracker := c.operationTrackerStore.Tracker(tr)
	externalName := meta.GetExternalName(tr)
	if externalName == "" {
		externalName = opTracker.GetTfID()
	}
	params, err := tr.GetParameters()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get parameters")
	}
	if err = resource.GetSensitiveParameters(ctx, &APISecretClient{kube: c.kube}, tr, params, tr.GetConnectionDetailsMapping()); err != nil {
		return nil, errors.Wrap(err, "cannot store sensitive parameters into params")
	}
	c.config.ExternalName.SetIdentifierArgumentFn(params, externalName)
	if c.config.TerraformConfigurationInjector != nil {
		m, err := getJSONMap(mg)
		if err != nil {
			return nil, errors.Wrap(err, "cannot get JSON map for the managed resource's spec.forProvider value")
		}
		c.config.TerraformConfigurationInjector(m, params)
	}

	tfID, err := c.config.ExternalName.GetIDFn(ctx, externalName, params, ts.Map())
	if err != nil {
		return nil, errors.Wrap(err, "cannot get ID")
	}
	params["id"] = tfID
	// we need to parameterize the following for a provider
	// not all providers may have this attribute
	// TODO: tags_all handling
	schemaBlock := c.config.TerraformResource.CoreConfigSchema()
	attrs := schemaBlock.Attributes
	if _, ok := attrs["tags_all"]; ok {
		params["tags_all"] = params["tags"]
	}

	var rawConfig cty.Value
	if !opTracker.HasState() {
		logger.Debug("Instance state not found in cache, reconstructing...")
		tfState, err := tr.GetObservation()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get the observation")
		}
		copyParams := len(tfState) == 0
		if err = resource.GetSensitiveParameters(ctx, &APISecretClient{kube: c.kube}, tr, tfState, tr.GetConnectionDetailsMapping()); err != nil {
			return nil, errors.Wrap(err, "cannot store sensitive parameters into tfState")
		}
		c.config.ExternalName.SetIdentifierArgumentFn(tfState, externalName)
		tfState["id"] = tfID
		if copyParams {
			tfState = copyParameters(tfState, params)
		}

		tfStateCtyValue, err := schema.JSONMapToStateValue(tfState, schemaBlock)
		if err != nil {
			return nil, errors.Wrap(err, "cannot convert JSON map to state cty.Value")
		}
		s, err := c.config.TerraformResource.ShimInstanceStateFromValue(tfStateCtyValue)
		if err != nil {
			return nil, errors.Wrap(err, "failed to convert cty.Value to terraform.InstanceState")
		}
		s.RawPlan = tfStateCtyValue
		rawConfig, err = schema.JSONMapToStateValue(params, schemaBlock)
		if err != nil {
			return nil, errors.Wrap(err, "failed to convert params JSON map to cty.Value")
		}
		s.RawConfig = rawConfig
		opTracker.SetTfState(s)
	}

	return &noForkExternal{
		ts:             ts,
		resourceSchema: c.config.TerraformResource,
		config:         c.config,
		kube:           c.kube,
		params:         params,
		rawConfig:      rawConfig,
		logger:         logger,
		metricRecorder: c.metricRecorder,
		opTracker:      opTracker,
	}, nil
}

func (n *noForkExternal) getResourceDataDiff(ctx context.Context, s *tf.InstanceState) (*tf.InstanceDiff, error) {
	instanceDiff, err := schema.InternalMap(n.resourceSchema.Schema).Diff(ctx, s, tf.NewResourceConfigRaw(n.params), nil, n.ts.Meta, false)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get *terraform.InstanceDiff")
	}
	if n.config.TerraformCustomDiff != nil {
		instanceDiff, err = n.config.TerraformCustomDiff(instanceDiff)
		if err != nil {
			return nil, errors.Wrap(err, "failed to compute the customized terraform.InstanceDiff")
		}
	}
	if instanceDiff != nil {
		v := cty.EmptyObjectVal
		v, err = instanceDiff.ApplyToValue(v, n.resourceSchema.CoreConfigSchema())
		if err != nil {
			return nil, errors.Wrap(err, "cannot apply Terraform instance diff to an empty value")
		}
		instanceDiff.RawPlan = v
	}
	if instanceDiff != nil && len(instanceDiff.Attributes) > 0 {
		n.logger.Debug("Diff detected", "instanceDiff", instanceDiff.GoString())
		instanceDiff.RawConfig = n.rawConfig
	}
	return instanceDiff, nil
}

func (n *noForkExternal) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	n.logger.Debug("Observing the external resource")
	start := time.Now()
	newState, diag := n.resourceSchema.RefreshWithoutUpgrade(ctx, n.opTracker.GetTfState(), n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("read").Observe(time.Since(start).Seconds())
	if diag != nil && diag.HasError() {
		return managed.ExternalObservation{}, errors.Errorf("failed to observe the resource: %v", diag)
	}
	n.opTracker.SetTfState(newState) // TODO: missing RawConfig & RawPlan here...
	// compute the instance diff
	instanceDiff, err := n.getResourceDataDiff(ctx, newState)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot compute the instance diff")
	}
	n.instanceDiff = instanceDiff
	noDiff := instanceDiff.Empty()

	var connDetails managed.ConnectionDetails
	resourceExists := newState != nil && newState.ID != ""
	if !resourceExists && mg.GetDeletionTimestamp() != nil {
		gvk := mg.GetObjectKind().GroupVersionKind()
		metrics.DeletionTime.WithLabelValues(gvk.Group, gvk.Version, gvk.Kind).Observe(time.Since(mg.GetDeletionTimestamp().Time).Seconds())
	}
	specUpdateRequired := false
	if resourceExists {
		if mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionUnknown ||
			mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionFalse {
			addTTR(mg)
		}
		mg.SetConditions(xpv1.Available())
		stateValueMap, err := n.fromInstanceStateToJSONMap(newState)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot convert instance state to JSON map")
		}

		buff, err := json.TFParser.Marshal(stateValueMap)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot marshal the attributes of the new state for late-initialization")
		}
		specUpdateRequired, err = mg.(resource.Terraformed).LateInitialize(buff)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot late-initialize the managed resource")
		}

		err = mg.(resource.Terraformed).SetObservation(stateValueMap)
		if err != nil {
			return managed.ExternalObservation{}, errors.Errorf("could not set observation: %v", err)
		}
		connDetails, err = resource.GetConnectionDetails(stateValueMap, mg.(resource.Terraformed), n.config)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot get connection details")
		}

		if noDiff {
			n.metricRecorder.SetReconcileTime(mg.GetName())
		}
		if !specUpdateRequired {
			resource.SetUpToDateCondition(mg, noDiff)
		}
		// check for an external-name change
		if nameChanged, err := n.setExternalName(mg, newState); err != nil {
			return managed.ExternalObservation{}, errors.Wrapf(err, "failed to set the external-name of the managed resource during observe")
		} else {
			specUpdateRequired = specUpdateRequired || nameChanged
		}
	}

	return managed.ExternalObservation{
		ResourceExists:          resourceExists,
		ResourceUpToDate:        noDiff,
		ConnectionDetails:       connDetails,
		ResourceLateInitialized: specUpdateRequired,
	}, nil
}

// sets the external-name on the MR. Returns `true`
// if the external-name of the MR has changed.
func (n *noForkExternal) setExternalName(mg xpresource.Managed, newState *tf.InstanceState) (bool, error) {
	if newState.ID == "" {
		return false, nil
	}
	newName, err := n.config.ExternalName.GetExternalNameFn(map[string]any{
		"id": newState.ID,
	})
	if err != nil {
		return false, errors.Wrapf(err, "failed to get the external-name from ID: %s", newState.ID)
	}
	oldName := meta.GetExternalName(mg)
	// we have to make sure the newly set external-name is recorded
	meta.SetExternalName(mg, newName)
	return oldName != newName, nil
}

func (n *noForkExternal) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	n.logger.Debug("Creating the external resource")
	start := time.Now()
	newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), n.instanceDiff, n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("create").Observe(time.Since(start).Seconds())
	// diag := n.resourceSchema.CreateWithoutTimeout(ctx, n.resourceData, n.ts.Meta)
	if diag != nil && diag.HasError() {
		return managed.ExternalCreation{}, errors.Errorf("failed to create the resource: %v", diag)
	}

	if newState == nil || newState.ID == "" {
		return managed.ExternalCreation{}, errors.New("failed to read the ID of the new resource")
	}
	n.opTracker.SetTfState(newState)

	if _, err := n.setExternalName(mg, newState); err != nil {
		return managed.ExternalCreation{}, errors.Wrapf(err, "failed to set the external-name of the managed resource during create")
	}
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
	n.logger.Debug("Updating the external resource")
	start := time.Now()
	newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), n.instanceDiff, n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("update").Observe(time.Since(start).Seconds())
	if diag != nil && diag.HasError() {
		return managed.ExternalUpdate{}, errors.Errorf("failed to update the resource: %v", diag)
	}
	n.opTracker.SetTfState(newState)

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
	n.logger.Debug("Deleting the external resource")
	if n.instanceDiff == nil {
		n.instanceDiff = tf.NewInstanceDiff()
	}

	n.instanceDiff.Destroy = true
	start := time.Now()
	newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), n.instanceDiff, n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	if diag != nil && diag.HasError() {
		return errors.Errorf("failed to delete the resource: %v", diag)
	}
	n.opTracker.SetTfState(newState)
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
