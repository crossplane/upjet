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

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
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
	observationMode             ObservationMode
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

// WithTerraformPluginFrameworkObservationMode configures how the client
// observes the external resource. The default, ReadExternalResource, reads it
// from the provider's API. UseLocalState instead observes the Terraform state
// the operation tracker already holds, so that a caller which supplies that
// state itself, such as the diff server, never reaches the API.
func WithTerraformPluginFrameworkObservationMode(m ObservationMode) TerraformPluginFrameworkConnectorOption {
	return func(c *TerraformPluginFrameworkConnector) {
		c.observationMode = m
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
		observationMode:       ReadExternalResource,
	}
	for _, f := range opts {
		f(connector)
	}
	return connector
}

type terraformPluginFrameworkExternalClient struct {
	ts              terraform.Setup
	config          *config.Resource
	logger          logging.Logger
	metricRecorder  *metrics.MetricRecorder
	opTracker       *AsyncTracker
	resource        fwresource.Resource
	server          tfprotov6.ProviderServer
	params          map[string]any
	planResponse    *tfprotov6.PlanResourceChangeResponse
	plannedIdentity *tfprotov6.ResourceIdentityData
	resourceSchema  rschema.Schema
	// the terraform value type associated with the resource schema
	resourceValueTerraformType tftypes.Type
	// configured value for the resource in terraform type system
	resourceTerraformConfigValue tftypes.Value
	observationMode              ObservationMode
	isManagementPoliciesEnabled  bool
}

// TerraformPluginFrameworkPlanResponse returns the plan the given external
// client computed during its last Observe. It is the Terraform Plugin
// Framework counterpart of TerraformPluginSDKInstanceDiff.
func TerraformPluginFrameworkPlanResponse(ec managed.ExternalClient) (*tfprotov6.PlanResourceChangeResponse, error) {
	n, ok := ec.(*terraformPluginFrameworkExternalClient)
	if !ok {
		return nil, errors.New("not a Terraform plugin Framework external client")
	}
	return n.planResponse, nil
}

// supportsIdentity reports whether the underlying TF resource implements
// fwresource.ResourceWithIdentity and therefore participates in identity
// propagation through Read/Plan/Apply cycles.
func (n *terraformPluginFrameworkExternalClient) supportsIdentity() bool {
	_, ok := n.resource.(fwresource.ResourceWithIdentity)
	return ok
}

func getFrameworkExtendedParameters(ctx context.Context, tr resource.Terraformed, externalName string, cfg *config.Resource, ts terraform.Setup, initParamsMerged bool, kube client.Client, fwResSchema rschema.Schema) (map[string]any, error) { //nolint:gocyclo // easier to follow as a unit
	var err error
	var params map[string]any                          // Assigned by both branches; functions return non-nil on success
	if cfg.ControllerReconcileVersion == cfg.Version { //nolint:staticcheck // still handling deprecated field behavior
		params, err = tr.GetMergedParameters(initParamsMerged)
		if err != nil {
			return nil, errors.Wrap(err, "cannot get merged parameters")
		}
	} else {
		params, err = mergeAnnotationFieldsWithSpec(tr, initParamsMerged, tr.GetAnnotations())
		if err != nil {
			return nil, errors.Wrap(err, "cannot merge annotation fields")
		}
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
func (c *TerraformPluginFrameworkConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	c.metricRecorder.ObserveReconcileDelay(mg.GetObjectKind().GroupVersionKind(), metrics.NameForManaged(mg))
	logger := c.logger.WithValues("uid", mg.GetUID(), "name", mg.GetName(), "namespace", mg.GetNamespace(), "gvk", mg.GetObjectKind().GroupVersionKind().String())
	tr := mg.(resource.Terraformed)
	ts, params, resourceSchema, resourceConfigTFValue, err := c.ReconstructFrameworkTerraformState(ctx, tr, logger)
	if err != nil {
		return nil, err
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
		opTracker:                    c.operationTrackerStore.Tracker(tr),
		resource:                     c.config.TerraformPluginFrameworkResource,
		server:                       configuredProviderServer,
		params:                       params,
		resourceSchema:               resourceSchema,
		resourceValueTerraformType:   resourceSchema.Type().TerraformType(ctx),
		resourceTerraformConfigValue: resourceConfigTFValue,
		observationMode:              c.observationMode,
		isManagementPoliciesEnabled:  c.isManagementPoliciesEnabled,
	}, nil
}

// ReconstructFrameworkTerraformState reconstructs the Terraform state the
// given managed resource's observation and parameters imply, and stores it on
// the tracker, so that a later Observe or Plan has a prior state to work
// from. An observation that is empty, i.e. a resource that has not been
// created yet, is seeded with the parameters instead. The reconstructed
// state is stored on the tracker rather than returned, and a cached state is
// left untouched.
//
// This is also what the diff server uses to reconstruct an actual resource's
// state under a tracker slot keyed by a different (the desired resource's)
// UID, without paying for a provider server it would immediately discard:
// Connect needs one, to serve Observe and Plan on the connection it returns,
// but a caller that only wants the tracker populated does not.
//
// On error, the returned terraform.Setup is the zero value, and the
// parameter map, schema, and cty.Value are nil or zero valued.
func (c *TerraformPluginFrameworkConnector) ReconstructFrameworkTerraformState(ctx context.Context, tr resource.Terraformed, logger logging.Logger) (terraform.Setup, map[string]any, rschema.Schema, tftypes.Value, error) { //nolint:gocyclo // mirrors the plugin SDKv2 counterpart step for step
	logger.Debug("Connecting to the service provider")
	start := time.Now()
	ts, err := c.getTerraformSetup(ctx, c.kube, tr)
	metrics.ExternalAPITime.WithLabelValues("connect").Observe(time.Since(start).Seconds())
	if err != nil {
		return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrap(err, errGetTerraformSetup)
	}

	opTracker := c.operationTrackerStore.Tracker(tr)
	externalName := meta.GetExternalName(tr)
	resourceSchema, err := c.getResourceSchema(ctx)
	if err != nil {
		return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrap(err, "could not retrieve resource schema")
	}
	params, err := getFrameworkExtendedParameters(ctx, tr, externalName, c.config, ts, c.isManagementPoliciesEnabled, c.kube, resourceSchema)
	if err != nil {
		return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrapf(err, "failed to get the extended parameters for resource %q", client.ObjectKeyFromObject(tr))
	}

	resourceTfValueType := resourceSchema.Type().TerraformType(ctx)
	resourceConfigTFValue, err := getResourceConfigTerraformValue(ctx, c.config, resourceTfValueType, params, resourceSchema)
	if err != nil {
		return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrap(err, "could not get resource config TF value")
	}
	hasState := false
	if opTracker.HasFrameworkTFState() {
		tfStateValue, err := opTracker.GetFrameworkTFState().Unmarshal(resourceTfValueType)
		if err != nil {
			return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrap(err, "cannot unmarshal TF state dynamic value during state existence check")
		}
		hasState = !tfStateValue.IsNull()
	}

	if !hasState {
		logger.Debug("Instance state not found in cache, reconstructing...")
		tfState, err := tr.GetObservation()
		if err != nil {
			return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrap(err, "failed to get the observation")
		}
		if err := mergeAnnotationFieldsWithStatus(tfState, tr.GetAnnotations(), c.config); err != nil {
			return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrapf(err, "failed to merge annotations on resource %q", client.ObjectKeyFromObject(tr))
		}
		tfState, err = c.config.ApplyTFConversions(tfState, config.ToTerraform)
		if err != nil {
			return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrap(err, "failed to run the API converters on the Terraform state")
		}
		// several possibilities for this:
		// - resource is being reconciled for the first time
		// - after initial reconciliation, we failed to set the state
		// - resource is getting imported
		// - previous TF operation returned an empty state
		copyParams := len(tfState) == 0
		if err = resource.GetSensitiveParameters(ctx, &APISecretClient{kube: c.kube}, tr, tfState, tr.GetConnectionDetailsMapping()); err != nil {
			return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrap(err, "cannot store sensitive parameters into tfState")
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
			return terraform.Setup{}, nil, rschema.Schema{}, tftypes.Value{}, errors.Wrap(err, "cannot construct dynamic value for TF state")
		}
		opTracker.SetReconstructedFrameworkTFState(tfStateDynamicValue)
	}

	return ts, params, resourceSchema, resourceConfigTFValue, nil
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

func getResourceConfigTerraformValue(ctx context.Context, cfg *config.Resource, tfType tftypes.Type, params map[string]any, sch rschema.Schema) (tftypes.Value, error) {
	configValues := maps.Clone(params)
	// if some computed identifiers have been configured explicitly,
	// remove them from config.
	for _, id := range cfg.ExternalName.TFPluginFrameworkOptions.ComputedIdentifierAttributes {
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
// Exceptions:
//   - A diff where the prior value (Value2) is non-null and the
//     planned value (Value1) is null, and the diff does not belong to
//     an attribute which is computed-only or one of its ancestors is
//     computed-only. Such a case is taken as a previously set value being
//     unset and is thus, not filtered.
func (n *terraformPluginFrameworkExternalClient) filteredDiffExists(ctx context.Context, rawDiff []tftypes.ValueDiff) bool {
	filteredDiff := make([]tftypes.ValueDiff, 0)
	for _, diff := range rawDiff {
		// Keep diffs where the planned value is non-null and known.
		if diff.Value1 != nil && diff.Value1.IsKnown() && !diff.Value1.IsNull() {
			filteredDiff = append(filteredDiff, diff)
			continue
		}
		// Check if a previously set value is now unset. If not,
		// then it will be filtered out at this stage.
		if diff.Value1 == nil || !diff.Value1.IsNull() || diff.Value2 == nil || diff.Value2.IsNull() {
			continue
		}
		// Check if the attribute at diff path is computed-only, or
		// if one of its ancestors is computed-only. If so, the diff
		// will be filtered out at this stage.
		if n.isUnderComputedOnlyAttribute(ctx, diff.Path) {
			continue
		}
		filteredDiff = append(filteredDiff, diff)
	}
	return len(filteredDiff) > 0
}

// isUnderComputedOnlyAttribute returns true if the attribute itself or
// one of its ancestors is computed-only.
func (n *terraformPluginFrameworkExternalClient) isUnderComputedOnlyAttribute(ctx context.Context, p *tftypes.AttributePath) bool {
	for cur := p; cur != nil && len(cur.Steps()) > 0; cur = cur.WithoutLastStep() {
		attr, err := n.resourceSchema.AttributeAtTerraformPath(ctx, cur)
		if err != nil || attr == nil {
			// Not an attribute, a block or an attribute without a schema, etc.
			// Continue with its parent.
			continue
		}
		if attr.IsComputed() && !attr.IsOptional() {
			return true
		}
	}
	return false
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
	if n.supportsIdentity() {
		prcReq.PriorIdentity = n.opTracker.GetFrameworkIdentity()
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

	priorValue, plannedValue, rawDiff, err := NormalizedDiff(tfStateValue, plannedStateValue)
	if err != nil {
		return nil, false, err
	}

	if err := n.filterRequiresReplace(ctx, planResponse, priorValue, plannedValue); err != nil {
		return nil, false, errors.Wrap(err, "failed to check for required replacement fields")
	}
	if n.supportsIdentity() {
		n.plannedIdentity = planResponse.PlannedIdentity
	}

	return planResponse, n.filteredDiffExists(ctx, rawDiff), nil
}

// filterRequiresReplace checks the TF plan response for fields that require/force resource
// replacement, and filters false-positives. The generated plan sometimes reports a field,
// but the prior and plan values are actually the same.
// The caller normalizes stateValue and plannedValue with nullCollectionsAsEmpty,
// and each path is looked up with its set elements normalized the same way.
func (n *terraformPluginFrameworkExternalClient) filterRequiresReplace(ctx context.Context, planResponse *tfprotov6.PlanResourceChangeResponse, stateValue, plannedValue tftypes.Value) error {
	filtered, err := FilterRequiresReplace(ctx, n.logger, n.resourceSchema, planResponse.RequiresReplace, stateValue, plannedValue)
	if err != nil {
		return err
	}
	planResponse.RequiresReplace = filtered
	return nil
}

// FilterRequiresReplace checks requiresReplace for paths whose prior and
// planned values are actually the same, and filters out those false
// positives. stateValue and plannedValue must already be normalized with
// NormalizedDiff, and each path is looked up with its set elements normalized
// the same way.
//
// This is also what the diff server's Plugin Framework path uses, so that a
// RequiresReplace path it reports agrees with what the reconciler itself
// would, rather than reimplementing this filtering on its own.
func FilterRequiresReplace(ctx context.Context, logger logging.Logger, sch rschema.Schema, requiresReplace []*tftypes.AttributePath, stateValue, plannedValue tftypes.Value) ([]*tftypes.AttributePath, error) { //nolint:gocyclo // easier to follow as a unit
	var filtered []*tftypes.AttributePath
	for _, path := range requiresReplace {
		lookupPath, err := nullCollectionsAsEmptyInPath(path)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot normalize the path %s", path)
		}
		priorValInt, _, errPrior := tftypes.WalkAttributePath(stateValue, lookupPath)
		plannedValInt, _, errPlanned := tftypes.WalkAttributePath(plannedValue, lookupPath)
		if errPrior != nil && errPlanned != nil {
			logger.Debug("upstream TF provider generated an invalid plan")
			continue
		}
		tfType, err := sch.TypeAtTerraformPath(ctx, path)
		if err != nil {
			return nil, errors.New("cannot get the type at path from resource schema: %v")
		}

		priorVal, ok := priorValInt.(tftypes.Value)
		if !ok {
			if priorValInt != nil {
				return nil, fmt.Errorf("cannot convert prior value to tftypes.Value")
			}
			priorVal = tftypes.NewValue(tfType.TerraformType(ctx), nil)
		}

		plannedVal, ok := plannedValInt.(tftypes.Value)
		if !ok {
			if plannedValInt != nil {
				return nil, fmt.Errorf("cannot convert planned value to tftypes.Value")
			}
			plannedVal = tftypes.NewValue(tfType.TerraformType(ctx), nil)
		}
		if !plannedVal.Equal(priorVal) {
			filtered = append(filtered, path)
			continue
		}
		logger.Debug("TF plan reported a diff at path that require resource replacement, but the prior and plan values are equal. Skipping...", "path", path)
	}
	return filtered, nil
}

// NormalizedDiff normalizes the prior and the planned state with
// nullCollectionsAsEmpty and returns them together with the diff between
// them. The prior state rebuilt from a managed resource cannot tell an empty
// collection from an absent one, and the plan carries the framework's empty
// defaults, so both sides are compared with null collections as empty.
//
// This is also what the diff server's Plugin Framework path uses, so that it
// is not caught by the same null-versus-empty noise this was written to fix
// for the reconciler.
func NormalizedDiff(prior, planned tftypes.Value) (tftypes.Value, tftypes.Value, []tftypes.ValueDiff, error) {
	priorValue, err := nullCollectionsAsEmpty(prior)
	if err != nil {
		return tftypes.Value{}, tftypes.Value{}, nil, errors.Wrap(err, "cannot normalize prior state")
	}
	plannedValue, err := nullCollectionsAsEmpty(planned)
	if err != nil {
		return tftypes.Value{}, tftypes.Value{}, nil, errors.Wrap(err, "cannot normalize planned state")
	}
	rawDiff, err := plannedValue.Diff(priorValue)
	if err != nil {
		return tftypes.Value{}, tftypes.Value{}, nil, errors.Wrap(err, "cannot compare prior state and plan")
	}
	return priorValue, plannedValue, rawDiff, nil
}

// nullCollectionsAsEmpty returns a copy of v in which every null list, set or
// map, at any depth, is replaced by an empty collection of the same type.
// Unknown values, non-empty collections and values of other types are kept.
//
// The prior state reconstructed from a managed resource cannot tell an empty
// collection from an absent one: the generated parameters and observation are
// marshalled with omitempty, so an empty collection stored by the provider
// comes back as null after a provider restart or once the status is dropped.
// The plan, on the other hand, carries the framework's empty defaults. A
// null-versus-empty difference between the two is an artifact of the
// reconstruction, not a change the user asked for, so the diff and the
// RequiresReplace paths are evaluated on values normalized this way.
func nullCollectionsAsEmpty(v tftypes.Value) (tftypes.Value, error) {
	return tftypes.Transform(v, func(_ *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if !v.IsNull() {
			return v, nil
		}
		switch v.Type().(type) {
		case tftypes.List, tftypes.Set:
			return tftypes.NewValue(v.Type(), []tftypes.Value{}), nil
		case tftypes.Map:
			return tftypes.NewValue(v.Type(), map[string]tftypes.Value{}), nil
		default:
			return v, nil
		}
	})
}

// nullCollectionsAsEmptyInPath returns a copy of p in which the value of every
// set element step is normalized with nullCollectionsAsEmpty.
//
// A set element step matches only an element equal to its value, and the paths
// that require replacement come from the raw plan, so they have to be
// normalized like the values they are looked up in. This also covers the
// framework building those steps from its internal value, where nested blocks
// are null, while the planned state carries them as empty collections.
func nullCollectionsAsEmptyInPath(p *tftypes.AttributePath) (*tftypes.AttributePath, error) {
	steps := p.Steps()
	for i, step := range steps {
		element, ok := step.(tftypes.ElementKeyValue)
		if !ok {
			continue
		}
		v, err := nullCollectionsAsEmpty(tftypes.Value(element))
		if err != nil {
			return nil, err
		}
		steps[i] = tftypes.ElementKeyValue(v)
	}
	return tftypes.NewAttributePathWithSteps(steps), nil
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

// diagSummaryMissingResourceIdentity is the diagnostic summary returned by
// terraform-plugin-framework when a resource with an IdentitySchema has its
// state set to null (resource deleted externally or not yet created) but the
// provider's identity interceptor skips populating identity for null states.
const diagSummaryMissingResourceIdentity = "Missing Resource Identity After Read"

// hasMissingResourceIdentityDiagnostic checks whether the diagnostics contain
// the "Missing Resource Identity After Read" error. We treat this as a
// resource-not-found condition.
func hasMissingResourceIdentityDiagnostic(diags []*tfprotov6.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError && d.Summary == diagSummaryMissingResourceIdentity {
			return true
		}
	}
	return false
}

// localState returns the Terraform state the operation tracker holds, for an
// external client observing in UseLocalState mode. Nothing is read from the
// provider's API.
func (n *terraformPluginFrameworkExternalClient) localState() (tftypes.Value, error) {
	state := n.opTracker.GetFrameworkTFState()
	if state == nil {
		return tftypes.NewValue(n.resourceValueTerraformType, nil), nil
	}
	v, err := state.Unmarshal(n.resourceValueTerraformType)
	return v, errors.Wrap(err, "cannot unmarshal the local Terraform state")
}

// readState reads the external resource through the Terraform provider and
// returns its state, recording it on the operation tracker.
func (n *terraformPluginFrameworkExternalClient) readState(ctx context.Context) (tftypes.Value, error) { //nolint:gocyclo // preserved from Observe, where it used to be inline
	readRequest := &tfprotov6.ReadResourceRequest{
		TypeName:     n.config.Name,
		CurrentState: n.opTracker.GetFrameworkTFState(),
	}
	if n.supportsIdentity() {
		readRequest.CurrentIdentity = n.opTracker.GetFrameworkIdentity()
	}
	readResponse, err := n.server.ReadResource(ctx, readRequest)
	if err != nil {
		n.opTracker.ResetReconstructedFrameworkTFState()
		return tftypes.Value{}, errors.Wrap(err, "cannot read resource")
	}

	// Some Terraform resource implementations return SeverityError diagnostics
	// in case of the resource not found. We check here whether we should
	// suppress them if the resource has such configuration.
	isResourceNotFoundDiags := n.hasResourceNotFoundDiagnostic(readResponse.Diagnostics)
	if fatalDiags := getFatalDiagnostics(readResponse.Diagnostics); fatalDiags != nil {
		isMissingIdentityDiags := n.supportsIdentity() && hasMissingResourceIdentityDiagnostic(readResponse.Diagnostics)
		if !isResourceNotFoundDiags && !isMissingIdentityDiags {
			n.opTracker.ResetReconstructedFrameworkTFState()
			return tftypes.Value{}, errors.Wrap(fatalDiags, "read resource request failed")
		}
		if isResourceNotFoundDiags {
			n.logger.Debug("TF ReadResource returned error diagnostics, but XP resource was configured to treat them as `Resource not exists`. Skipping", "skippedDiags", fatalDiags)
		}
		if isMissingIdentityDiags {
			n.logger.Debug("TF ReadResource returned Missing Resource Identity diagnostic, treating as resource not found", "skippedDiags", fatalDiags)
			isResourceNotFoundDiags = true
		}
	}

	if isResourceNotFoundDiags {
		// we nullify the state here, because the resource has an explicit
		// configuration that says, these diagnostics actually correspond
		// to a "resource not found" situation.
		tfStateValue := tftypes.NewValue(n.resourceValueTerraformType, nil)
		nildynamicValue, err := tfprotov6.NewDynamicValue(n.resourceValueTerraformType, tfStateValue)
		if err != nil {
			return tftypes.Value{}, errors.Wrap(err, "cannot create nil dynamic value")
		}
		n.opTracker.SetFrameworkTFState(&nildynamicValue)
		if n.supportsIdentity() {
			n.opTracker.SetFrameworkIdentity(nil)
		}
		return tfStateValue, nil
	}

	tfStateValue, err := readResponse.NewState.Unmarshal(n.resourceValueTerraformType)
	if err != nil {
		return tftypes.Value{}, errors.Wrap(err, "cannot unmarshal state value")
	}
	n.opTracker.SetFrameworkTFState(readResponse.NewState)
	if n.supportsIdentity() {
		n.opTracker.SetFrameworkIdentity(readResponse.NewIdentity)
	}
	return tfStateValue, nil
}

func (n *terraformPluginFrameworkExternalClient) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) { //nolint:gocyclo
	n.logger.Debug("Observing the external resource")

	if meta.WasDeleted(mg) && n.opTracker.IsDeleted() {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	var tfStateValue tftypes.Value
	var err error
	switch n.observationMode { //nolint:exhaustive // the default branch covers ReadExternalResource and the zero value
	case UseLocalState:
		// The caller supplied the state to observe, so there is nothing to
		// read. Everything below, the existence check and the plan, is
		// computed against the state the operation tracker already holds.
		tfStateValue, err = n.localState()
	default:
		tfStateValue, err = n.readState(ctx)
	}
	if err != nil {
		return managed.ExternalObservation{}, err
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
				if n.supportsIdentity() {
					n.opTracker.SetFrameworkIdentity(nil)
				}
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
	} else if n.supportsIdentity() {
		// For FW resources that use stub/placeholder identifiers
		// in their external name, the initial Observe returns a
		// "not-found" with nil TF state as expected, however they might
		// return a non-empty TF identity with the placeholder identifier.
		// Discard the TF identity for non-existent resource observations
		// otherwise subsequent reconciles detect a superfluous TF identity
		// change, failing the Observe.
		//
		// This also clears the TF identity for other not-found cases
		// like resource was fully established but was deleted out-of-band.
		// This is fine as Upjet in fact does not rely on TF resource
		// identities and rehydrates them in subsequent reconciles
		n.opTracker.SetFrameworkIdentity(nil)
	}

	// Keep the current values of initProvider-exclusive fields, so that they
	// aren't re-applied/removed on update.
	// This is done against the state returned by Read, so that a resource
	// which doesn't exist is still created with its initProvider values.
	if resourceExists && n.isManagementPoliciesEnabled {
		err := preserveInitProviderExclusiveParams(mg.(resource.Terraformed), n.params, stateValueMap, n.config)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot preserve initProvider-exclusive params")
		}
		n.resourceTerraformConfigValue, err = getResourceConfigTerraformValue(ctx, n.config, n.resourceValueTerraformType, n.params, n.resourceSchema)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "could not get resource config TF value")
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
		if mg.GetCondition(xpv2.TypeReady).Status == corev1.ConditionUnknown ||
			mg.GetCondition(xpv2.TypeReady).Status == corev1.ConditionFalse {
			addTTR(mg)
		}
		mg.SetConditions(xpv2.Available())

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

		policySet := sets.New[xpv2.ManagementAction](mg.(resource.Terraformed).GetManagementPolicies()...)
		policyHasLateInit := policySet.HasAny(xpv2.ManagementActionLateInitialize, xpv2.ManagementActionAll)
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

		obs, err := mg.(resource.Terraformed).GetObservation()
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "could not get observation")
		}
		annotations := mg.GetAnnotations()
		annotationUpdate, err := moveTFStateValuesToAnnotation(stateValueMap, obs, annotations, n.config)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot move status values to annotation")
		}
		if annotationUpdate {
			mg.SetAnnotations(annotations)
		}
		specUpdateRequired = specUpdateRequired || annotationUpdate

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
	if n.supportsIdentity() {
		applyRequest.PlannedIdentity = n.plannedIdentity
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
		// note: do not store returned TF Framework Identity here.
		// https://github.com/crossplane-contrib/provider-upjet-aws/issues/2135
		// The returned TF identity might be partial/garbage (missing some fields).
		// When subsequent observe recovers the resource via state, it detects an
		// superfluous identity change garbage->valid and errors out.
		//
		// Instead, ignore the returned TF identity and let subsequent
		// reconciliations rehydrate TF identity via state

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
	if n.supportsIdentity() {
		n.opTracker.SetFrameworkIdentity(applyResponse.NewIdentity)
	}

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

	obs, err := mg.(resource.Terraformed).GetObservation()
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "could not get observation")
	}
	annotations := mg.GetAnnotations()
	if annotationUpdate, err := moveTFStateValuesToAnnotation(stateValueMap, obs, annotations, n.config); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot move status values to annotation")
	} else if annotationUpdate {
		mg.SetAnnotations(annotations)
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

func (n *terraformPluginFrameworkExternalClient) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) { //nolint:gocyclo // easier to follow as a unit
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
	if n.supportsIdentity() {
		applyRequest.PlannedIdentity = n.plannedIdentity
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
	if n.supportsIdentity() {
		n.opTracker.SetFrameworkIdentity(applyResponse.NewIdentity)
	}

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

	obs, err := mg.(resource.Terraformed).GetObservation()
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "could not get observation")
	}
	annotations := mg.GetAnnotations()
	if annotationUpdate, err := moveTFStateValuesToAnnotation(stateValueMap, obs, annotations, n.config); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot move status values to annotation")
	} else if annotationUpdate {
		mg.SetAnnotations(annotations)
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
		// PlannedIdentity is intentionally nil for delete: the planned state is
		// null (resource is going away) so there is no planned identity. Passing
		// the update-plan identity stored in n.plannedIdentity would be stale
		// and semantically incorrect.
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
	if n.supportsIdentity() {
		n.opTracker.SetFrameworkIdentity(applyResponse.NewIdentity)
	}

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
		return nil, errors.Wrap(err, "cannot construct TF value from raw map")
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
