// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	_ "github.com/hashicorp/terraform-plugin-framework/tfsdk"
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

type TerraformPluginFrameworkConnector struct {
	getTerraformSetup                terraform.SetupFn
	kube                             client.Client
	config                           *config.Resource
	logger                           logging.Logger
	metricRecorder                   *metrics.MetricRecorder
	operationTrackerStore            *OperationTrackerStore
	isManagementPoliciesEnabled      bool
	terraformPluginFrameworkProvider *fwprovider.Provider
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

func NewTerraformPluginFrameworkConnector(kube client.Client, sf terraform.SetupFn, cfg *config.Resource, ots *OperationTrackerStore, terraformPluginFrameworkProvider *fwprovider.Provider, opts ...TerraformPluginFrameworkConnectorOption) *TerraformPluginFrameworkConnector {
	connector := &TerraformPluginFrameworkConnector{
		getTerraformSetup:                sf,
		kube:                             kube,
		config:                           cfg,
		operationTrackerStore:            ots,
		terraformPluginFrameworkProvider: terraformPluginFrameworkProvider,
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
	resource       *fwresource.Resource
	server         tfprotov5.ProviderServer
	params         map[string]any
	plannedState   *tfprotov5.DynamicValue
	resourceSchema rschema.Schema
}

func (c *TerraformPluginFrameworkConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
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

	if !opTracker.HasFrameworkTFState() {
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

		tfStateJsonBytes, err := json.Marshal(tfState)
		if err != nil {
			return nil, errors.Wrap(err, "could not marshal TF state map")
		}

		opTracker.SetFrameworkTFState(&tfprotov5.DynamicValue{
			JSON: tfStateJsonBytes,
		})

	}

	configuredProviderServer, err := c.configureProvider(ctx, ts)
	if err != nil {
		return nil, errors.Wrap(err, "could not configure provider server")
	}

	resourceSchema, err := c.getResourceSchema(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "could not retrieve resource schema")
	}

	return &terraformPluginFrameworkExternalClient{
		ts:             ts,
		config:         c.config,
		logger:         logger,
		metricRecorder: c.metricRecorder,
		opTracker:      opTracker,
		resource:       c.config.TerraformPluginFrameworkResource,
		server:         configuredProviderServer,
		params:         params,
		resourceSchema: resourceSchema,
	}, nil
}

func (c *TerraformPluginFrameworkConnector) getResourceSchema(ctx context.Context) (rschema.Schema, error) {
	res := *c.config.TerraformPluginFrameworkResource
	schemaResp := &fwresource.SchemaResponse{}
	res.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		return rschema.Schema{}, errors.Errorf("could not retrieve resource schema: %v", schemaResp.Diagnostics)
	}

	return schemaResp.Schema, nil
}

func (c *TerraformPluginFrameworkConnector) configureProvider(ctx context.Context, ts terraform.Setup) (tfprotov5.ProviderServer, error) {
	providerServer := providerserver.NewProtocol5(*c.terraformPluginFrameworkProvider)()
	tsBytes, err := json.Marshal(ts.Configuration)
	if err != nil {
		return nil, errors.Wrap(err, "cannot marshal ts config")
	}
	configureProviderReq := &tfprotov5.ConfigureProviderRequest{
		TerraformVersion: "crossTF000",
		Config: &tfprotov5.DynamicValue{
			JSON: tsBytes,
		},
	}
	providerResp, err := providerServer.ConfigureProvider(ctx, configureProviderReq)
	if err != nil {
		return nil, err
	}
	// TODO(erhan): improve diag reporting
	if hasFatalDiag := hasFatalDiagnostics(providerResp.Diagnostics); hasFatalDiag {
		return nil, errors.Errorf("provider configure request returned fatal diagnostics")
	}
	return providerServer, nil
}

func (n *terraformPluginFrameworkExternalClient) getDiffPlan(ctx context.Context,
	tfStateValue tftypes.Value) (*tfprotov5.DynamicValue, bool, error) {

	valueTerraformType := n.resourceSchema.Type().TerraformType(ctx)
	paramBytes, err := json.Marshal(n.params)
	if err != nil {
		return &tfprotov5.DynamicValue{}, false, errors.Wrap(err, "cannot convert params to json bytes")
	}

	tfConfigValue, err := tftypes.ValueFromJSONWithOpts(paramBytes, valueTerraformType, tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true})
	if err != nil {
		return &tfprotov5.DynamicValue{}, false, err
	}

	tfConfig, err := tfprotov5.NewDynamicValue(valueTerraformType, tfConfigValue)
	if err != nil {
		return &tfprotov5.DynamicValue{}, false, err
	}

	tfPlannedState, err := tfprotov5.NewDynamicValue(valueTerraformType, tfConfigValue.Copy())
	if err != nil {
		return &tfprotov5.DynamicValue{}, false, err
	}

	diffs, err := tfStateValue.Diff(tfConfigValue)
	if err != nil {
		return &tfprotov5.DynamicValue{}, false, err
	}

	// process diffs
	processedDiffs := diffs[:0]
	for _, diff := range diffs {
		if !diff.Value2.IsNull() {
			processedDiffs = append(processedDiffs, diff)
		}
	}

	if len(processedDiffs) == 0 {
		return nil, false, nil
	}

	prcReq := &tfprotov5.PlanResourceChangeRequest{
		TypeName:         n.config.Name,
		Config:           &tfConfig,
		ProposedNewState: &tfPlannedState,
	}
	planResponse, err := n.server.PlanResourceChange(ctx, prcReq)
	if err != nil {
		return &tfprotov5.DynamicValue{}, false, errors.Wrap(err, "cannot plan change")
	}
	// TODO: improve diag reporting
	if isFatal := hasFatalDiagnostics(planResponse.Diagnostics); isFatal {
		return &tfprotov5.DynamicValue{}, false, errors.New("plan resource change has fatal diags")
	}

	if len(planResponse.RequiresReplace) > 0 {
		var sb strings.Builder
		sb.WriteString("diff contains fields that require resource replacement: ")
		for _, attrPath := range planResponse.RequiresReplace {
			sb.WriteString(attrPath.String())
			sb.WriteString(", ")
		}
		return nil, false, errors.New(sb.String())
	}

	return planResponse.PlannedState, len(processedDiffs) > 0, nil

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

	// TODO(erhan): improve diag reporting
	if shouldError := hasFatalDiagnostics(readResponse.Diagnostics); shouldError {
		return managed.ExternalObservation{}, errors.New("read returned diags")
	}

	tfStateValue, err := readResponse.NewState.Unmarshal(n.resourceSchema.Type().TerraformType(ctx))
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot unmarshal state value")
	}

	n.opTracker.SetFrameworkTFState(readResponse.NewState)
	resourceExists := !tfStateValue.IsNull()

	var stateValueMap map[string]any
	if resourceExists {
		if conv, err := valueToGo(tfStateValue); err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot convert instance state to JSON map")
		} else {
			stateValueMap = conv.(map[string]any)
		}
	}

	plannedState, hasDiff, err := n.getDiffPlan(ctx, tfStateValue)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot calculate diff")
	}

	n.plannedState = plannedState

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

	configJsonBytes, err := json.Marshal(n.params)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot convert params to json bytes")
	}

	applyRequest := &tfprotov5.ApplyResourceChangeRequest{
		TypeName:     n.config.Name,
		PriorState:   n.opTracker.GetFrameworkTFState(),
		PlannedState: n.plannedState,
		Config: &tfprotov5.DynamicValue{
			JSON: configJsonBytes,
		},
	}
	start := time.Now()
	applyResponse, err := n.server.ApplyResourceChange(ctx, applyRequest)
	metrics.ExternalAPITime.WithLabelValues("create").Observe(time.Since(start).Seconds())
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot create resource")
	}

	// TODO(erhan): check diags reporting
	if fatal := hasFatalDiagnostics(applyResponse.Diagnostics); fatal {
		return managed.ExternalCreation{}, errors.Errorf("failed to create the resource:")
	}

	// TODO(erhan): refactor schema
	res := *n.resource
	schemaResp := &fwresource.SchemaResponse{}
	res.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)

	newStateAfterApplyVal, err := applyResponse.NewState.Unmarshal(schemaResp.Schema.Type().TerraformType(ctx))
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot unmarshal planned state")
	}

	// TODO(erhan): check if new state ID is available
	if newStateAfterApplyVal.IsNull() {
		return managed.ExternalCreation{}, errors.New("new state is empty after creation")
	}

	var stateValueMap map[string]any
	if goval, err := valueToGo(newStateAfterApplyVal); err != nil {
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

	// TODO(erhan): check config.Sensitive
	conn, err := resource.GetConnectionDetails(stateValueMap, mg.(resource.Terraformed), n.config)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot get connection details")
	}

	return managed.ExternalCreation{ConnectionDetails: conn}, nil
}

func (n *terraformPluginFrameworkExternalClient) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	n.logger.Debug("Updating the external resource")

	configJsonBytes, err := json.Marshal(n.params)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot convert params to json bytes")
	}

	applyRequest := &tfprotov5.ApplyResourceChangeRequest{
		TypeName:     n.config.Name,
		PriorState:   n.opTracker.GetFrameworkTFState(),
		PlannedState: n.plannedState,
		Config: &tfprotov5.DynamicValue{
			JSON: configJsonBytes,
		},
	}
	start := time.Now()
	applyResponse, err := n.server.ApplyResourceChange(ctx, applyRequest)
	metrics.ExternalAPITime.WithLabelValues("update").Observe(time.Since(start).Seconds())
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot update resource")
	}
	if fatal := hasFatalDiagnostics(applyResponse.Diagnostics); fatal {
		return managed.ExternalUpdate{}, errors.Errorf("failed to create the resource:")
	}
	n.opTracker.SetFrameworkTFState(applyResponse.NewState)

	newStateAfterApplyVal, err := applyResponse.NewState.Unmarshal(n.resourceSchema.Type().TerraformType(ctx))
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot unmarshal updated state")
	}

	if newStateAfterApplyVal.IsNull() {
		return managed.ExternalUpdate{}, errors.New("new state is empty after update")
	}

	var stateValueMap map[string]any
	if goval, err := valueToGo(newStateAfterApplyVal); err != nil {
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
	configJsonBytes, err := json.Marshal(n.params)
	if err != nil {
		return errors.Wrap(err, "cannot convert params to json bytes")
	}

	schemaType := n.resourceSchema.Type().TerraformType(ctx)
	// set an empty planned state, this corresponds to deleting
	plannedState, err := tfprotov5.NewDynamicValue(schemaType, tftypes.NewValue(schemaType, nil))
	if err != nil {
		return errors.Wrap(err, "cannot set the planned state")
	}

	applyRequest := &tfprotov5.ApplyResourceChangeRequest{
		TypeName:     n.config.Name,
		PriorState:   n.opTracker.GetFrameworkTFState(),
		PlannedState: &plannedState,
		Config: &tfprotov5.DynamicValue{
			JSON: configJsonBytes,
		},
	}
	start := time.Now()
	applyResponse, err := n.server.ApplyResourceChange(ctx, applyRequest)
	metrics.ExternalAPITime.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	if err != nil {
		return errors.Wrap(err, "cannot delete resource")
	}
	// TODO(erhan): improve diagnostics reporting
	if fatal := hasFatalDiagnostics(applyResponse.Diagnostics); fatal {
		return errors.Errorf("failed to delete the resource with diags")
	}
	n.opTracker.SetFrameworkTFState(applyResponse.NewState)

	newStateAfterApplyVal, err := applyResponse.NewState.Unmarshal(schemaType)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal updated state")
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

func valueToGo(input tftypes.Value) (any, error) {
	if input.IsNull() {
		return nil, nil
	}
	valType := input.Type()
	if valType.Is(tftypes.Object{}) || valType.Is(tftypes.Map{}) {
		destInterim := make(map[string]tftypes.Value)
		dest := make(map[string]any)
		if err := input.As(&destInterim); err != nil {
			return nil, err
		}
		for k, v := range destInterim {
			if res, err := valueToGo(v); err != nil {
				return nil, err
			} else {
				dest[k] = res
			}
		}
		return dest, nil
	} else if valType.Is(tftypes.Set{}) || valType.Is(tftypes.List{}) || valType.Is(tftypes.Tuple{}) {
		destInterim := make([]tftypes.Value, 0)
		dest := make([]any, 0)
		if err := input.As(&destInterim); err != nil {
			return nil, err
		}
		for i, v := range destInterim {
			if res, err := valueToGo(v); err != nil {
				return nil, err
			} else {
				dest[i] = res
			}
		}
		return dest, nil
	} else if valType.Is(tftypes.Bool) {
		var x bool
		if err := input.As(&x); err != nil {
			return nil, err
		}
		return x, nil
	} else if valType.Is(tftypes.Number) {
		var x big.Float
		if err := input.As(&x); err != nil {
			return nil, err
		}
		xf, _ := x.Float64()
		return xf, nil
	} else if valType.Is(tftypes.String) {
		var x string
		if err := input.As(&x); err != nil {
			return nil, err
		}
		return x, nil
	} else if valType.Is(tftypes.DynamicPseudoType) {
		// TODO: check if we need to handle DynamicPseudoType
		return nil, nil
	} else {
		return nil, nil
	}
}

func hasFatalDiagnostics(diags []*tfprotov5.Diagnostic) bool {
	shouldError := false
	for _, tfdiag := range diags {
		if tfdiag.Severity == tfprotov5.DiagnosticSeverityInvalid || tfdiag.Severity == tfprotov5.DiagnosticSeverityError {
			shouldError = true
		}
	}
	return shouldError
}
