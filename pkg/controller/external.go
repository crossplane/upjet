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
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/terrajet/pkg/resource"
	"github.com/crossplane-contrib/terrajet/pkg/resource/json"
	"github.com/crossplane-contrib/terrajet/pkg/terraform"
)

const (
	errUnexpectedObject = "the managed resource is not a Terraformed resource"
)

// Option allows you to configure Connector.
type Option func(*Connector)

// WithLogger allows you to set logger of the Connector.
func WithLogger(l logging.Logger) Option {
	return func(c *Connector) {
		c.logger = l
	}
}

// UseAsync configures the controller to use async variant of the functions
// of the Terraform client.
func UseAsync() Option {
	return func(c *Connector) {
		c.async = true
	}
}

// NewConnector returns a new Connector object.
func NewConnector(kube client.Client, ws *terraform.WorkspaceStore, sf terraform.SetupFn, opts ...Option) *Connector {
	c := &Connector{
		kube:           kube,
		logger:         logging.NewNopLogger(),
		terraformSetup: sf,
		store:          ws,
	}
	for _, f := range opts {
		f(c)
	}
	return c
}

// Connector initializes the external client with credentials and other configuration
// parameters.
type Connector struct {
	kube           client.Client
	logger         logging.Logger
	store          *terraform.WorkspaceStore
	terraformSetup terraform.SetupFn
	async          bool
}

// Connect makes sure the underlying client is ready to issue requests to the
// provider API.
func (c *Connector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return nil, errors.New(errUnexpectedObject)
	}

	ts, err := c.terraformSetup(ctx, c.kube, mg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get provider setup")
	}

	tf, err := c.store.Workspace(ctx, tr, ts, c.logger)
	if err != nil {
		return nil, errors.Wrap(err, "cannot build terraform workspace for resource")
	}

	return &external{
		kube:  c.kube,
		tf:    tf,
		log:   c.logger,
		async: c.async,
	}, nil
}

type external struct {
	kube client.Client
	tf   Client

	async bool
	log   logging.Logger
}

func (e *external) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errUnexpectedObject)
	}

	res, err := e.tf.Refresh(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(xpresource.Ignore(terraform.IsNotFound, err), "cannot check if resource exists")
	}
	if res.IsDestroying || res.IsApplying {
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}
	if res.LastOperationError != nil {
		return managed.ExternalObservation{}, errors.Wrap(res.LastOperationError, "operation failed")
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

	// TODO(hasan): Handle late initialization of parameters.
	lateInited, err := lateInitializeAnnotations(tr, attr, string(res.State.GetPrivateRaw()))
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot late initialize annotations")
	}

	// During creation (i.e. apply), Terraform already waits until resource is
	// ready. So, I believe it would be safe to assume it is available if create
	// step completed (i.e. resource exists).
	tr.SetConditions(xpv1.Available())

	plan, err := e.tf.Plan(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot check resource status")
	}

	// TODO(muvaf): Handle connection details.
	return managed.ExternalObservation{
		ResourceExists:          true,
		ResourceUpToDate:        plan.UpToDate,
		ResourceLateInitialized: lateInited,
	}, nil
}

func (e *external) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errUnexpectedObject)
	}
	if e.async {
		return managed.ExternalCreation{}, errors.Wrap(e.tf.ApplyAsync(CriticalAnnotationsCallback(e.kube, tr)), "cannot start async apply")
	}
	res, err := e.tf.Apply(ctx)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot create")
	}
	attr := map[string]interface{}{}
	if err := json.JSParser.Unmarshal(res.State.GetAttributes(), &attr); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot unmarshal state attributes")
	}
	// TODO(muvaf): Handle connection details.

	// NOTE(muvaf): Only spec and metadata changes are saved after Create call.
	_, err = lateInitializeAnnotations(tr, attr, string(res.State.GetPrivateRaw()))
	return managed.ExternalCreation{}, errors.Wrap(err, "cannot late initialize annotations")
}

func (e *external) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	if e.async {
		return managed.ExternalUpdate{}, errors.Wrap(e.tf.ApplyAsync(terraform.NopCallbackFn), "cannot start async apply")
	}
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errUnexpectedObject)
	}
	res, err := e.tf.Apply(ctx)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot create")
	}
	attr := map[string]interface{}{}
	if err := json.JSParser.Unmarshal(res.State.GetAttributes(), &attr); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot unmarshal state attributes")
	}
	return managed.ExternalUpdate{}, errors.Wrap(tr.SetObservation(attr), "cannot set observation")
}

func (e *external) Delete(ctx context.Context, _ xpresource.Managed) error {
	if e.async {
		return errors.Wrap(e.tf.DestroyAsync(), "cannot start async destroy")
	}
	return errors.Wrap(e.tf.Destroy(ctx), "cannot destroy")
}

func lateInitializeAnnotations(tr resource.Terraformed, attr map[string]interface{}, privateRaw string) (bool, error) {
	changed := false
	if _, ok := tr.GetAnnotations()[terraform.AnnotationKeyPrivateRawAttribute]; !ok {
		xpmeta.AddAnnotations(tr, map[string]string{
			terraform.AnnotationKeyPrivateRawAttribute: privateRaw,
		})
		changed = true
	}
	if xpmeta.GetExternalName(tr) != "" {
		return changed, nil
	}

	// Terraform stores id for the external resource as an attribute in the
	// resource state. Key for the attribute holding external identifier is
	// resource specific. We rely on GetTerraformResourceIdField() function
	// to find out that key.
	id, exists := attr[tr.GetTerraformResourceIdField()]
	if !exists {
		return false, errors.Errorf("no value for id field: %s", tr.GetTerraformResourceIdField())
	}
	extID, ok := id.(string)
	if !ok {
		return false, errors.Errorf("id field is not a string")
	}
	xpmeta.SetExternalName(tr, extID)
	return true, nil
}

// CriticalAnnotationsCallback returns a callback function that would store the
// time-sensitive annotations.
func CriticalAnnotationsCallback(kube client.Client, tr resource.Terraformed) terraform.CallbackFn {
	return func(ctx context.Context, s *json.StateV4) error {
		attr := map[string]interface{}{}
		if err := json.JSParser.Unmarshal(s.GetAttributes(), &attr); err != nil {
			return errors.Wrap(err, "cannot unmarshal state attributes")
		}
		_, err := lateInitializeAnnotations(tr, attr, string(s.GetPrivateRaw()))
		if err != nil {
			return errors.Wrap(err, "cannot late initialize annotations")
		}
		// TODO(muvaf): We issue the update call even if the annotations didn't
		// change so that the resource gets enqueued. We need to test this
		// assumption.
		c := managed.NewRetryingCriticalAnnotationUpdater(kube)
		return errors.Wrap(c.UpdateCriticalAnnotations(ctx, tr), "cannot update critical annotations")
	}
}
