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
	"fmt"
	"strings"

	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/terraform"
)

func convertMapToCty(data map[string]any) cty.Value {
	transformedData := make(map[string]cty.Value)

	for key, value := range data {
		switch v := value.(type) {
		case int:
			transformedData[key] = cty.NumberIntVal(int64(v))
		case string:
			transformedData[key] = cty.StringVal(v)
		case bool:
			transformedData[key] = cty.BoolVal(v)
		case float64:
			transformedData[key] = cty.NumberFloatVal(v)
		case map[string]any:
			transformedData[key] = convertMapToCty(v)
		// more cases...
		default:
			// handle unknown types, for now we will ignore them
			continue
		}
	}

	return cty.ObjectVal(transformedData)
}

func fromFlatmap(m map[string]string) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		// we need to handle name hierarchies
		if strings.Contains(k, ".") {
			continue
		}
		result[k] = v
	}
	return result
}

type NoForkConnector struct {
	getTerraformSetup terraform.SetupFn
	kube              client.Client
	config            *config.Resource
}

func NewNoForkConnector(kube client.Client, sf terraform.SetupFn, cfg *config.Resource) *NoForkConnector {
	return &NoForkConnector{
		kube:              kube,
		getTerraformSetup: sf,
		config:            cfg,
	}
}

func (c *NoForkConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	/*	tr, ok := mg.(resource.Terraformed)
		if !ok {
			return nil, errors.New(errUnexpectedObject)
		}
	*/
	ts, err := c.getTerraformSetup(ctx, c.kube, mg)
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
		return nil, errors.Wrap(err, "cannot get sensitive parameters")
	}
	c.config.ExternalName.SetIdentifierArgumentFn(params, meta.GetExternalName(tr))

	tfID, err := c.config.ExternalName.GetIDFn(ctx, meta.GetExternalName(mg), params, ts.Map())
	if err != nil {
		return nil, errors.Wrap(err, "cannot get ID")
	}

	tfState, err := tr.GetObservation()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get the observation")
	}
	tfState["id"] = tfID
	s, err := c.config.TerraformResource.ShimInstanceStateFromValue(convertMapToCty(tfState))
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert cty.Value to terraform.InstanceState")
	}

	params["id"] = tfID
	instanceDiff, err := schema.InternalMap(c.config.TerraformResource.Schema).Diff(ctx, s, &tf.ResourceConfig{
		Raw:    params,
		Config: params,
	}, nil, ts.Meta, false)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get *terraform.InstanceDiff")
	}

	resourceData, err := schema.InternalMap(c.config.TerraformResource.Schema).Data(s, instanceDiff)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get *schema.ResourceData")
	}

	return &noForkExternal{
		ts:             ts,
		resourceSchema: c.config.TerraformResource,
		config:         c.config,
		kube:           c.kube,
		resourceData:   resourceData,
		instanceState:  s,
		instanceDiff:   instanceDiff,
	}, nil
}

type noForkExternal struct {
	ts             terraform.Setup
	resourceSchema *schema.Resource
	config         *config.Resource
	kube           client.Client
	resourceData   *schema.ResourceData
	instanceState  *tf.InstanceState
	instanceDiff   *tf.InstanceDiff
}

func (n *noForkExternal) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	s, diag := n.resourceSchema.RefreshWithoutUpgrade(ctx, n.instanceState, n.ts.Meta)
	fmt.Println(diag)
	if diag != nil && diag.HasError() {
		return managed.ExternalObservation{}, errors.Errorf("failed to observe the resource: %v", diag)
	}
	resourceExists := n.resourceData.Id() != ""
	if resourceExists {
		mg.SetConditions(xpv1.Available())
		if s != nil {
			tr := mg.(resource.Terraformed)
			tr.SetObservation(fromFlatmap(s.Attributes))
		}
	}
	noDiff := n.instanceDiff.Empty()
	return managed.ExternalObservation{
		ResourceExists:   resourceExists,
		ResourceUpToDate: noDiff,
	}, nil
}

func (n *noForkExternal) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	newState, diag := n.resourceSchema.Apply(ctx, n.instanceState, n.instanceDiff, n.ts.Meta)
	// diag := n.resourceSchema.CreateWithoutTimeout(ctx, n.resourceData, n.ts.Meta)
	fmt.Println(diag)
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

	return managed.ExternalCreation{}, nil
}

func (n *noForkExternal) Update(ctx context.Context, _ xpresource.Managed) (managed.ExternalUpdate, error) {
	_, diag := n.resourceSchema.Apply(ctx, n.instanceState, n.instanceDiff, n.ts.Meta)
	fmt.Println(diag)
	if diag != nil && diag.HasError() {
		return managed.ExternalUpdate{}, errors.Errorf("failed to update the resource: %v", diag)
	}
	return managed.ExternalUpdate{}, nil
}

func (n *noForkExternal) Delete(ctx context.Context, _ xpresource.Managed) error {
	diag := n.resourceSchema.DeleteWithoutTimeout(ctx, n.resourceData, n.ts.Meta)
	fmt.Println(diag)

	return nil
}
