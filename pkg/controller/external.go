/*
Copyright 2021 Upbound Inc.
*/

package controller

import (
	"context"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/metrics"
	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/resource/json"
	"github.com/upbound/upjet/pkg/terraform"
)

const (
	errUnexpectedObject  = "the custom resource is not a Terraformed resource"
	errGetTerraformSetup = "cannot get terraform setup"
	errGetWorkspace      = "cannot get a terraform workspace for resource"
	errRefresh           = "cannot run refresh"
	errImport            = "cannot run import"
	errPlan              = "cannot run plan"
	errStartAsyncApply   = "cannot start async apply"
	errStartAsyncDestroy = "cannot start async destroy"
	errApply             = "cannot apply"
	errDestroy           = "cannot destroy"
	errStatusUpdate      = "cannot update status of custom resource"
	errScheduleProvider  = "cannot schedule native Terraform provider process"
)

// Option allows you to configure Connector.
type Option func(*Connector)

// WithCallbackProvider configures the controller to use async variant of the functions
// of the Terraform client and run given callbacks once those operations are
// completed.
func WithCallbackProvider(ac CallbackProvider) Option {
	return func(c *Connector) {
		c.callback = ac
	}
}

// WithLogger configures a logger for the Connector.
func WithLogger(l logging.Logger) Option {
	return func(c *Connector) {
		c.logger = l
	}
}

// NewConnector returns a new Connector object.
func NewConnector(kube client.Client, ws Store, sf terraform.SetupFn, cfg *config.Resource, opts ...Option) *Connector {
	c := &Connector{
		kube:              kube,
		getTerraformSetup: sf,
		store:             ws,
		config:            cfg,
		logger:            logging.NewNopLogger(),
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
	config            *config.Resource
	callback          CallbackProvider
	logger            logging.Logger
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

	ws, err := c.store.Workspace(ctx, &APISecretClient{kube: c.kube}, tr, ts, c.config)
	if err != nil {
		return nil, errors.Wrap(err, errGetWorkspace)
	}
	return &external{
		workspace:         ws,
		config:            c.config,
		callback:          c.callback,
		providerScheduler: ts.Scheduler,
		providerHandle:    ws.ProviderHandle,
		logger:            c.logger.WithValues("uid", mg.GetUID()),
	}, nil
}

type external struct {
	workspace         Workspace
	config            *config.Resource
	callback          CallbackProvider
	providerScheduler terraform.ProviderScheduler
	providerHandle    terraform.ProviderHandle
	logger            logging.Logger
}

func (e *external) scheduleProvider() error {
	if e.providerScheduler == nil || e.workspace == nil {
		return nil
	}
	inuse, attachmentConfig, err := e.providerScheduler.Start(e.providerHandle)
	if err != nil {
		return errors.Wrap(err, errScheduleProvider)
	}
	if ps, ok := e.workspace.(ProviderSharer); ok {
		ps.UseProvider(inuse, attachmentConfig)
	}
	return nil
}

func (e *external) stopProvider() {
	if e.providerScheduler == nil {
		return
	}
	if err := e.providerScheduler.Stop(e.providerHandle); err != nil {
		e.logger.Info("ExternalClient failed to stop the native provider", "error", err)
	}
}

func (e *external) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) { //nolint:gocyclo
	// We skip the gocyclo check because most of the operations are straight-forward
	// and serial.
	// TODO(muvaf): Look for ways to reduce the cyclomatic complexity without
	// increasing the difficulty of understanding the flow.
	if err := e.scheduleProvider(); err != nil {
		return managed.ExternalObservation{}, errors.Wrapf(err, "cannot schedule a native provider during observe: %s", mg.GetUID())
	}
	defer e.stopProvider()
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errUnexpectedObject)
	}

	// Note(turkenh): We don't need to check if the management policies are
	// enabled or not because the crossplane-runtime's managed reconciler already
	// does that for us. In other words, if the management policy is set to
	// "ObserveOnly" without management policies being enabled, the managed
	// reconciler will error out before reaching this point.
	// https://github.com/crossplane/crossplane-runtime/pull/384/files#diff-97300a2543f95f5a2ada3560bf47dd7334e237e27976574d15d1cddef2e66c01R696
	if tr.GetManagementPolicy() == xpv1.ManagementObserveOnly {
		return e.Import(ctx, tr)
	}

	res, err := e.workspace.Refresh(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errRefresh)
	}

	switch {
	case res.ASyncInProgress:
		mg.SetConditions(resource.AsyncOperationOngoingCondition())
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	case !res.Exists:
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}
	// There might be a case where async operation is finished and the status
	// update marking it as finished didn't go through. At this point, we are
	// sure that there is no ongoing operation.
	if e.config.UseAsync {
		tr.SetConditions(resource.AsyncOperationFinishedCondition())
	}

	// No operation was in progress, our observation completed successfully, and
	// we have an observation to consume.
	tfstate := map[string]any{}
	if err := json.JSParser.Unmarshal(res.State.GetAttributes(), &tfstate); err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot unmarshal state attributes")
	}
	if err := tr.SetObservation(tfstate); err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot set observation")
	}

	annotationsUpdated, err := resource.SetCriticalAnnotations(tr, e.config, tfstate, string(res.State.GetPrivateRaw()))
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot set critical annotations")
	}
	conn, err := resource.GetConnectionDetails(tfstate, tr, e.config)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot get connection details")
	}

	lateInitedParams, err := tr.LateInitialize(res.State.GetAttributes())
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot late initialize parameters")
	}
	markedAvailable := tr.GetCondition(xpv1.TypeReady).Equal(xpv1.Available())

	// In the following switch block, before running a relatively costly
	// Terraform apply and that may fail before critical annotations are
	// updated, or late-initialized configuration is written to main.tf.json,
	// we try to perform the following in the given order:
	// 1. Update critical annotations if they have changed
	// 2. Update status
	// 3. Update spec with late-initialized fields
	// We prioritize critical annotation updates most not to lose certain info
	// (like the Cloud provider generated ID) before anything else. Then we
	// prioritize status updates over late-initialization spec updates to
	// mark the resource as available as soon as possible because a spec
	// update due to late-initialized fields will void the status update.
	switch {
	// we prioritize critical annotation updates over status updates
	case annotationsUpdated:
		return managed.ExternalObservation{
			ResourceExists:          true,
			ResourceUpToDate:        true,
			ConnectionDetails:       conn,
			ResourceLateInitialized: true,
		}, nil
	// we prioritize status updates over late-init'ed spec updates
	case !markedAvailable:
		addTTR(tr)
		tr.SetConditions(xpv1.Available())
		return managed.ExternalObservation{
			ResourceExists:    true,
			ResourceUpToDate:  true,
			ConnectionDetails: conn,
		}, nil
	// with the least priority wrt critical annotation updates and status updates
	// we allow a late-initialization before the Workspace.Plan call
	case lateInitedParams:
		return managed.ExternalObservation{
			ResourceExists:          true,
			ResourceUpToDate:        true,
			ConnectionDetails:       conn,
			ResourceLateInitialized: true,
		}, nil
	// now we do a Workspace.Refresh
	default:
		plan, err := e.workspace.Plan(ctx)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errPlan)
		}

		resource.SetUpToDateCondition(mg, plan.UpToDate)

		return managed.ExternalObservation{
			ResourceExists:    true,
			ResourceUpToDate:  plan.UpToDate,
			ConnectionDetails: conn,
		}, nil
	}
}

func addTTR(mg xpresource.Managed) {
	gvk := mg.GetObjectKind().GroupVersionKind()
	metrics.TTRMeasurements.WithLabelValues(gvk.Group, gvk.Version, gvk.Kind).Observe(time.Since(mg.GetCreationTimestamp().Time).Seconds())
}

func (e *external) Create(ctx context.Context, mg xpresource.Managed) (managed.ExternalCreation, error) {
	if err := e.scheduleProvider(); err != nil {
		return managed.ExternalCreation{}, errors.Wrapf(err, "cannot schedule a native provider during create: %s", mg.GetUID())
	}
	defer e.stopProvider()
	if e.config.UseAsync {
		return managed.ExternalCreation{}, errors.Wrap(e.workspace.ApplyAsync(e.callback.Apply(mg.GetName())), errStartAsyncApply)
	}
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errUnexpectedObject)
	}
	res, err := e.workspace.Apply(ctx)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errApply)
	}
	tfstate := map[string]any{}
	if err := json.JSParser.Unmarshal(res.State.GetAttributes(), &tfstate); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot unmarshal state attributes")
	}

	conn, err := resource.GetConnectionDetails(tfstate, tr, e.config)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot get connection details")
	}

	// NOTE(muvaf): Only spec and metadata changes are saved after Create call.
	_, err = resource.SetCriticalAnnotations(tr, e.config, tfstate, string(res.State.GetPrivateRaw()))
	return managed.ExternalCreation{ConnectionDetails: conn}, errors.Wrap(err, "cannot set critical annotations")
}

func (e *external) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	if err := e.scheduleProvider(); err != nil {
		return managed.ExternalUpdate{}, errors.Wrapf(err, "cannot schedule a native provider during update: %s", mg.GetUID())
	}
	defer e.stopProvider()
	if e.config.UseAsync {
		return managed.ExternalUpdate{}, errors.Wrap(e.workspace.ApplyAsync(e.callback.Apply(mg.GetName())), errStartAsyncApply)
	}
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errUnexpectedObject)
	}
	res, err := e.workspace.Apply(ctx)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errApply)
	}
	attr := map[string]any{}
	if err := json.JSParser.Unmarshal(res.State.GetAttributes(), &attr); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "cannot unmarshal state attributes")
	}
	return managed.ExternalUpdate{}, errors.Wrap(tr.SetObservation(attr), "cannot set observation")
}

func (e *external) Delete(ctx context.Context, mg xpresource.Managed) error {
	if err := e.scheduleProvider(); err != nil {
		return errors.Wrapf(err, "cannot schedule a native provider during delete: %s", mg.GetUID())
	}
	defer e.stopProvider()
	if e.config.UseAsync {
		return errors.Wrap(e.workspace.DestroyAsync(e.callback.Destroy(mg.GetName())), errStartAsyncDestroy)
	}
	return errors.Wrap(e.workspace.Destroy(ctx), errDestroy)
}

func (e *external) Import(ctx context.Context, tr resource.Terraformed) (managed.ExternalObservation, error) {
	res, err := e.workspace.Import(ctx, tr)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errImport)
	}
	// We normally don't expect apply/destroy to be in progress when the
	// management policy is set to "ObserveOnly". However, this could happen
	// if the policy is changed to "ObserveOnly" while an async operation is
	// in progress. In that case, we want to wait for the operation to finish
	// before we start observing.
	if res.ASyncInProgress {
		tr.SetConditions(resource.AsyncOperationOngoingCondition())
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}
	// If the resource doesn't exist, we don't need to do anything else.
	// We report it to the managed reconciler as a non-existent resource and
	// it will take care of reporting it to the user as an error case for
	// observe-only policy.
	if !res.Exists {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	// No operation was in progress, our observation completed successfully, and
	// we have an observation to consume.
	tfstate := map[string]any{}
	if err := json.JSParser.Unmarshal(res.State.GetAttributes(), &tfstate); err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot unmarshal state attributes")
	}
	if err := tr.SetObservation(tfstate); err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot set observation")
	}
	conn, err := resource.GetConnectionDetails(tfstate, tr, e.config)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot get connection details")
	}

	tr.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		ResourceExists:    true,
		ResourceUpToDate:  true,
		ConnectionDetails: conn,
	}, nil
}
