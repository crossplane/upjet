/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/terrajet/pkg/config"
	"github.com/crossplane-contrib/terrajet/pkg/resource"
	"github.com/crossplane-contrib/terrajet/pkg/resource/json"
	"github.com/crossplane-contrib/terrajet/pkg/terraform"
)

const (
	errUnexpectedObject  = "the managed resource is not a Terraformed resource"
	errGetTerraformSetup = "cannot get terraform setup"
	errGetWorkspace      = "cannot get a terraform workspace for resource"
	errRefresh           = "cannot run refresh"
	errPlan              = "cannot run plan"
	errStartAsyncApply   = "cannot start async apply"
	errStartAsyncDestroy = "cannot start async destroy"
	errApply             = "cannot apply"
	errDestroy           = "cannot destroy"
	errStatusUpdate      = "cannot update status of custom resource"
)

// Option allows you to configure Connector.
type Option func(*Connector)

// UseAsync configures the controller to use async variant of the functions
// of the Terraform client.
func UseAsync() Option {
	return func(c *Connector) {
		c.async = true
	}
}

// NewConnector returns a new Connector object.
func NewConnector(kube client.Client, ws Store, sf terraform.SetupFn, cfg config.Resource, opts ...Option) *Connector {
	c := &Connector{
		kube:              kube,
		getTerraformSetup: sf,
		store:             ws,
		config:            cfg,
	}
	for _, f := range opts {
		f(c)
	}
	return c
}

// Connector initializes the external client with credentials and other configuration
// parameters.
type Connector struct {
	kube              client.Client
	store             Store
	getTerraformSetup terraform.SetupFn
	async             bool
	config            config.Resource
}

// Connect makes sure the underlying client is ready to issue requests to the
// provider API.
func (c *Connector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return nil, errors.New(errUnexpectedObject)
	}

	ts, err := c.getTerraformSetup(ctx, c.kube, mg)
	if err != nil {
		return nil, errors.Wrap(err, errGetTerraformSetup)
	}

	tf, err := c.store.Workspace(ctx, &APISecretClient{kube: c.kube}, tr, ts, c.config)
	if err != nil {
		return nil, errors.Wrap(err, errGetWorkspace)
	}

	return &external{
		kube:      c.kube,
		workspace: tf,
		async:     c.async,
		config:    c.config,
	}, nil
}

type external struct {
	kube      client.Client
	workspace Workspace

	async  bool
	config config.Resource
}

func (e *external) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) { // nolint:gocyclo
	// We skip the gocyclo check because most of the operations are straight-forward
	// and serial.
	// TODO(muvaf): Look for ways to reduce the cyclomatic complexity without
	// increasing the difficulty of understanding the flow.
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errUnexpectedObject)
	}

	res, err := e.workspace.Refresh(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errRefresh)
	}
	if e.async {
		tr.SetConditions(resource.LastOperationCondition(res.LastOperationError))
		if err := e.kube.Status().Update(ctx, tr); err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errStatusUpdate)
		}
	}
	switch {
	case res.IsApplying, res.IsDestroying:
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	case !res.Exists:
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	// No operation was in progress, our observation completed successfully, and
	// we have an observation to consume.
	attr := map[string]interface{}{}
	if err := json.JSParser.Unmarshal(res.State.GetAttributes(), &attr); err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot unmarshal state attributes")
	}
	if err := tr.SetObservation(attr); err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot set observation")
	}

	lateInitedAnn, err := resource.LateInitializeAnnotations(tr, attr, string(res.State.GetPrivateRaw()))
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot late initialize annotations")
	}
	conn, err := resource.GetConnectionDetails(attr, tr, e.config)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot get connection details")
	}

	// We try to mark the resource ready before the spec late initialization
	// and a call for "Plan" because those operations are costly, i.e. late-init
	// causes a spec update which defers status update to the next reconcile and
	// "Plan" takes a few seconds.
	if !tr.GetCondition(xpv1.TypeReady).Equal(xpv1.Available()) {
		tr.SetConditions(xpv1.Available())
		return managed.ExternalObservation{
			ResourceExists:          true,
			ResourceUpToDate:        true,
			ConnectionDetails:       conn,
			ResourceLateInitialized: lateInitedAnn,
		}, nil
	}

	lateInitedParams, err := tr.LateInitialize(res.State.GetAttributes())
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot late initialize parameters")
	}

	plan, err := e.workspace.Plan(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errPlan)
	}

	return managed.ExternalObservation{
		ResourceExists:          true,
		ResourceUpToDate:        plan.UpToDate,
		ResourceLateInitialized: lateInitedAnn || lateInitedParams,
		ConnectionDetails:       conn,
	}, nil
}

func (e *external) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errUnexpectedObject)
	}
	if e.async {
		return managed.ExternalCreation{}, errors.Wrap(e.workspace.ApplyAsync(CriticalAnnotationsCallback(e.kube, tr)), errStartAsyncApply)
	}
	res, err := e.workspace.Apply(ctx)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errApply)
	}
	attr := map[string]interface{}{}
	if err := json.JSParser.Unmarshal(res.State.GetAttributes(), &attr); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot unmarshal state attributes")
	}

	conn, err := resource.GetConnectionDetails(attr, tr, e.config)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot get connection details")
	}

	// NOTE(muvaf): Only spec and metadata changes are saved after Create call.
	_, err = resource.LateInitializeAnnotations(tr, attr, string(res.State.GetPrivateRaw()))
	return managed.ExternalCreation{ConnectionDetails: conn}, errors.Wrap(err, "cannot late initialize annotations")
}

func (e *external) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	if e.async {
		return managed.ExternalUpdate{}, errors.Wrap(e.workspace.ApplyAsync(terraform.NopCallbackFn), errStartAsyncApply)
	}
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errUnexpectedObject)
	}
	res, err := e.workspace.Apply(ctx)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errApply)
	}
	attr := map[string]interface{}{}
	if err := json.JSParser.Unmarshal(res.State.GetAttributes(), &attr); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot unmarshal state attributes")
	}
	return managed.ExternalUpdate{}, errors.Wrap(tr.SetObservation(attr), "cannot set observation")
}

func (e *external) Delete(ctx context.Context, _ xpresource.Managed) error {
	if e.async {
		return errors.Wrap(e.workspace.DestroyAsync(), errStartAsyncDestroy)
	}
	return errors.Wrap(e.workspace.Destroy(ctx), errDestroy)
}

// CriticalAnnotationsCallback returns a callback function that would store the
// time-sensitive annotations.
func CriticalAnnotationsCallback(kube client.Client, tr resource.Terraformed) terraform.CallbackFn {
	return func(ctx context.Context, s *json.StateV4) error {
		attr := map[string]interface{}{}
		if err := json.JSParser.Unmarshal(s.GetAttributes(), &attr); err != nil {
			return errors.Wrap(err, "cannot unmarshal state attributes")
		}
		if _, err := resource.LateInitializeAnnotations(tr, attr, string(s.GetPrivateRaw())); err != nil {
			return errors.Wrap(err, "cannot late initialize annotations")
		}
		// TODO(muvaf): We issue the update call even if the annotations didn't
		// change so that the resource gets enqueued. We need to test this
		// assumption.
		c := managed.NewRetryingCriticalAnnotationUpdater(kube)
		return errors.Wrap(c.UpdateCriticalAnnotations(ctx, tr), "cannot update critical annotations")
	}
}
