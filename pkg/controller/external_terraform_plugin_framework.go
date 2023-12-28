// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/resource"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/metrics"
	"github.com/crossplane/upjet/pkg/terraform"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
}

func processParamsWithStateFunc(schemaMap map[string]*schema.Schema, params map[string]any) map[string]any {
	if params == nil {
		return params
	}
	for key, param := range params {
		if sc, ok := schemaMap[key]; ok {
			params[key] = applyStateFuncToParam(sc, param)
		} else {
			params[key] = param
		}
	}
	return params
}

func applyStateFuncToParam(sc *schema.Schema, param any) any { //nolint:gocyclo
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
				pmap[pk] = applyStateFuncToParam(mapSchema, pv)
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
				pArray[i] = applyStateFuncToParam(setSchema, p)
			}
			return pArray
		} else if setResource, ok := sc.Elem.(*schema.Resource); ok {
			for i, p := range pArray {
				if resParam, okRParam := p.(map[string]any); okRParam {
					pArray[i] = processParamsWithStateFunc(setResource.Schema, resParam)
				}
			}
		}
	case schema.TypeString:
		// For String types check if it is an HCL string and process
		if isHCLSnippetPattern.MatchString(param.(string)) {
			hclProccessedParam, err := processHCLParam(param.(string))
			if err != nil {
				// c.logger.Debug("could not process param, returning original", "param", sc.GoString())
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

	// To Compute the ResourceDiff: n.resourceSchema.Diff(...)
	tr := mg.(resource.Terraformed)
	opTracker := c.operationTrackerStore.Tracker(tr)
	externalName := meta.GetExternalName(tr)
	params, err := getExtendedParameters(ctx, tr, externalName, c.config, ts, c.isManagementPoliciesEnabled, c.kube)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get the extended parameters for resource %q", mg.GetName())
	}

	// TODO(cem): Check if we need to perform an equivalent functionality for framework resources.
	params = processParamsWithStateFunc(c.config.TerraformResource.Schema, params)

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

	return &terraformPluginFrameworkExternalClient{
		ts:             ts,
		config:         c.config,
		logger:         logger,
		metricRecorder: c.metricRecorder,
		opTracker:      opTracker,
		resource:       c.config.TerraformPluginFrameworkResource,
		server:         providerserver.NewProtocol5(*c.terraformPluginFrameworkProvider)(),
		params:         params,
		// rawConfig:      rawConfig,
	}, nil
}

func (n *terraformPluginFrameworkExternalClient) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) { //nolint:gocyclo
	n.logger.Debug("Observing the external resource")

	if meta.WasDeleted(mg) && n.opTracker.IsDeleted() {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	jsonBytes, err := json.Marshal(n.params)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot convert params to json bytes")
	}

	readRequest := &tfprotov5.ReadResourceRequest{
		TypeName: n.config.Name,
		CurrentState: &tfprotov5.DynamicValue{
			JSON: jsonBytes,
		},
	}
	readResponse, err := n.server.ReadResource(ctx, readRequest)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot read resource")
	}

	fmt.Printf("%v", readResponse)

	// start := time.Now()
	// newState, diag := n.resourceSchema.RefreshWithoutUpgrade(ctx, n.opTracker.GetTfState(), n.ts.Meta)
	// metrics.ExternalAPITime.WithLabelValues("read").Observe(time.Since(start).Seconds())
	// if diag != nil && diag.HasError() {
	// 	return managed.ExternalObservation{}, errors.Errorf("failed to observe the resource: %v", diag)
	// }
	// n.opTracker.SetTfState(newState) // TODO: missing RawConfig & RawPlan here...

	// resourceExists := newState != nil && newState.ID != ""
	// instanceDiff, err := n.getResourceDataDiff(mg.(resource.Terraformed), ctx, newState, resourceExists)
	// if err != nil {
	// 	return managed.ExternalObservation{}, errors.Wrap(err, "cannot compute the instance diff")
	// }
	// n.instanceDiff = instanceDiff
	// noDiff := instanceDiff.Empty()

	// var connDetails managed.ConnectionDetails
	// if !resourceExists && mg.GetDeletionTimestamp() != nil {
	// 	gvk := mg.GetObjectKind().GroupVersionKind()
	// 	metrics.DeletionTime.WithLabelValues(gvk.Group, gvk.Version, gvk.Kind).Observe(time.Since(mg.GetDeletionTimestamp().Time).Seconds())
	// }
	// specUpdateRequired := false
	// if resourceExists {
	// 	if mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionUnknown ||
	// 		mg.GetCondition(xpv1.TypeReady).Status == corev1.ConditionFalse {
	// 		addTTR(mg)
	// 	}
	// 	mg.SetConditions(xpv1.Available())
	// 	stateValueMap, err := n.fromInstanceStateToJSONMap(newState)
	// 	if err != nil {
	// 		return managed.ExternalObservation{}, errors.Wrap(err, "cannot convert instance state to JSON map")
	// 	}

	// 	buff, err := json.TFParser.Marshal(stateValueMap)
	// 	if err != nil {
	// 		return managed.ExternalObservation{}, errors.Wrap(err, "cannot marshal the attributes of the new state for late-initialization")
	// 	}
	// 	specUpdateRequired, err = mg.(resource.Terraformed).LateInitialize(buff)
	// 	if err != nil {
	// 		return managed.ExternalObservation{}, errors.Wrap(err, "cannot late-initialize the managed resource")
	// 	}

	// 	err = mg.(resource.Terraformed).SetObservation(stateValueMap)
	// 	if err != nil {
	// 		return managed.ExternalObservation{}, errors.Errorf("could not set observation: %v", err)
	// 	}
	// 	connDetails, err = resource.GetConnectionDetails(stateValueMap, mg.(resource.Terraformed), n.config)
	// 	if err != nil {
	// 		return managed.ExternalObservation{}, errors.Wrap(err, "cannot get connection details")
	// 	}

	// 	if noDiff {
	// 		n.metricRecorder.SetReconcileTime(mg.GetName())
	// 	}
	// 	if !specUpdateRequired {
	// 		resource.SetUpToDateCondition(mg, noDiff)
	// 	}
	// 	// check for an external-name change
	// 	if nameChanged, err := n.setExternalName(mg, newState); err != nil {
	// 		return managed.ExternalObservation{}, errors.Wrapf(err, "failed to set the external-name of the managed resource during observe")
	// 	} else {
	// 		specUpdateRequired = specUpdateRequired || nameChanged
	// 	}
	// }

	return managed.ExternalObservation{
		ResourceExists:          true,
		ResourceUpToDate:        true,
		ConnectionDetails:       managed.ConnectionDetails{},
		ResourceLateInitialized: false,
	}, nil
}

func (n *terraformPluginFrameworkExternalClient) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	n.logger.Debug("Creating the external resource")

	// start := time.Now()
	// newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), n.instanceDiff, n.ts.Meta)
	// metrics.ExternalAPITime.WithLabelValues("create").Observe(time.Since(start).Seconds())
	// // diag := n.resourceSchema.CreateWithoutTimeout(ctx, n.resourceData, n.ts.Meta)
	// if diag != nil && diag.HasError() {
	// 	return managed.ExternalCreation{}, errors.Errorf("failed to create the resource: %v", diag)
	// }

	// if newState == nil || newState.ID == "" {
	// 	return managed.ExternalCreation{}, errors.New("failed to read the ID of the new resource")
	// }
	// n.opTracker.SetTfState(newState)

	// if _, err := n.setExternalName(mg, newState); err != nil {
	// 	return managed.ExternalCreation{}, errors.Wrapf(err, "failed to set the external-name of the managed resource during create")
	// }
	// stateValueMap, err := n.fromInstanceStateToJSONMap(newState)
	// if err != nil {
	// 	return managed.ExternalCreation{}, err
	// }
	// err = mg.(resource.Terraformed).SetObservation(stateValueMap)
	// if err != nil {
	// 	return managed.ExternalCreation{}, errors.Errorf("could not set observation: %v", err)
	// }
	// conn, err := resource.GetConnectionDetails(stateValueMap, mg.(resource.Terraformed), n.config)
	// if err != nil {
	// 	return managed.ExternalCreation{}, errors.Wrap(err, "cannot get connection details")
	// }

	// TODO: Obviously, remove the following definition of conn, after it is defined correctly above.
	conn := managed.ConnectionDetails{}

	return managed.ExternalCreation{ConnectionDetails: conn}, nil
}

func (n *terraformPluginFrameworkExternalClient) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	n.logger.Debug("Updating the external resource")

	// if err := n.assertNoForceNew(); err != nil {
	// 	return managed.ExternalUpdate{}, errors.Wrap(err, "refuse to update the external resource")
	// }

	// start := time.Now()
	// newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), n.instanceDiff, n.ts.Meta)
	// metrics.ExternalAPITime.WithLabelValues("update").Observe(time.Since(start).Seconds())
	// if diag != nil && diag.HasError() {
	// 	return managed.ExternalUpdate{}, errors.Errorf("failed to update the resource: %v", diag)
	// }
	// n.opTracker.SetTfState(newState)

	// stateValueMap, err := n.fromInstanceStateToJSONMap(newState)
	// if err != nil {
	// 	return managed.ExternalUpdate{}, err
	// }

	// err = mg.(resource.Terraformed).SetObservation(stateValueMap)
	// if err != nil {
	// 	return managed.ExternalUpdate{}, errors.Errorf("failed to set observation: %v", err)
	// }

	return managed.ExternalUpdate{}, nil
}

func (n *terraformPluginFrameworkExternalClient) Delete(ctx context.Context, _ xpresource.Managed) error {
	n.logger.Debug("Deleting the external resource")
	// if n.instanceDiff == nil {
	// 	n.instanceDiff = tf.NewInstanceDiff()
	// }

	// n.instanceDiff.Destroy = true
	// start := time.Now()
	// newState, diag := n.resourceSchema.Apply(ctx, n.opTracker.GetTfState(), n.instanceDiff, n.ts.Meta)
	// metrics.ExternalAPITime.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	// if diag != nil && diag.HasError() {
	// 	return errors.Errorf("failed to delete the resource: %v", diag)
	// }
	// n.opTracker.SetTfState(newState)
	// // mark the resource as logically deleted if the TF call clears the state
	// n.opTracker.SetDeleted(newState == nil)

	return nil
}
