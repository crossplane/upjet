// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"math/big"
	"strings"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/metrics"
	"github.com/crossplane/upjet/v2/pkg/resource"
	upjson "github.com/crossplane/upjet/v2/pkg/resource/json"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	tferrors "github.com/crossplane/upjet/v2/pkg/terraform/errors"
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
	server         tfprotov6.ProviderServer
	params         map[string]any
	planResponse   *tfprotov6.PlanResourceChangeResponse
	resourceSchema rschema.Schema
	// the terraform value type associated with the resource schema
	resourceValueTerraformType tftypes.Type
	// configured value for the resource in terraform type system
	resourceTerraformConfigValue tftypes.Value
}

func getFrameworkExtendedParameters(ctx context.Context, tr resource.Terraformed, externalName string, cfg *config.Resource, ts terraform.Setup, initParamsMerged bool, kube client.Client, fwResSchema rschema.Schema) (map[string]any, error) { //nolint:gocyclo // easier to follow as a unit
	params, err := tr.GetMergedParameters(initParamsMerged)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get merged parameters")
	}
	params, err = cfg.ApplyTFConversions(params, config.ToTerraform)
	if err != nil {
		return nil, errors.Wrap(err, "cannot apply tf conversions")
	}
	if err = resource.GetSensitiveParameters(ctx, &APISecretClient{kube: kube}, tr, params, tr.GetConnectionDetailsMapping()); err != nil {
		return nil, errors.Wrap(err, "cannot store sensitive parameters into params")
	}
	cfg.ExternalName.SetIdentifierArgumentFn(params, externalName)
	if cfg.TerraformConfigurationInjector != nil {
		m, err := getJSONMap(tr)
		if err != nil {
			return nil, errors.Wrap(err, "cannot get JSON map for the managed resource's spec.forProvider value")
		}
		if err := cfg.TerraformConfigurationInjector(m, params); err != nil {
			return nil, errors.Wrap(err, "cannot invoke the configured TerraformConfigurationInjector")
		}
	}

	// ID is not necessarily part of the TF framework resources
	// inject it only if it exists in the schema
	if _, ok := fwResSchema.Attributes["id"]; ok {
		tfID, err := cfg.ExternalName.GetIDFn(ctx, externalName, params, ts.Map())
		if err != nil {
			return nil, errors.Wrap(err, "cannot get ID")
		}
		params["id"] = tfID
	}

	return params, nil
}

// Connect makes sure the underlying client is ready to issue requests to the
// provider API.
func (c *TerraformPluginFrameworkConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) { //nolint:gocyclo
	c.metricRecorder.ObserveReconcileDelay(mg.GetObjectKind().GroupVersionKind(), metrics.NameForManaged(mg))
	logger := c.logger.WithValues("uid", mg.GetUID(), "name", mg.GetName(), "namespace", mg.GetNamespace(), "gvk", mg.GetObjectKind().GroupVersionKind().String())
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
	resourceSchema, err := c.getResourceSchema(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "could not retrieve resource schema")
	}
	params, err := getFrameworkExtendedParameters(ctx, tr, externalName, c.config, ts, c.isManagementPoliciesEnabled, c.kube, resourceSchema)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get the extended parameters for resource %q", client.ObjectKeyFromObject(mg))
	}

	resourceTfValueType := resourceSchema.Type().TerraformType(ctx)
	resourceConfigTFValue, err := c.getResourceConfigTerraformValue(ctx, resourceTfValueType, params, resourceSchema)
	if err != nil {
		return nil, errors.Wrap(err, "could not get resource config TF value")
	}
	hasState := false
	if opTracker.HasFrameworkTFState() {
		tfStateValue, err := opTracker.GetFrameworkTFState().Unmarshal(resourceTfValueType)
		if err != nil {
			return nil, errors.Wrap(err, "cannot unmarshal TF state dynamic value during state existence check")
		}
		hasState = !tfStateValue.IsNull()
	}

	if !hasState {
		logger.Debug("Instance state not found in cache, reconstructing...")
		tfState, err := tr.GetObservation()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get the observation")
		}
		tfState, err = c.config.ApplyTFConversions(tfState, config.ToTerraform)
		if err != nil {
			return nil, errors.Wrap(err, "failed to run the API converters on the Terraform state")
		}
		// several possibilities for this:
		// - resource is being reconciled for the first time
		// - after initial reconciliation, we failed to set the state
		// - resource is getting imported
		// - previous TF operation returned an empty state
		copyParams := len(tfState) == 0
		if err = resource.GetSensitiveParameters(ctx, &APISecretClient{kube: c.kube}, tr, tfState, tr.GetConnectionDetailsMapping()); err != nil {
			return nil, errors.Wrap(err, "cannot store sensitive parameters into tfState")
		}
		c.config.ExternalName.SetIdentifierArgumentFn(tfState, externalName)
		_, hasIDInSchema := resourceSchema.GetAttributes()["id"]
		if id, ok := params["id"]; ok && id != nil && id.(string) != "" && hasIDInSchema {
			tfState["id"] = params["id"]
		}
		if copyParams {
			tfState = copyParameters(tfState, params)
		}

		tfStateDynamicValue, err := protov6DynamicValueFromMap(tfState, resourceTfValueType)
		if err != nil {
			return nil, errors.Wrap(err, "cannot construct dynamic value for TF state")
		}
		opTracker.SetReconstructedFrameworkTFState(tfStateDynamicValue)
	}

	configuredProviderServer, err := c.configureProvider(ctx, ts)
	if err != nil {
		return nil, errors.Wrap(err, "could not configure provider server")
	}

	return &terraformPluginFrameworkExternalClient{
		ts:                           ts,
		config:                       c.config,
		logger:                       logger,
		metricRecorder:               c.metricRecorder,
		opTracker:                    opTracker,
		resource:                     c.config.TerraformPluginFrameworkResource,
		server:                       configuredProviderServer,
		params:                       params,
		resourceSchema:               resourceSchema,
		resourceValueTerraformType:   resourceTfValueType,
		resourceTerraformConfigValue: resourceConfigTFValue,
	}, nil
}

// getResourceSchema returns the Terraform Plugin Framework-style resource schema for the configured framework resource on the connector
func (c *TerraformPluginFrameworkConnector) getResourceSchema(ctx context.Context) (rschema.Schema, error) {
	res := c.config.TerraformPluginFrameworkResource
	schemaResp := &fwresource.SchemaResponse{}
	res.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		return rschema.Schema{}, tferrors.FrameworkDiagnosticsError("could not retrieve resource schema", schemaResp.Diagnostics)
	}

	return schemaResp.Schema, nil
}

// configureProvider returns a configured Terraform protocol v5 provider server
// with the preconfigured provider instance in the terraform setup.
// The provider instance used should be already preconfigured
// at the terraform setup layer with the relevant provider meta if needed
// by the provider implementation.
func (c *TerraformPluginFrameworkConnector) configureProvider(ctx context.Context, ts terraform.Setup) (tfprotov6.ProviderServer, error) {
	if ts.FrameworkProvider == nil {
		return nil, fmt.Errorf("cannot retrieve framework provider")
	}

	var schemaResp fwprovider.SchemaResponse
	ts.FrameworkProvider.Schema(ctx, fwprovider.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		return nil, tferrors.FrameworkDiagnosticsError("cannot retrieve provider schema", schemaResp.Diagnostics)
	}
	providerServer := providerserver.NewProtocol6(ts.FrameworkProvider)()

	providerConfigDynamicVal, err := protov6DynamicValueFromMap(ts.Configuration, schemaResp.Schema.Type().TerraformType(ctx))
	if err != nil {
		return nil, errors.Wrap(err, "cannot construct dynamic value for TF provider config")
	}

	configureProviderReq := &tfprotov6.ConfigureProviderRequest{
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

func (c *TerraformPluginFrameworkConnector) getResourceConfigTerraformValue(ctx context.Context, tfType tftypes.Type, params map[string]any, sch rschema.Schema) (tftypes.Value, error) {
	configValues := maps.Clone(params)
	// if some computed identifiers have been configured explicitly,
	// remove them from config.
	for _, id := range c.config.ExternalName.TFPluginFrameworkOptions.ComputedIdentifierAttributes {
		delete(configValues, id)
	}

	tfConfigValue, err := tfValueFromMap(configValues, tfType)
	if err != nil {
		return tftypes.Value{}, errors.Wrap(err, "cannot construct TF value for resource config")
	}

	// remove any computed + not-optional (read-only) attribute from resource config
	// we might still need them at `params` for prior state reconstruction in initial reads,
	// however, they should not exist in the final resource config value sent to TF layer.
	// currently read-only params can end up in the `params` via getFrameworkExtendedParameters:
	// - externalname.SetIdentifierArgumentFn might set some computed identifiers
	// - externalname.GetIDFn, id is mostly read-only
	tfConfigValueClean, err := tftypes.Transform(tfConfigValue, func(path *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if !v.IsKnown() || v.IsNull() {
			return v, nil
		}
		attr, err := sch.AttributeAtTerraformPath(ctx, path)
		if err != nil || attr == nil {
			// Not an attribute, could be an element or attribute of a complex type
			// we can safely ignore them
			return v, nil //nolint:nilerr // intentional per above explanation
		}
		if attr.IsComputed() && !attr.IsOptional() {
			// nullify the value with its own type
			return tftypes.NewValue(v.Type(), nil), nil
		}
		// no-op
		return v, nil
	})
	if err != nil {
		return tftypes.Value{}, errors.Wrap(err, "cannot remove read-only attributes from resource config")
	}
	return tfConfigValueClean, nil
}

// Filter diffs that have unknown plan values, which correspond to
// computed fields, and null plan values, which correspond to
// not-specified fields. Such cases cause unnecessary diff detection
// when only computed attributes or not-specified argument diffs
// exist in the raw diff and no actual diff exists in the
// parametrizable attributes.
func (n *terraformPluginFrameworkExternalClient) filteredDiffExists(rawDiff []tftypes.ValueDiff) bool {
	filteredDiff := make([]tftypes.ValueDiff, 0)
	for _, diff := range rawDiff {
		if diff.Value1 != nil && diff.Value1.IsKnown() && !diff.Value1.IsNull() {
			filteredDiff = append(filteredDiff, diff)
		}
	}
	return len(filteredDiff) > 0
}

// getDiffPlanResponse calls the underlying native TF provider's PlanResourceChange RPC,
// and returns the planned state and whether a diff exists.
// If plan response contains non-empty RequiresReplace (i.e. the resource needs
// to be recreated) an error is returned as Crossplane Resource Model (XRM)
// prohibits resource re-creations and rejects this plan.
func (n *terraformPluginFrameworkExternalClient) getDiffPlanResponse(ctx context.Context, tfStateValue tftypes.Value) (*tfprotov6.PlanResourceChangeResponse, bool, error) {
	tfConfigDynamicVal, err := tfprotov6.NewDynamicValue(n.resourceValueTerraformType, n.resourceTerraformConfigValue.Copy())
	if err != nil {
		return nil, false, errors.Wrap(err, "cannot construct dynamic value for TF Config")
	}

	proposedStateVal := proposedState(n.resourceSchema, tfStateValue, n.resourceTerraformConfigValue) //nolint:contextcheck
	tfProposedStateDynamicVal, err := tfprotov6.NewDynamicValue(n.resourceValueTerraformType, proposedStateVal)
	if err != nil {
		return nil, false, errors.Wrap(err, "cannot construct dynamic value for TF Planned State")
	}

	prcReq := &tfprotov6.PlanResourceChangeRequest{
		TypeName:         n.config.Name,
		PriorState:       n.opTracker.GetFrameworkTFState(),
		Config:           &tfConfigDynamicVal,
		ProposedNewState: &tfProposedStateDynamicVal,
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

	if err := n.filterRequiresReplace(ctx, planResponse, tfStateValue, plannedStateValue); err != nil {
		return nil, false, errors.Wrap(err, "failed to check for required replacement fields")
	}

	return planResponse, n.filteredDiffExists(rawDiff), nil
}

// filterRequiresReplace checks the TF plan response for fields that require/force resource
// replacement, and filters false-positives. The generated plan sometimes reports a field,
// but the prior and plan values are actually the same.
func (n *terraformPluginFrameworkExternalClient) filterRequiresReplace(ctx context.Context, planResponse *tfprotov6.PlanResourceChangeResponse, stateValue, plannedValue tftypes.Value) error {
	var filteredRequiresReplace []*tftypes.AttributePath
	for _, path := range planResponse.RequiresReplace {
		priorValInt, _, errPrior := tftypes.WalkAttributePath(stateValue, path)
		plannedValInt, _, errPlanned := tftypes.WalkAttributePath(plannedValue, path)
		if errPrior != nil && errPlanned != nil {
			n.logger.Debug("upstream TF provider generated an invalid plan")
			continue
		}
		tfType, err := n.resourceSchema.TypeAtTerraformPath(ctx, path)
		if err != nil {
			return errors.New("cannot get the type at path from resource schema: %v")
		}

		priorVal, ok := priorValInt.(tftypes.Value)
		if !ok {
			if priorValInt != nil {
				return fmt.Errorf("cannot convert prior value to tftypes.Value")
			}
			priorVal = tftypes.NewValue(tfType.TerraformType(ctx), nil)
		}

		plannedVal, ok := plannedValInt.(tftypes.Value)
		if !ok {
			if plannedValInt != nil {
				return fmt.Errorf("cannot convert planned value to tftypes.Value")
			}
			plannedVal = tftypes.NewValue(tfType.TerraformType(ctx), nil)
		}
		if !plannedVal.Equal(priorVal) {
			filteredRequiresReplace = append(filteredRequiresReplace, path)
		}
		n.logger.Debug("TF plan reported a diff at path that require resource replacement, but the prior and plan values are equal. Skipping...", "path", path)
	}
	planResponse.RequiresReplace = filteredRequiresReplace
	return nil
}

// recoverExternalName tries to extract the externalname from the current TF state
// and sets it to the runtime MR object. Returns whether the externalname is changed.
func (n *terraformPluginFrameworkExternalClient) recoverExternalName(mg xpresource.Managed) (isChanged bool) {
	if !n.opTracker.HasFrameworkTFState() || meta.GetExternalName(mg) != "" {
		return false
	}
	tfStateValue, err := n.opTracker.GetFrameworkTFState().Unmarshal(n.resourceValueTerraformType)
	if err != nil || tfStateValue.IsNull() {
		return false
	}
	tfStateGoValue, err := tfValueToGoValue(tfStateValue)
	if err != nil {
		return false
	}
	tfStateMap, ok := tfStateGoValue.(map[string]interface{})
	if !ok {
		return false
	}
	changed, err := n.setExternalName(mg, tfStateMap)
	if err != nil {
		return false
	}
	return changed
}

// hasResourceNotFoundDiagnostic checks whether supplied TF ReadResource diagnostics
// corresponds to a non-existent resource, and should be ignored.
func (n *terraformPluginFrameworkExternalClient) hasResourceNotFoundDiagnostic(diags []*tfprotov6.Diagnostic) (shouldSupress bool) {
	if n.config.ExternalName.IsNotFoundDiagnosticFn == nil {
		return false
	}
	return n.config.ExternalName.IsNotFoundDiagnosticFn(diags)
}
func (n *terraformPluginFrameworkExternalClient) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) { //nolint:gocyclo
	n.logger.Debug("Observing the external resource")

	if meta.WasDeleted(mg) && n.opTracker.IsDeleted() {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	readRequest := &tfprotov6.ReadResourceRequest{
		TypeName:     n.config.Name,
		CurrentState: n.opTracker.GetFrameworkTFState(),
	}
	readResponse, err := n.server.ReadResource(ctx, readRequest)
	if err != nil {
		n.opTracker.ResetReconstructedFrameworkTFState()
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot read resource")
	}

	// Some Terraform resource implementations return SeverityError diagnostics
	// in case of the resource not found. We check here whether we should
	// suppress them if the resource has such configuration.
	isResourceNotFoundDiags := n.hasResourceNotFoundDiagnostic(readResponse.Diagnostics)
	if fatalDiags := getFatalDiagnostics(readResponse.Diagnostics); fatalDiags != nil {
		if !isResourceNotFoundDiags {
			n.opTracker.ResetReconstructedFrameworkTFState()
			return managed.ExternalObservation{}, errors.Wrap(fatalDiags, "read resource request failed")
		}
		n.logger.Debug("TF ReadResource returned error diagnostics, but XP resource was configured to treat them as `Resource not exists`. Skipping", "skippedDiags", fatalDiags)
	}

	var tfStateValue tftypes.Value
	if isResourceNotFoundDiags {
		// we nullify the state here, because the resource has an explicit
		// configuration that says, these diagnostics actually correspond
		// to a "resource not found" situation.
		tfStateValue = tftypes.NewValue(n.resourceValueTerraformType, nil)
		nildynamicValue, err := tfprotov6.NewDynamicValue(n.resourceValueTerraformType, tfStateValue)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot create nil dynamic value")
		}
		n.opTracker.SetFrameworkTFState(&nildynamicValue)
	} else {
		tfStateValue, err = readResponse.NewState.Unmarshal(n.resourceValueTerraformType)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot unmarshal state value")
		}
		n.opTracker.SetFrameworkTFState(readResponse.NewState)
	}

	// Determine if the resource exists based on Terraform state
	// Some TF resources return a non-null tftypes.Value, for non-existent
	// external resources. We check for non-null but "empty" state values
	// here.
	resourceExists := false
	if !tfStateValue.IsNull() {
		// Resource state is not null, assume it exists
		resourceExists = true
		// If a custom empty state check function is configured, use it to verify existence
		if n.config.TerraformPluginFrameworkIsStateEmptyFn != nil {
			isEmpty, err := n.config.TerraformPluginFrameworkIsStateEmptyFn(ctx, tfStateValue, n.resourceSchema)
			if err != nil {
				return managed.ExternalObservation{}, errors.Wrap(err, "cannot check if TF State is empty")
			}
			// Override existence based on custom check result
			resourceExists = !isEmpty
			// If custom check determines resource doesn't exist, reset state to nil
			if !resourceExists {
				nilTfValue := tftypes.NewValue(n.resourceValueTerraformType, nil)
				nildynamicValue, err := tfprotov6.NewDynamicValue(n.resourceValueTerraformType, nilTfValue)
				if err != nil {
					return managed.ExternalObservation{}, errors.Wrap(err, "cannot create nil dynamic value")
				}
				n.opTracker.SetFrameworkTFState(&nildynamicValue)
			}
		}
	}

	var stateValueMap map[string]any
	if resourceExists {
		if conv, err := tfValueToGoValue(tfStateValue); err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot convert instance state to JSON map")
		} else {
			stateValueMap = conv.(map[string]any)
		}
	}

	// TODO(cem): Consider skipping diff calculation to avoid potential config
	// validation errors in the import path. See
	// https://github.com/crossplane/upjet/pull/461
	planResponse, hasDiff, err := n.getDiffPlanResponse(ctx, tfStateValue)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot calculate diff")
	}

	n.planResponse = planResponse

	if !resourceExists && mg.GetDeletionTimestamp() != nil {
		gvk := mg.GetObjectKind().GroupVersionKind()
		metrics.DeletionTime.WithLabelValues(gvk.Group, gvk.Version, gvk.Kind).Observe(time.Since(mg.GetDeletionTimestamp().Time).Seconds())
	}

	var connDetails managed.ConnectionDetails
	specUpdateRequired := false
	if resourceExists {
		if mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionUnknown ||
			mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionFalse {
			addTTR(mg)
		}
		mg.SetConditions(xpv1.Available())

		// we get the connection details from the observed state before
		// the conversion because the sensitive paths assume the native Terraform
		// schema.
		connDetails, err = resource.GetConnectionDetails(stateValueMap, mg.(resource.Terraformed), n.config)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot get connection details")
		}

		stateValueMap, err = n.config.ApplyTFConversions(stateValueMap, config.FromTerraform)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot convert the singleton lists in the observed state value map into embedded objects")
		}
		// late-init preparation
		buff, err := upjson.TFParser.Marshal(stateValueMap)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot marshal the attributes of the new state for late-initialization")
		}

		policySet := sets.New[xpv1.ManagementAction](mg.(resource.Terraformed).GetManagementPolicies()...)
		policyHasLateInit := policySet.HasAny(xpv1.ManagementActionLateInitialize, xpv1.ManagementActionAll)
		if policyHasLateInit {
			specUpdateRequired, err = mg.(resource.Terraformed).LateInitialize(buff)
			if err != nil {
				return managed.ExternalObservation{}, errors.Wrap(err, "cannot late-initialize the managed resource")
			}
		}

		err = mg.(resource.Terraformed).SetObservation(stateValueMap)
		if err != nil {
			return managed.ExternalObservation{}, errors.Errorf("could not set observation: %v", err)
		}
		if !hasDiff {
			n.metricRecorder.SetReconcileTime(metrics.NameForManaged(mg))
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

func (n *terraformPluginFrameworkExternalClient) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) { //nolint:gocyclo // easier to follow as a unit
	n.logger.Debug("Creating the external resource")

	tfConfigDynamicVal, err := tfprotov6.NewDynamicValue(n.resourceValueTerraformType, n.resourceTerraformConfigValue.Copy())
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot construct dynamic value for TF Config")
	}

	applyRequest := &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     n.config.Name,
		PriorState:   n.opTracker.GetFrameworkTFState(),
		PlannedState: n.planResponse.PlannedState,
		Config:       &tfConfigDynamicVal,
	}
	start := time.Now()
	applyResponse, err := n.server.ApplyResourceChange(ctx, applyRequest)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot create resource")
	}
	metrics.ExternalAPITime.WithLabelValues("create").Observe(time.Since(start).Seconds())
	if fatalDiags := getFatalDiagnostics(applyResponse.Diagnostics); fatalDiags != nil {
		// Save the (partial) state here as the resource might be
		// actually created in the external service, and the provider returns
		// some identifier field(s), especially for resources that have
		// non-deterministic identifiers (generated by the external service).
		// In the following reconciles, this helps to track the external
		// resource, rather than try to recreate that might cause leaking.
		n.opTracker.SetFrameworkTFState(applyResponse.NewState)
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
	// we get the connection details from the observed state before
	// the conversion because the sensitive paths assume the native Terraform
	// schema.
	conn, err := resource.GetConnectionDetails(stateValueMap, mg.(resource.Terraformed), n.config)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot get connection details")
	}

	stateValueMap, err = n.config.ApplyTFConversions(stateValueMap, config.FromTerraform)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot apply TF conversions to state value map after create")
	}
	err = mg.(resource.Terraformed).SetObservation(stateValueMap)
	if err != nil {
		return managed.ExternalCreation{}, errors.Errorf("could not set observation: %v", err)
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

	tfConfigDynamicVal, err := tfprotov6.NewDynamicValue(n.resourceValueTerraformType, n.resourceTerraformConfigValue.Copy())
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot construct dynamic value for TF Config")
	}

	applyRequest := &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     n.config.Name,
		PriorState:   n.opTracker.GetFrameworkTFState(),
		PlannedState: n.planResponse.PlannedState,
		Config:       &tfConfigDynamicVal,
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

	stateValueMap, err = n.config.ApplyTFConversions(stateValueMap, config.FromTerraform)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot apply TF conversions to state value map after update")
	}

	err = mg.(resource.Terraformed).SetObservation(stateValueMap)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Errorf("could not set observation: %v", err)
	}
	return managed.ExternalUpdate{}, nil
}

func (n *terraformPluginFrameworkExternalClient) Delete(ctx context.Context, _ xpresource.Managed) (managed.ExternalDelete, error) {
	n.logger.Debug("Deleting the external resource")

	tfConfigDynamicVal, err := tfprotov6.NewDynamicValue(n.resourceValueTerraformType, n.resourceTerraformConfigValue.Copy())
	if err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, "cannot construct dynamic value for TF Config")
	}
	// set an empty planned state, this corresponds to deleting
	plannedState, err := tfprotov6.NewDynamicValue(n.resourceValueTerraformType, tftypes.NewValue(n.resourceValueTerraformType, nil))
	if err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, "cannot set the planned state for deletion")
	}

	applyRequest := &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     n.config.Name,
		PriorState:   n.opTracker.GetFrameworkTFState(),
		PlannedState: &plannedState,
		Config:       &tfConfigDynamicVal,
	}
	start := time.Now()
	applyResponse, err := n.server.ApplyResourceChange(ctx, applyRequest)
	if err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, "cannot delete resource")
	}
	metrics.ExternalAPITime.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	if fatalDiags := getFatalDiagnostics(applyResponse.Diagnostics); fatalDiags != nil {
		return managed.ExternalDelete{}, errors.Wrap(fatalDiags, "resource deletion call returned error diags")
	}
	n.opTracker.SetFrameworkTFState(applyResponse.NewState)

	newStateAfterApplyVal, err := applyResponse.NewState.Unmarshal(n.resourceValueTerraformType)
	if err != nil {
		return managed.ExternalDelete{}, errors.Wrap(err, "cannot unmarshal state after deletion")
	}
	// mark the resource as logically deleted if the TF call clears the state
	n.opTracker.SetDeleted(newStateAfterApplyVal.IsNull())

	return managed.ExternalDelete{}, nil
}

func (n *terraformPluginFrameworkExternalClient) setExternalName(mg xpresource.Managed, stateValueMap map[string]interface{}) (bool, error) {
	newName, err := n.config.ExternalName.GetExternalNameFn(stateValueMap)
	if err != nil {
		return false, errors.Wrap(err, "failed to compute the external-name from the state map")
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
		if input.IsKnown() && input.IsNull() {
			return nil, nil
		}
		return nil, errors.New("DynamicPseudoType conversion is not supported")
	default:
		return nil, fmt.Errorf("input value has unknown type: %s", valType.String())
	}
}

// getFatalDiagnostics traverses the given Terraform protov6 diagnostics type
// and constructs a Go error. If the provided diag slice is empty, returns nil.
func getFatalDiagnostics(diags []*tfprotov6.Diagnostic) error {
	var errs error
	var diagErrors []string
	for _, tfdiag := range diags {
		if tfdiag.Severity == tfprotov6.DiagnosticSeverityInvalid || tfdiag.Severity == tfprotov6.DiagnosticSeverityError {
			diagErrors = append(diagErrors, fmt.Sprintf("%s: %s", tfdiag.Summary, tfdiag.Detail))
		}
	}
	if len(diagErrors) > 0 {
		errs = errors.New(strings.Join(diagErrors, "\n"))
	}
	return errs
}

// protov6DynamicValueFromMap constructs a protov6 DynamicValue given the
// map[string]any using the terraform type as reference.
func protov6DynamicValueFromMap(data map[string]any, terraformType tftypes.Type) (*tfprotov6.DynamicValue, error) {
	tfValue, err := tfValueFromMap(data, terraformType)
	if err != nil {
		return nil, err
	}
	dynamicValue, err := tfprotov6.NewDynamicValue(terraformType, tfValue)
	if err != nil {
		return nil, errors.Wrap(err, "cannot construct dynamic value from tf value")
	}
	return &dynamicValue, nil
}

// tfValueFromMap constructs a tftypes.Value given the map[string]any
// representation using the terraform type as reference.
func tfValueFromMap(data map[string]any, terraformType tftypes.Type) (tftypes.Value, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return tftypes.Value{}, errors.Wrap(err, "cannot marshal json")
	}
	tfValue, err := tftypes.ValueFromJSONWithOpts(jsonBytes, terraformType, tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true})
	if err != nil {
		return tftypes.Value{}, errors.Wrap(err, "cannot construct tf value from json")
	}
	return tfValue, nil
}

func (n *terraformPluginFrameworkExternalClient) Disconnect(_ context.Context) error {
	return nil
}
