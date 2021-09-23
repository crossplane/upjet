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

package terraform

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/terrajet/pkg/conversion"
	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
	"github.com/crossplane-contrib/terrajet/pkg/tfcli"
)

const (
	errUnexpectedObject = "the managed resource is not an Terraformed resource"
)

// SetupFn is a function that returns Terraform setup which contains
// provider requirement, configuration and Terraform version.
type SetupFn func(ctx context.Context, client client.Client, mg xpresource.Managed) (tfcli.TerraformSetup, error)

// NewConnector returns a new Connector object.
func NewConnector(kube client.Client, l logging.Logger, sf SetupFn) *Connector {
	return &Connector{
		kube:           kube,
		logger:         l,
		terraformSetup: sf,
	}
}

// Connector initializes the external client with credentials and other configuration
// parameters.
type Connector struct {
	kube           client.Client
	logger         logging.Logger
	terraformSetup SetupFn
}

// Connect makes sure the underlying client is ready to issue requests to the
// provider API.
func (c *Connector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	c.logger.Info("reconciled")
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return nil, errors.New(errUnexpectedObject)
	}

	ps, err := c.terraformSetup(ctx, c.kube, mg)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get provider setup")
	}

	tfCli, err := conversion.BuildClientForResource(ctx, tr, tfcli.WithLogger(c.logger), tfcli.WithTerraformSetup(ps))
	if err != nil {
		return nil, errors.Wrap(err, "cannot build tf client for resource")
	}

	return &external{
		kube:   c.kube,
		tf:     conversion.NewCLI(tfCli),
		log:    c.logger,
		record: event.NewNopRecorder(),
	}, nil
}

type external struct {
	kube client.Client
	tf   conversion.Adapter

	log    logging.Logger
	record event.Recorder
}

func (e *external) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errUnexpectedObject)
	}

	if xpmeta.GetExternalName(tr) == "" {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	res, err := e.tf.Observe(ctx, tr)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot check if resource exists")
	}

	// During creation (i.e. apply), Terraform already waits until resource is
	// ready. So, I believe it would be safe to assume it is available if create
	// step completed (i.e. resource exists).
	if res.Exists {
		tr.SetConditions(xpv1.Available())
	}

	return managed.ExternalObservation{
		ResourceExists:          res.Exists,
		ResourceUpToDate:        res.UpToDate,
		ResourceLateInitialized: res.LateInitialized,
		ConnectionDetails:       res.ConnectionDetails,
	}, nil
}

func (e *external) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	// Terraform does not have distinct 'create' and 'update' operations.
	u, err := e.Update(ctx, mg)
	return managed.ExternalCreation{ConnectionDetails: u.ConnectionDetails}, err
}

func (e *external) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errUnexpectedObject)
	}

	res, err := e.tf.CreateOrUpdate(ctx, tr)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "failed to update")
	}
	if !res.Completed {
		// Update is in progress, do nothing. We will check again after the poll interval.
		return managed.ExternalUpdate{}, nil
	}

	if err := e.persistState(ctx, tr); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot persist state")
	}

	return managed.ExternalUpdate{
		ConnectionDetails: res.ConnectionDetails,
	}, nil
}

func (e *external) Delete(ctx context.Context, mg xpresource.Managed) error {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return errors.New(errUnexpectedObject)
	}

	_, err := e.tf.Delete(ctx, tr)
	if err != nil {
		return errors.Wrap(err, "failed to delete")
	}

	return nil
}

// persistState does its best to store external name and tfstate annotations on
// the object.
func (e *external) persistState(ctx context.Context, obj xpresource.Object) error {
	externalName := xpmeta.GetExternalName(obj)
	privateRaw := obj.GetAnnotations()[tfcli.AnnotationKeyPrivateRawAttribute]

	err := retry.OnError(retry.DefaultRetry, xpresource.IsAPIError, func() error {
		nn := types.NamespacedName{Name: obj.GetName()}
		if err := e.kube.Get(ctx, nn, obj); err != nil {
			return err
		}

		xpmeta.SetExternalName(obj, externalName)
		xpmeta.AddAnnotations(obj, map[string]string{tfcli.AnnotationKeyPrivateRawAttribute: privateRaw})
		return e.kube.Update(ctx, obj)
	})

	return errors.Wrap(err, "cannot update resource state")
}
