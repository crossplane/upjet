// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/hashicorp/go-cty/cty"
	tfdiag "github.com/hashicorp/terraform-plugin-sdk/v2/diag"
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

type Resource interface {
	Apply(ctx context.Context, s *tf.InstanceState, d *tf.InstanceDiff, meta interface{}) (*tf.InstanceState, tfdiag.Diagnostics)
	RefreshWithoutUpgrade(ctx context.Context, s *tf.InstanceState, meta interface{}) (*tf.InstanceState, tfdiag.Diagnostics)
}

type noForkExternal struct {
	ts             terraform.Setup
	resourceSchema Resource
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
	// TODO: tags-tags_all implementation is AWS specific.
	// Consider making this logic independent of provider.
	if _, ok := config.TerraformResource.CoreConfigSchema().Attributes["tags_all"]; ok {
		params["tags_all"] = params["tags"]
	}
	return params, nil
}

func (c *NoForkConnector) processParamsWithStateFunc(schemaMap map[string]*schema.Schema, params map[string]any) map[string]any {
	if params == nil {
		return params
	}
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
	if param == nil {
		return param
	}
	switch sc.Type {
	case schema.TypeMap:
		if sc.Elem == nil {
			return param
		}
		pmap, okParam := param.(map[string]any)
		// TypeMap only supports schema in Elem
		if mapSchema, ok := sc.Elem.(*schema.Schema); ok && okParam {
			for pk, pv := range pmap {
				pmap[pk] = c.applyStateFuncToParam(mapSchema, pv)
			}
			return pmap
		}
	case schema.TypeSet, schema.TypeList:
		if sc.Elem == nil {
			return param
		}
		pArray, okParam := param.([]any)
		if setSchema, ok := sc.Elem.(*schema.Schema); ok && okParam {
			for i, p := range pArray {
				pArray[i] = c.applyStateFuncToParam(setSchema, p)
			}
			return pArray
		} else if setResource, ok := sc.Elem.(*schema.Resource); ok {
			for i, p := range pArray {
				if resParam, okRParam := p.(map[string]any); okRParam {
					pArray[i] = c.processParamsWithStateFunc(setResource.Schema, resParam)
				}
			}
		}
	case schema.TypeString:
		// For String types check if it is an HCL string and process
		if isHCLSnippetPattern.MatchString(param.(string)) {
			hclProccessedParam, err := processHCLParam(param.(string))
			if err != nil {
				c.logger.Debug("could not process param, returning original", "param", sc.GoString())
			} else {
				param = hclProccessedParam
			}
		}
		if sc.StateFunc != nil {
			return sc.StateFunc(param)
		}
		return param
	case schema.TypeBool, schema.TypeInt, schema.TypeFloat:
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

func (c *NoForkConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) { //nolint:gocyclo
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

		timeouts := getTimeoutParameters(c.config)
		if len(timeouts) > 0 {
			if s == nil {
				s = &tf.InstanceState{}
			}
			if s.Meta == nil {
				s.Meta = make(map[string]interface{})
			}
			s.Meta[schema.TimeoutKey] = timeouts
		}
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
			keyToIgnoreAsPrefix := fmt.Sprintf("%s.", keyToIgnore)
			if keyToIgnore != attributeKey && !strings.HasPrefix(attributeKey, keyToIgnoreAsPrefix) {
				continue
			}

			delete(instanceDiff.Attributes, attributeKey)

			// TODO: tags-tags_all implementation is AWS specific.
			// Consider making this logic independent of provider.
			keyComponents := strings.Split(attributeKey, ".")
			if keyComponents[0] != "tags" {
				continue
			}
			keyComponents[0] = "tags_all"
			tagsAllAttributeKey := strings.Join(keyComponents, ".")
			delete(instanceDiff.Attributes, tagsAllAttributeKey)
		}
	}

	// Delete length keys, such as "tags.%" (schema.TypeMap) and
	// "cidrBlocks.#" (schema.TypeSet), because of two reasons:
	//
	// 1. Diffs are applied successfully without them, except for
	// schema.TypeList.
	//
	// 2. If only length keys remain in the diff, after ignored
	// attributes are removed above, they cause diff to be considered
	// non-empty, even though it is effectively empty, therefore causing
	// an infinite update loop.
	for _, keyToIgnore := range initProviderExclusiveParamKeys {
		keyComponents := strings.Split(keyToIgnore, ".")
		if len(keyComponents) < 2 {
			continue
		}

		// TODO: Consider locating the schema corresponding to keyToIgnore
		// and checking whether it's a collection, before attempting to
		// delete its length key.
		for _, lengthSymbol := range []string{"%", "#"} {
			keyComponents[len(keyComponents)-1] = lengthSymbol
			lengthKey := strings.Join(keyComponents, ".")
			delete(instanceDiff.Attributes, lengthKey)
		}

		// TODO: tags-tags_all implementation is AWS specific.
		// Consider making this logic independent of provider.
		if keyComponents[0] == "tags" {
			keyComponents[0] = "tags_all"
			keyComponents[len(keyComponents)-1] = "%"
			lengthKey := strings.Join(keyComponents, ".")
			delete(instanceDiff.Attributes, lengthKey)
		}
	}
	return nil
}

// resource timeouts configuration
func getTimeoutParameters(config *config.Resource) map[string]any { //nolint:gocyclo
	timeouts := make(map[string]any)
	// first use the timeout overrides specified in
	// the Terraform resource schema
	if config.TerraformResource.Timeouts != nil {
		if config.TerraformResource.Timeouts.Create != nil && *config.TerraformResource.Timeouts.Create != 0 {
			timeouts[schema.TimeoutCreate] = config.TerraformResource.Timeouts.Create.Nanoseconds()
		}
		if config.TerraformResource.Timeouts.Update != nil && *config.TerraformResource.Timeouts.Update != 0 {
			timeouts[schema.TimeoutUpdate] = config.TerraformResource.Timeouts.Update.Nanoseconds()
		}
		if config.TerraformResource.Timeouts.Delete != nil && *config.TerraformResource.Timeouts.Delete != 0 {
			timeouts[schema.TimeoutDelete] = config.TerraformResource.Timeouts.Delete.Nanoseconds()
		}
		if config.TerraformResource.Timeouts.Read != nil && *config.TerraformResource.Timeouts.Read != 0 {
			timeouts[schema.TimeoutRead] = config.TerraformResource.Timeouts.Read.Nanoseconds()
		}
	}
	// then, override any Terraform defaults using any upjet
	// resource configuration overrides
	if config.OperationTimeouts.Create != 0 {
		timeouts[schema.TimeoutCreate] = config.OperationTimeouts.Create.Nanoseconds()
	}
	if config.OperationTimeouts.Update != 0 {
		timeouts[schema.TimeoutUpdate] = config.OperationTimeouts.Update.Nanoseconds()
	}
	if config.OperationTimeouts.Delete != 0 {
		timeouts[schema.TimeoutDelete] = config.OperationTimeouts.Delete.Nanoseconds()
	}
	if config.OperationTimeouts.Read != 0 {
		timeouts[schema.TimeoutRead] = config.OperationTimeouts.Read.Nanoseconds()
	}
	return timeouts
}

func (n *noForkExternal) getResourceDataDiff(tr resource.Terraformed, ctx context.Context, s *tf.InstanceState, resourceExists bool) (*tf.InstanceDiff, error) { //nolint:gocyclo
	resourceConfig := tf.NewResourceConfigRaw(n.params)
	instanceDiff, err := schema.InternalMap(n.config.TerraformResource.Schema).Diff(ctx, s, resourceConfig, n.config.TerraformResource.CustomizeDiff, n.ts.Meta, false)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get *terraform.InstanceDiff")
	}
	if n.config.TerraformCustomDiff != nil {
		instanceDiff, err = n.config.TerraformCustomDiff(instanceDiff, s, resourceConfig)
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
		v, err = instanceDiff.ApplyToValue(v, n.config.TerraformResource.CoreConfigSchema())
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
	timeouts := getTimeoutParameters(n.config)
	if len(timeouts) > 0 {
		if instanceDiff == nil {
			instanceDiff = tf.NewInstanceDiff()
		}
		if instanceDiff.Meta == nil {
			instanceDiff.Meta = make(map[string]interface{})
		}
		instanceDiff.Meta[schema.TimeoutKey] = timeouts
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
	diffState := n.opTracker.GetTfState()
	n.opTracker.SetTfState(newState) // TODO: missing RawConfig & RawPlan here...
	resourceExists := newState != nil && newState.ID != ""

	var stateValueMap map[string]any
	if resourceExists {
		jsonMap, stateValue, err := n.fromInstanceStateToJSONMap(newState)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot convert instance state to JSON map")
		}
		stateValueMap = jsonMap
		newState.RawPlan = stateValue
		diffState = newState
	} else if diffState != nil {
		diffState.Attributes = nil
	}
	instanceDiff, err := n.getResourceDataDiff(mg.(resource.Terraformed), ctx, diffState, resourceExists)
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
		if nameChanged, err := n.setExternalName(mg, stateValueMap); err != nil {
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
func (n *noForkExternal) setExternalName(mg xpresource.Managed, stateValueMap map[string]interface{}) (bool, error) {
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

func (n *noForkExternal) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	n.logger.Debug("Creating the external resource")
	start := time.Now()
	newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), n.instanceDiff, n.ts.Meta)
	metrics.ExternalAPITime.WithLabelValues("create").Observe(time.Since(start).Seconds())
	if diag != nil && diag.HasError() {
		// we need to store the Terraform state from the downstream create call if
		// one is available even if the diagnostics has reported errors.
		// The downstream create call comprises multiple external API calls such as
		// the external resource create call, expected state assertion calls
		// (external resource state reads) and external resource state refresh
		// calls, etc. Any of these steps can fail and if the initial
		// external resource create call succeeds, then the TF plugin SDK makes the
		// state (together with the TF ID associated with the external resource)
		// available reporting any encountered issues in the returned diagnostics.
		// If we don't record the returned state from the successful create call,
		// then we may hit issues for resources whose Crossplane identifiers cannot
		// be computed solely from spec parameters and provider configs, i.e.,
		// those that contain a random part generated by the CSP. Please see:
		// https://github.com/upbound/provider-aws/issues/1010, or
		// https://github.com/upbound/provider-aws/issues/1018, which both involve
		// MRs with config.IdentifierFromProvider external-name configurations.
		// NOTE: The safe (and thus the proper) thing to do in this situation from
		// the Crossplane provider's perspective is to set the MR's
		// `crossplane.io/external-create-failed` annotation because the provider
		// does not know the exact state the external resource is in and a manual
		// intervention may be required. But at the time we are introducing this
		// fix, we believe associating the external-resource with the MR will just
		// provide a better UX although the external resource may not be in the
		// expected/desired state yet. We are also planning for improvements on the
		// crossplane-runtime's managed reconciler to better support upjet's async
		// operations in this regard.
		if !n.opTracker.HasState() { // we do not expect a previous state here but just being defensive
			n.opTracker.SetTfState(newState)
		}
		return managed.ExternalCreation{}, errors.Errorf("failed to create the resource: %v", diag)
	}

	if newState == nil || newState.ID == "" {
		return managed.ExternalCreation{}, errors.New("failed to read the ID of the new resource")
	}
	n.opTracker.SetTfState(newState)

	stateValueMap, _, err := n.fromInstanceStateToJSONMap(newState)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "failed to convert instance state to map")
	}
	if _, err := n.setExternalName(mg, stateValueMap); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "failed to set the external-name of the managed resource during create")
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

	stateValueMap, _, err := n.fromInstanceStateToJSONMap(newState)
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

func (n *noForkExternal) fromInstanceStateToJSONMap(newState *tf.InstanceState) (map[string]interface{}, cty.Value, error) {
	impliedType := n.config.TerraformResource.CoreConfigSchema().ImpliedType()
	attrsAsCtyValue, err := newState.AttrsAsObjectValue(impliedType)
	if err != nil {
		return nil, cty.NilVal, errors.Wrap(err, "could not convert attrs to cty value")
	}
	stateValueMap, err := schema.StateValueToJSONMap(attrsAsCtyValue, impliedType)
	if err != nil {
		return nil, cty.NilVal, errors.Wrap(err, "could not convert instance state value to JSON")
	}
	return stateValueMap, attrsAsCtyValue, nil
}
