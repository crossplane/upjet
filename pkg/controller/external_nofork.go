// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/metrics"
	"github.com/crossplane/upjet/pkg/resource"
	"github.com/crossplane/upjet/pkg/resource/json"
	"github.com/crossplane/upjet/pkg/terraform"
)

type NoForkConnector struct {
	getTerraformSetup           terraform.SetupFn
	kube                        client.Client
	config                      *config.Resource
	logger                      logging.Logger
	metricRecorder              *metrics.MetricRecorder
	operationTrackerStore       *OperationTrackerStore
	isManagementPoliciesEnabled bool
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

// WithNoForkManagementPolicies configures whether the client should
// handle management policies.
func WithNoForkManagementPolicies(isManagementPoliciesEnabled bool) NoForkOption {
	return func(c *NoForkConnector) {
		c.isManagementPoliciesEnabled = isManagementPoliciesEnabled
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
	instanceDiff   *tf.InstanceDiff
	params         map[string]any
	rawConfig      cty.Value
	logger         logging.Logger
	metricRecorder *metrics.MetricRecorder
	opTracker      *AsyncTracker
}

func getExtendedParameters(ctx context.Context, tr resource.Terraformed, externalName string, config *config.Resource, ts terraform.Setup, initParamsMerged bool, kube client.Client) (map[string]any, error) {
	params, err := tr.GetMergedParameters(initParamsMerged)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get merged parameters")
	}
	if err = resource.GetSensitiveParameters(ctx, &APISecretClient{kube: kube}, tr, params, tr.GetConnectionDetailsMapping()); err != nil {
		return nil, errors.Wrap(err, "cannot store sensitive parameters into params")
	}
	config.ExternalName.SetIdentifierArgumentFn(params, externalName)
	if config.TerraformConfigurationInjector != nil {
		m, err := getJSONMap(tr)
		if err != nil {
			return nil, errors.Wrap(err, "cannot get JSON map for the managed resource's spec.forProvider value")
		}
		config.TerraformConfigurationInjector(m, params)
	}

	tfID, err := config.ExternalName.GetIDFn(ctx, externalName, params, ts.Map())
	if err != nil {
		return nil, errors.Wrap(err, "cannot get ID")
	}
	params["id"] = tfID
	// we need to parameterize the following for a provider
	// not all providers may have this attribute
	// TODO: tags_all handling
	if _, ok := config.TerraformResource.CoreConfigSchema().Attributes["tags_all"]; ok {
		params["tags_all"] = params["tags"]
	}
	return params, nil
}

func (c *NoForkConnector) processParamsWithStateFunc(schemaMap map[string]*schema.Schema, params map[string]any) map[string]any {
	for key, param := range params {
		if sc, ok := schemaMap[key]; ok {
			params[key] = c.applyStateFuncToParam(sc, param)
		} else {
			params[key] = param
		}
	}
	return params
}

func (c *NoForkConnector) applyStateFuncToParam(sc *schema.Schema, param any) any { //nolint:gocyclo
	switch sc.Type {
	case schema.TypeMap:
		if sc.Elem == nil {
			return param
		}
		// TypeMap only supports schema in Elem
		if mapSchema, ok := sc.Elem.(*schema.Schema); ok {
			pmap := param.(map[string]any)
			for pk, pv := range pmap {
				pmap[pk] = c.applyStateFuncToParam(mapSchema, pv)
			}
			return pmap
		}
	case schema.TypeSet, schema.TypeList:
		if sc.Elem == nil {
			return param
		}
		pArray := param.([]any)
		if setSchema, ok := sc.Elem.(*schema.Schema); ok {
			for i, p := range pArray {
				pArray[i] = c.applyStateFuncToParam(setSchema, p)
			}
			return pArray
		} else if setResource, ok := sc.Elem.(*schema.Resource); ok {
			for i, p := range pArray {
				resParam := p.(map[string]any)
				pArray[i] = c.processParamsWithStateFunc(setResource.Schema, resParam)
			}
		}
	case schema.TypeBool, schema.TypeInt, schema.TypeFloat, schema.TypeString:
		if sc.StateFunc != nil {
			return sc.StateFunc(param)
		}
		return param
	case schema.TypeInvalid:
		return param
	default:
		return param
	}
	return param
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
	params, err := getExtendedParameters(ctx, tr, externalName, c.config, ts, c.isManagementPoliciesEnabled, c.kube)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get the extended parameters for resource %q", mg.GetName())
	}
	params = c.processParamsWithStateFunc(c.config.TerraformResource.Schema, params)

	schemaBlock := c.config.TerraformResource.CoreConfigSchema()
	rawConfig, err := schema.JSONMapToStateValue(params, schemaBlock)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert params JSON map to cty.Value")
	}
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
		tfState["id"] = params["id"]
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
		s.RawConfig = rawConfig
		opTracker.SetTfState(s)
	}

	return &noForkExternal{
		ts:             ts,
		resourceSchema: c.config.TerraformResource,
		config:         c.config,
		params:         params,
		rawConfig:      rawConfig,
		logger:         logger,
		metricRecorder: c.metricRecorder,
		opTracker:      opTracker,
	}, nil
}

func deleteInstanceDiffAttribute(instanceDiff *tf.InstanceDiff, paramKey string) error {
	delete(instanceDiff.Attributes, paramKey)

	keyComponents := strings.Split(paramKey, ".")
	if len(keyComponents) < 2 {
		return nil
	}

	keyComponents[len(keyComponents)-1] = "%"
	lengthKey := strings.Join(keyComponents, ".")
	if lengthValue, ok := instanceDiff.Attributes[lengthKey]; ok {
		newValue, err := strconv.Atoi(lengthValue.New)
		if err != nil {
			return errors.Wrapf(err, "cannot convert instance diff attribute %q to integer", lengthValue.New)
		}

		// TODO: consider what happens if oldValue = ""
		oldValue, err := strconv.Atoi(lengthValue.Old)
		if err != nil {
			return errors.Wrapf(err, "cannot convert instance diff attribute %q to integer", lengthValue.Old)
		}

		newValue -= 1
		if oldValue == newValue {
			delete(instanceDiff.Attributes, lengthKey)
		} else {
			// TODO: consider what happens if oldValue = ""
			lengthValue.New = strconv.Itoa(newValue)
		}
	}

	return nil
}

func filterInitExclusiveDiffs(tr resource.Terraformed, instanceDiff *tf.InstanceDiff) error { //nolint:gocyclo
	if instanceDiff == nil || instanceDiff.Empty() {
		return nil
	}
	paramsForProvider, err := tr.GetParameters()
	if err != nil {
		return errors.Wrap(err, "cannot get spec.forProvider parameters")
	}
	paramsInitProvider, err := tr.GetInitParameters()
	if err != nil {
		return errors.Wrap(err, "cannot get spec.initProvider parameters")
	}
	initProviderExclusiveParamKeys := getTerraformIgnoreChanges(paramsForProvider, paramsInitProvider)

	for _, keyToIgnore := range initProviderExclusiveParamKeys {
		for attributeKey := range instanceDiff.Attributes {
			if keyToIgnore != attributeKey {
				continue
			}

			if err := deleteInstanceDiffAttribute(instanceDiff, keyToIgnore); err != nil {
				return errors.Wrapf(err, "cannot delete key %q from instance diff", keyToIgnore)
			}

			keyComponents := strings.Split(keyToIgnore, ".")
			if keyComponents[0] != "tags" {
				continue
			}
			keyComponents[0] = "tags_all"
			keyToIgnore = strings.Join(keyComponents, ".")
			if err := deleteInstanceDiffAttribute(instanceDiff, keyToIgnore); err != nil {
				return errors.Wrapf(err, "cannot delete key %q from instance diff", keyToIgnore)
			}
		}
	}
	return nil
}

func (n *noForkExternal) getResourceDataDiff(tr resource.Terraformed, ctx context.Context, s *tf.InstanceState, resourceExists bool) (*tf.InstanceDiff, error) {
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

	if resourceExists {
		if err := filterInitExclusiveDiffs(tr, instanceDiff); err != nil {
			return nil, errors.Wrap(err, "failed to filter the diffs exclusive to spec.initProvider in the terraform.InstanceDiff")
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
	if instanceDiff != nil && !instanceDiff.Empty() {
		n.logger.Debug("Diff detected", "instanceDiff", instanceDiff.GoString())
		// Assumption: Source of truth when applying diffs, for instance on updates, is instanceDiff.Attributes.
		// Setting instanceDiff.RawConfig has no effect on diff application.
		instanceDiff.RawConfig = n.rawConfig
	}
	return instanceDiff, nil
}

func (n *noForkExternal) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) { //nolint:gocyclo
	n.logger.Debug("Observing the external resource")

	if meta.WasDeleted(mg) && n.opTracker.IsDeleted() {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	start := time.Now()
	newState, diag := n.resourceSchema.RefreshWithoutUpgrade(ctx, n.opTracker.GetTfState(), n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("read").Observe(time.Since(start).Seconds())
	if diag != nil && diag.HasError() {
		return managed.ExternalObservation{}, errors.Errorf("failed to observe the resource: %v", diag)
	}
	n.opTracker.SetTfState(newState) // TODO: missing RawConfig & RawPlan here...

	resourceExists := newState != nil && newState.ID != ""
	instanceDiff, err := n.getResourceDataDiff(mg.(resource.Terraformed), ctx, newState, resourceExists)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot compute the instance diff")
	}
	n.instanceDiff = instanceDiff
	noDiff := instanceDiff.Empty()

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

func (n *noForkExternal) assertNoForceNew() error {
	if n.instanceDiff == nil {
		return nil
	}
	for k, ad := range n.instanceDiff.Attributes {
		if ad == nil {
			continue
		}
		// TODO: use a multi-error implementation to report changes to
		//  all `ForceNew` arguments.
		if ad.RequiresNew {
			if ad.Sensitive {
				return errors.Errorf("cannot change the value of the argument %q", k)
			}
			return errors.Errorf("cannot change the value of the argument %q from %q to %q", k, ad.Old, ad.New)
		}
	}
	return nil
}

func (n *noForkExternal) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	n.logger.Debug("Updating the external resource")

	if err := n.assertNoForceNew(); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "refuse to update the external resource")
	}

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
	// mark the resource as logically deleted if the TF call clears the state
	n.opTracker.SetDeleted(newState == nil)
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

// getTerraformIgnoreChanges returns a sorted Terraform `ignore_changes`
// lifecycle meta-argument expression by looking for differences between
// the `initProvider` and `forProvider` maps. The ignored fields are the ones
// that are present in initProvider, but not in forProvider.
// TODO: This method is copy-pasted from `pkg/resource/ignored.go` and adapted.
// Consider merging this implementation with the original one.
func getTerraformIgnoreChanges(forProvider, initProvider map[string]any) []string {
	ignored := getIgnoredFieldsMap("%s", forProvider, initProvider)
	return ignored
}

// TODO: This method is copy-pasted from `pkg/resource/ignored.go` and adapted.
// Consider merging this implementation with the original one.
func getIgnoredFieldsMap(format string, forProvider, initProvider map[string]any) []string {
	ignored := []string{}

	for k := range initProvider {
		if _, ok := forProvider[k]; !ok {
			ignored = append(ignored, fmt.Sprintf(format, k))
		} else {
			// both are the same type so we dont need to check for forProvider type
			if _, ok = initProvider[k].(map[string]any); ok {
				ignored = append(ignored, getIgnoredFieldsMap(fmt.Sprintf(format, k)+".%v", forProvider[k].(map[string]any), initProvider[k].(map[string]any))...)
			}
			// if its an array, we need to check if its an array of maps or not
			if _, ok = initProvider[k].([]any); ok {
				ignored = append(ignored, getIgnoredFieldsArray(fmt.Sprintf(format, k), forProvider[k].([]any), initProvider[k].([]any))...)
			}

		}
	}
	return ignored
}

// TODO: This method is copy-pasted from `pkg/resource/ignored.go` and adapted.
// Consider merging this implementation with the original one.
func getIgnoredFieldsArray(format string, forProvider, initProvider []any) []string {
	ignored := []string{}
	for i := range initProvider {
		// Construct the full field path with array index and prefix.
		fieldPath := fmt.Sprintf("%s[%d]", format, i)
		if i < len(forProvider) {
			if _, ok := initProvider[i].(map[string]any); ok {
				ignored = append(ignored, getIgnoredFieldsMap(fieldPath+".%s", forProvider[i].(map[string]any), initProvider[i].(map[string]any))...)
			}
			if _, ok := initProvider[i].([]any); ok {
				ignored = append(ignored, getIgnoredFieldsArray(fieldPath+"%s", forProvider[i].([]any), initProvider[i].([]any))...)
			}
		} else {
			ignored = append(ignored, fieldPath)
		}

	}
	return ignored
}
