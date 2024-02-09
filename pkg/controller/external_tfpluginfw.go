// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	fwdiag "github.com/hashicorp/terraform-plugin-framework/diag"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/metrics"
	"github.com/crossplane/upjet/pkg/resource"
	upjson "github.com/crossplane/upjet/pkg/resource/json"
	"github.com/crossplane/upjet/pkg/terraform"
)

// TerraformPluginFrameworkConnector is an external client, with credentials and
// other configuration parameters, for Terraform Plugin Framework resources. You
// can use NewTerraformPluginFrameworkConnector to construct.
type TerraformPluginFrameworkConnector struct {
	getTerraformSetup           terraform.SetupFn
	kube                        client.Client
	config                      *config.Resource
	logger                      logging.Logger
	metricRecorder              *metrics.MetricRecorder
	operationTrackerStore       *OperationTrackerStore
	isManagementPoliciesEnabled bool
}

// TerraformPluginFrameworkConnectorOption allows you to configure TerraformPluginFrameworkConnector.
type TerraformPluginFrameworkConnectorOption func(connector *TerraformPluginFrameworkConnector)

// WithTerraformPluginFrameworkLogger configures a logger for the TerraformPluginFrameworkConnector.
func WithTerraformPluginFrameworkLogger(l logging.Logger) TerraformPluginFrameworkConnectorOption {
	return func(c *TerraformPluginFrameworkConnector) {
		c.logger = l
	}
}

// WithTerraformPluginFrameworkMetricRecorder configures a metrics.MetricRecorder for the
// TerraformPluginFrameworkConnectorOption.
func WithTerraformPluginFrameworkMetricRecorder(r *metrics.MetricRecorder) TerraformPluginFrameworkConnectorOption {
	return func(c *TerraformPluginFrameworkConnector) {
		c.metricRecorder = r
	}
}

// WithTerraformPluginFrameworkManagementPolicies configures whether the client should
// handle management policies.
func WithTerraformPluginFrameworkManagementPolicies(isManagementPoliciesEnabled bool) TerraformPluginFrameworkConnectorOption {
	return func(c *TerraformPluginFrameworkConnector) {
		c.isManagementPoliciesEnabled = isManagementPoliciesEnabled
	}
}

// NewTerraformPluginFrameworkConnector creates a new
// TerraformPluginFrameworkConnector with given options.
func NewTerraformPluginFrameworkConnector(kube client.Client, sf terraform.SetupFn, cfg *config.Resource, ots *OperationTrackerStore, opts ...TerraformPluginFrameworkConnectorOption) *TerraformPluginFrameworkConnector {
	connector := &TerraformPluginFrameworkConnector{
		getTerraformSetup:     sf,
		kube:                  kube,
		config:                cfg,
		operationTrackerStore: ots,
	}
	for _, f := range opts {
		f(connector)
	}
	return connector
}

type terraformPluginFrameworkExternalClient struct {
	ts             terraform.Setup
	config         *config.Resource
	logger         logging.Logger
	metricRecorder *metrics.MetricRecorder
	opTracker      *AsyncTracker
	resource       fwresource.Resource
	server         tfprotov5.ProviderServer
	params         map[string]any
	planResponse   *tfprotov5.PlanResourceChangeResponse
	resourceSchema rschema.Schema
	// the terraform value type associated with the resource schema
	resourceValueTerraformType tftypes.Type
}

// Connect makes sure the underlying client is ready to issue requests to the
// provider API.
func (c *TerraformPluginFrameworkConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) { //nolint:gocyclo
	c.metricRecorder.ObserveReconcileDelay(mg.GetObjectKind().GroupVersionKind(), mg.GetName())
	logger := c.logger.WithValues("uid", mg.GetUID(), "name", mg.GetName(), "gvk", mg.GetObjectKind().GroupVersionKind().String())
	logger.Debug("Connecting to the service provider")
	start := time.Now()
	ts, err := c.getTerraformSetup(ctx, c.kube, mg)
	metrics.ExternalAPITime.WithLabelValues("connect").Observe(time.Since(start).Seconds())
	if err != nil {
		return nil, errors.Wrap(err, errGetTerraformSetup)
	}

	tr := mg.(resource.Terraformed)
	opTracker := c.operationTrackerStore.Tracker(tr)
	externalName := meta.GetExternalName(tr)
	params, err := getExtendedParameters(ctx, tr, externalName, c.config, ts, c.isManagementPoliciesEnabled, c.kube)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get the extended parameters for resource %q", mg.GetName())
	}

	resourceSchema, err := c.getResourceSchema(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "could not retrieve resource schema")
	}
	resourceTfValueType := resourceSchema.Type().TerraformType(ctx)
	hasState := false
	if opTracker.HasFrameworkTFState() {
		tfStateValue, err := opTracker.GetFrameworkTFState().Unmarshal(resourceTfValueType)
		if err != nil {
			return nil, errors.Wrap(err, "cannot unmarshal TF state dynamic value during state existence check")
		}
		hasState = err == nil && !tfStateValue.IsNull()
	}

	if !hasState {
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
		tfState["id"] = params["id"]
		if copyParams {
			tfState = copyParameters(tfState, params)
		}

		tfStateDynamicValue, err := protov5DynamicValueFromMap(tfState, resourceTfValueType)
		if err != nil {
			return nil, errors.Wrap(err, "cannot construct dynamic value for TF state")
		}
		opTracker.SetFrameworkTFState(tfStateDynamicValue)
	}

	configuredProviderServer, err := c.configureProvider(ctx, ts)
	if err != nil {
		return nil, errors.Wrap(err, "could not configure provider server")
	}

	return &terraformPluginFrameworkExternalClient{
		ts:                         ts,
		config:                     c.config,
		logger:                     logger,
		metricRecorder:             c.metricRecorder,
		opTracker:                  opTracker,
		resource:                   c.config.TerraformPluginFrameworkResource,
		server:                     configuredProviderServer,
		params:                     params,
		resourceSchema:             resourceSchema,
		resourceValueTerraformType: resourceTfValueType,
	}, nil
}

// getResourceSchema returns the Terraform Plugin Framework-style resource schema for the configured framework resource on the connector
func (c *TerraformPluginFrameworkConnector) getResourceSchema(ctx context.Context) (rschema.Schema, error) {
	res := c.config.TerraformPluginFrameworkResource
	schemaResp := &fwresource.SchemaResponse{}
	res.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		fwErrors := frameworkDiagnosticsToString(schemaResp.Diagnostics)
		return rschema.Schema{}, errors.Errorf("could not retrieve resource schema: %s", fwErrors)
	}

	return schemaResp.Schema, nil
}

// configureProvider returns a configured Terraform protocol v5 provider server
// with the preconfigured provider instance in the terraform setup.
// The provider instance used should be already preconfigured
// at the terraform setup layer with the relevant provider meta if needed
// by the provider implementation.
func (c *TerraformPluginFrameworkConnector) configureProvider(ctx context.Context, ts terraform.Setup) (tfprotov5.ProviderServer, error) {
	var schemaResp fwprovider.SchemaResponse
	ts.FrameworkProvider.Schema(ctx, fwprovider.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		fwDiags := frameworkDiagnosticsToString(schemaResp.Diagnostics)
		return nil, fmt.Errorf("cannot retrieve provider schema: %s", fwDiags)
	}
	providerServer := providerserver.NewProtocol5(ts.FrameworkProvider)()

	providerConfigDynamicVal, err := protov5DynamicValueFromMap(ts.Configuration, schemaResp.Schema.Type().TerraformType(ctx))
	if err != nil {
		return nil, errors.Wrap(err, "cannot construct dynamic value for TF provider config")
	}

	configureProviderReq := &tfprotov5.ConfigureProviderRequest{
		TerraformVersion: "crossTF000",
		Config:           providerConfigDynamicVal,
	}
	providerResp, err := providerServer.ConfigureProvider(ctx, configureProviderReq)
	if err != nil {
		return nil, errors.Wrap(err, "cannot configure framework provider")
	}
	if fatalDiags := getFatalDiagnostics(providerResp.Diagnostics); fatalDiags != nil {
		return nil, errors.Wrap(fatalDiags, "provider configure request failed")
	}
	return providerServer, nil
}

// getDiffPlanResponse calls the underlying native TF provider's PlanResourceChange RPC,
// and returns the planned state and whether a diff exists.
// If plan response contains non-empty RequiresReplace (i.e. the resource needs
// to be recreated) an error is returned as Crossplane Resource Model (XRM)
// prohibits resource re-creations and rejects this plan.
func (n *terraformPluginFrameworkExternalClient) getDiffPlanResponse(ctx context.Context,
	tfStateValue tftypes.Value) (*tfprotov5.PlanResourceChangeResponse, bool, error) {
	tfConfigDynamicVal, err := protov5DynamicValueFromMap(n.params, n.resourceValueTerraformType)
	if err != nil {
		return nil, false, errors.Wrap(err, "cannot construct dynamic value for TF Config")
	}

	//
	tfPlannedStateDynamicVal, err := protov5DynamicValueFromMap(n.params, n.resourceValueTerraformType)
	if err != nil {
		return nil, false, errors.Wrap(err, "cannot construct dynamic value for TF Planned State")
	}

	prcReq := &tfprotov5.PlanResourceChangeRequest{
		TypeName:         n.config.Name,
		PriorState:       n.opTracker.GetFrameworkTFState(),
		Config:           tfConfigDynamicVal,
		ProposedNewState: tfPlannedStateDynamicVal,
	}
	planResponse, err := n.server.PlanResourceChange(ctx, prcReq)
	if err != nil {
		return nil, false, errors.Wrap(err, "cannot plan change")
	}
	if fatalDiags := getFatalDiagnostics(planResponse.Diagnostics); fatalDiags != nil {
		return nil, false, errors.Wrap(fatalDiags, "plan resource change request failed")
	}

	plannedStateValue, err := planResponse.PlannedState.Unmarshal(n.resourceValueTerraformType)
	if err != nil {
		return nil, false, errors.Wrap(err, "cannot unmarshal planned state")
	}

	rawDiff, err := plannedStateValue.Diff(tfStateValue)
	if err != nil {
		return nil, false, errors.Wrap(err, "cannot compare prior state and plan")
	}

	// filter diffs that has unknown plan value which corresponds to
	// computed values. These cause unnecessary diff detection when only computed
	// attribute diffs exist in the raw diff and no actual diff exists in the
	// parametrizable attributes
	filteredDiff := make([]tftypes.ValueDiff, 0)
	for _, diff := range rawDiff {
		if diff.Value1.IsKnown() {
			filteredDiff = append(filteredDiff, diff)
		}
	}

	return planResponse, len(filteredDiff) > 0, nil
}

func (n *terraformPluginFrameworkExternalClient) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) { //nolint:gocyclo
	n.logger.Debug("Observing the external resource")

	if meta.WasDeleted(mg) && n.opTracker.IsDeleted() {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	readRequest := &tfprotov5.ReadResourceRequest{
		TypeName:     n.config.Name,
		CurrentState: n.opTracker.GetFrameworkTFState(),
	}
	readResponse, err := n.server.ReadResource(ctx, readRequest)

	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot read resource")
	}

	if fatalDiags := getFatalDiagnostics(readResponse.Diagnostics); fatalDiags != nil {
		return managed.ExternalObservation{}, errors.Wrap(fatalDiags, "read resource request failed")
	}

	tfStateValue, err := readResponse.NewState.Unmarshal(n.resourceValueTerraformType)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot unmarshal state value")
	}

	n.opTracker.SetFrameworkTFState(readResponse.NewState)
	resourceExists := !tfStateValue.IsNull()

	var stateValueMap map[string]any
	if resourceExists {
		if conv, err := tfValueToGoValue(tfStateValue); err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot convert instance state to JSON map")
		} else {
			stateValueMap = conv.(map[string]any)
		}
	}

	planResponse, hasDiff, err := n.getDiffPlanResponse(ctx, tfStateValue)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot calculate diff")
	}

	n.planResponse = planResponse

	var connDetails managed.ConnectionDetails
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
		buff, err := upjson.TFParser.Marshal(stateValueMap)
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
		if !hasDiff {
			n.metricRecorder.SetReconcileTime(mg.GetName())
		}
		if !specUpdateRequired {
			resource.SetUpToDateCondition(mg, !hasDiff)
		}
		if nameChanged, err := n.setExternalName(mg, stateValueMap); err != nil {
			return managed.ExternalObservation{}, errors.Wrapf(err, "failed to set the external-name of the managed resource during observe")
		} else {
			specUpdateRequired = specUpdateRequired || nameChanged
		}
	}

	return managed.ExternalObservation{
		ResourceExists:          resourceExists,
		ResourceUpToDate:        !hasDiff,
		ConnectionDetails:       connDetails,
		ResourceLateInitialized: specUpdateRequired,
	}, nil
}

func (n *terraformPluginFrameworkExternalClient) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	n.logger.Debug("Creating the external resource")

	tfConfigDynamicVal, err := protov5DynamicValueFromMap(n.params, n.resourceValueTerraformType)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot construct dynamic value for TF Config")
	}

	applyRequest := &tfprotov5.ApplyResourceChangeRequest{
		TypeName:     n.config.Name,
		PriorState:   n.opTracker.GetFrameworkTFState(),
		PlannedState: n.planResponse.PlannedState,
		Config:       tfConfigDynamicVal,
	}
	start := time.Now()
	applyResponse, err := n.server.ApplyResourceChange(ctx, applyRequest)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot create resource")
	}
	metrics.ExternalAPITime.WithLabelValues("create").Observe(time.Since(start).Seconds())
	if fatalDiags := getFatalDiagnostics(applyResponse.Diagnostics); fatalDiags != nil {
		return managed.ExternalCreation{}, errors.Wrap(fatalDiags, "resource creation call returned error diags")
	}

	newStateAfterApplyVal, err := applyResponse.NewState.Unmarshal(n.resourceValueTerraformType)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot unmarshal planned state")
	}

	if newStateAfterApplyVal.IsNull() {
		return managed.ExternalCreation{}, errors.New("new state is empty after creation")
	}

	var stateValueMap map[string]any
	if goval, err := tfValueToGoValue(newStateAfterApplyVal); err != nil {
		return managed.ExternalCreation{}, errors.New("cannot convert native state to go map")
	} else {
		stateValueMap = goval.(map[string]any)
	}

	n.opTracker.SetFrameworkTFState(applyResponse.NewState)

	if _, err := n.setExternalName(mg, stateValueMap); err != nil {
		return managed.ExternalCreation{}, errors.Wrapf(err, "failed to set the external-name of the managed resource during create")
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

func (n *terraformPluginFrameworkExternalClient) planRequiresReplace() (bool, string) {
	if n.planResponse == nil || len(n.planResponse.RequiresReplace) == 0 {
		return false, ""
	}

	var sb strings.Builder
	sb.WriteString("diff contains fields that require resource replacement: ")
	for _, attrPath := range n.planResponse.RequiresReplace {
		sb.WriteString(attrPath.String())
		sb.WriteString(", ")
	}
	return true, sb.String()

}

func (n *terraformPluginFrameworkExternalClient) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	n.logger.Debug("Updating the external resource")
	// refuse plans that require replace for XRM compliance
	if isReplace, fields := n.planRequiresReplace(); isReplace {
		return managed.ExternalUpdate{}, errors.Errorf("diff contains fields that require resource replacement: %s", fields)
	}

	tfConfigDynamicVal, err := protov5DynamicValueFromMap(n.params, n.resourceValueTerraformType)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot construct dynamic value for TF Config")
	}

	applyRequest := &tfprotov5.ApplyResourceChangeRequest{
		TypeName:     n.config.Name,
		PriorState:   n.opTracker.GetFrameworkTFState(),
		PlannedState: n.planResponse.PlannedState,
		Config:       tfConfigDynamicVal,
	}
	start := time.Now()
	applyResponse, err := n.server.ApplyResourceChange(ctx, applyRequest)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot update resource")
	}
	metrics.ExternalAPITime.WithLabelValues("update").Observe(time.Since(start).Seconds())
	if fatalDiags := getFatalDiagnostics(applyResponse.Diagnostics); fatalDiags != nil {
		return managed.ExternalUpdate{}, errors.Wrap(fatalDiags, "resource update call returned error diags")
	}
	n.opTracker.SetFrameworkTFState(applyResponse.NewState)

	newStateAfterApplyVal, err := applyResponse.NewState.Unmarshal(n.resourceValueTerraformType)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot unmarshal updated state")
	}

	if newStateAfterApplyVal.IsNull() {
		return managed.ExternalUpdate{}, errors.New("new state is empty after update")
	}

	var stateValueMap map[string]any
	if goval, err := tfValueToGoValue(newStateAfterApplyVal); err != nil {
		return managed.ExternalUpdate{}, errors.New("cannot convert native state to go map")
	} else {
		stateValueMap = goval.(map[string]any)
	}

	err = mg.(resource.Terraformed).SetObservation(stateValueMap)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Errorf("could not set observation: %v", err)
	}

	return managed.ExternalUpdate{}, nil
}

func (n *terraformPluginFrameworkExternalClient) Delete(ctx context.Context, _ xpresource.Managed) error {
	n.logger.Debug("Deleting the external resource")

	tfConfigDynamicVal, err := protov5DynamicValueFromMap(n.params, n.resourceValueTerraformType)
	if err != nil {
		return errors.Wrap(err, "cannot construct dynamic value for TF Config")
	}
	// set an empty planned state, this corresponds to deleting
	plannedState, err := tfprotov5.NewDynamicValue(n.resourceValueTerraformType, tftypes.NewValue(n.resourceValueTerraformType, nil))
	if err != nil {
		return errors.Wrap(err, "cannot set the planned state for deletion")
	}

	applyRequest := &tfprotov5.ApplyResourceChangeRequest{
		TypeName:     n.config.Name,
		PriorState:   n.opTracker.GetFrameworkTFState(),
		PlannedState: &plannedState,
		Config:       tfConfigDynamicVal,
	}
	start := time.Now()
	applyResponse, err := n.server.ApplyResourceChange(ctx, applyRequest)
	if err != nil {
		return errors.Wrap(err, "cannot delete resource")
	}
	metrics.ExternalAPITime.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	if fatalDiags := getFatalDiagnostics(applyResponse.Diagnostics); fatalDiags != nil {
		return errors.Wrap(fatalDiags, "resource deletion call returned error diags")
	}
	n.opTracker.SetFrameworkTFState(applyResponse.NewState)

	newStateAfterApplyVal, err := applyResponse.NewState.Unmarshal(n.resourceValueTerraformType)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal state after deletion")
	}
	// mark the resource as logically deleted if the TF call clears the state
	n.opTracker.SetDeleted(newStateAfterApplyVal.IsNull())

	return nil
}

func (n *terraformPluginFrameworkExternalClient) setExternalName(mg xpresource.Managed, stateValueMap map[string]interface{}) (bool, error) {
	id, ok := stateValueMap["id"]
	if !ok || id.(string) == "" {
		return false, nil
	}
	newName, err := n.config.ExternalName.GetExternalNameFn(stateValueMap)
	if err != nil {
		return false, errors.Wrapf(err, "failed to compute the external-name from the state map of the resource with the ID %s", id)
	}
	oldName := meta.GetExternalName(mg)
	// we have to make sure the newly set external-name is recorded
	meta.SetExternalName(mg, newName)
	return oldName != newName, nil
}

// tfValueToGoValue converts a given tftypes.Value to Go-native any type.
// Useful for converting terraform values of state to JSON or for setting
// observations at the MR.
// Nested values are recursively converted.
// Supported conversions:
// tftypes.Object, tftypes.Map => map[string]any
// tftypes.Set, tftypes.List, tftypes.Tuple => []string
// tftypes.Bool => bool
// tftypes.Number => int64, float64
// tftypes.String => string
// tftypes.DynamicPseudoType => conversion not supported and returns an error
func tfValueToGoValue(input tftypes.Value) (any, error) { //nolint:gocyclo
	if !input.IsKnown() {
		return nil, fmt.Errorf("cannot convert unknown value")
	}
	if input.IsNull() {
		return nil, nil
	}
	valType := input.Type()
	switch {
	case valType.Is(tftypes.Object{}), valType.Is(tftypes.Map{}):
		destInterim := make(map[string]tftypes.Value)
		dest := make(map[string]any)
		if err := input.As(&destInterim); err != nil {
			return nil, err
		}
		for k, v := range destInterim {
			res, err := tfValueToGoValue(v)
			if err != nil {
				return nil, err
			}
			dest[k] = res

		}
		return dest, nil
	case valType.Is(tftypes.Set{}), valType.Is(tftypes.List{}), valType.Is(tftypes.Tuple{}):
		destInterim := make([]tftypes.Value, 0)
		if err := input.As(&destInterim); err != nil {
			return nil, err
		}
		dest := make([]any, len(destInterim))
		for i, v := range destInterim {
			res, err := tfValueToGoValue(v)
			if err != nil {
				return nil, err
			}
			dest[i] = res
		}
		return dest, nil
	case valType.Is(tftypes.Bool):
		var x bool
		return x, input.As(&x)
	case valType.Is(tftypes.Number):
		var valBigF big.Float
		if err := input.As(&valBigF); err != nil {
			return nil, err
		}
		// try to parse as integer
		if valBigF.IsInt() {
			intVal, accuracy := valBigF.Int64()
			if accuracy != 0 {
				return nil, fmt.Errorf("value %v cannot be represented as a 64-bit integer", valBigF)
			}
			return intVal, nil
		}
		// try to parse as float64
		xf, accuracy := valBigF.Float64()
		// Underflow
		// Reference: https://pkg.go.dev/math/big#Float.Float64
		if xf == 0 && accuracy != big.Exact {
			return nil, fmt.Errorf("value %v cannot be represented as a 64-bit floating point", valBigF)
		}

		// Overflow
		// Reference: https://pkg.go.dev/math/big#Float.Float64
		if math.IsInf(xf, 0) {
			return nil, fmt.Errorf("value %v cannot be represented as a 64-bit floating point", valBigF)
		}
		return xf, nil

	case valType.Is(tftypes.String):
		var x string
		return x, input.As(&x)
	case valType.Is(tftypes.DynamicPseudoType):
		return nil, errors.New("DynamicPseudoType conversion is not supported")
	default:
		return nil, fmt.Errorf("input value has unknown type: %s", valType.String())
	}
}

// getFatalDiagnostics traverses the given Terraform protov5 diagnostics type
// and constructs a Go error. If the provided diag slice is empty, returns nil.
func getFatalDiagnostics(diags []*tfprotov5.Diagnostic) error {
	var errs error
	var diagErrors []string
	for _, tfdiag := range diags {
		if tfdiag.Severity == tfprotov5.DiagnosticSeverityInvalid || tfdiag.Severity == tfprotov5.DiagnosticSeverityError {
			diagErrors = append(diagErrors, fmt.Sprintf("%s: %s", tfdiag.Summary, tfdiag.Detail))
		}
	}
	if len(diagErrors) > 0 {
		errs = errors.New(strings.Join(diagErrors, "\n"))
	}
	return errs
}

// frameworkDiagnosticsToString constructs an error string from the provided
// Plugin Framework diagnostics instance. Only Error severity diagnostics are
// included.
func frameworkDiagnosticsToString(fwdiags fwdiag.Diagnostics) string {
	frameworkErrorDiags := fwdiags.Errors()
	diagErrors := make([]string, 0, len(frameworkErrorDiags))
	for _, tfdiag := range frameworkErrorDiags {
		diagErrors = append(diagErrors, fmt.Sprintf("%s: %s", tfdiag.Summary(), tfdiag.Detail()))
	}
	return strings.Join(diagErrors, "\n")
}

// protov5DynamicValueFromMap constructs a protov5 DynamicValue given the
// map[string]any using the terraform type as reference.
func protov5DynamicValueFromMap(data map[string]any, terraformType tftypes.Type) (*tfprotov5.DynamicValue, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, errors.Wrap(err, "cannot marshal json")
	}

	tfValue, err := tftypes.ValueFromJSONWithOpts(jsonBytes, terraformType, tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true})
	if err != nil {
		return nil, errors.Wrap(err, "cannot construct tf value from json")
	}

	dynamicValue, err := tfprotov5.NewDynamicValue(terraformType, tfValue)
	if err != nil {
		return nil, errors.Wrap(err, "cannot construct dynamic value from tf value")
	}

	return &dynamicValue, nil
}
