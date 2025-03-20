// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/controller/handler"
	"github.com/crossplane/upjet/pkg/metrics"
	"github.com/crossplane/upjet/pkg/resource"
	"github.com/crossplane/upjet/pkg/resource/json"
	"github.com/crossplane/upjet/pkg/terraform"
	tferrors "github.com/crossplane/upjet/pkg/terraform/errors"
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
	errScheduleProvider  = "cannot schedule native Terraform provider process, please consider increasing its TTL with the --provider-ttl command-line option"
	errUpdateAnnotations = "cannot update managed resource annotations"
)

const (
	rateLimiterScheduler = "scheduler"
	rateLimiterStatus    = "status"
	retryLimit           = 20
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

// WithConnectorEventHandler configures the EventHandler so that
// the external clients can requeue reconciliation requests.
func WithConnectorEventHandler(e *handler.EventHandler) Option {
	return func(c *Connector) {
		c.eventHandler = e
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
	eventHandler      *handler.EventHandler
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
		eventHandler:      c.eventHandler,
		kube:              c.kube,
		logger:            c.logger.WithValues("uid", mg.GetUID(), "namespace", mg.GetNamespace(), "name", mg.GetName(), "gvk", mg.GetObjectKind().GroupVersionKind().String()),
	}, nil
}

type external struct {
	workspace         Workspace
	config            *config.Resource
	callback          CallbackProvider
	providerScheduler terraform.ProviderScheduler
	providerHandle    terraform.ProviderHandle
	eventHandler      *handler.EventHandler
	kube              client.Client
	logger            logging.Logger
}

func (e *external) scheduleProvider(name types.NamespacedName) (bool, error) {
	if e.providerScheduler == nil || e.workspace == nil {
		return false, nil
	}
	inuse, attachmentConfig, err := e.providerScheduler.Start(e.providerHandle)
	if err != nil {
		retryLimit := retryLimit
		if tferrors.IsRetryScheduleError(err) && (e.eventHandler != nil && e.eventHandler.RequestReconcile(rateLimiterScheduler, name, &retryLimit)) {
			// the reconcile request has been requeued for a rate-limited retry
			return true, nil
		}
		return false, errors.Wrap(err, errScheduleProvider)
	}
	if e.eventHandler != nil {
		e.eventHandler.Forget(rateLimiterScheduler, name)
	}
	if ps, ok := e.workspace.(ProviderSharer); ok {
		ps.UseProvider(inuse, attachmentConfig)
	}
	return false, nil
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
	name := types.NamespacedName{
		Namespace: mg.GetNamespace(),
		Name:      mg.GetName(),
	}
	requeued, err := e.scheduleProvider(name)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrapf(err, "cannot schedule a native provider during observe: %s", mg.GetUID())
	}
	if requeued {
		// return a noop for Observe after requeuing the reconcile request
		// for a retry.
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}
	defer e.stopProvider()

	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errUnexpectedObject)
	}

	policySet := sets.New[xpv1.ManagementAction](tr.GetManagementPolicies()...)

	// Note(turkenh): We don't need to check if the management policies are
	// enabled or not because the crossplane-runtime's managed reconciler already
	// does that for us. In other words, if the management policies are set
	// without management policies being enabled, the managed
	// reconciler will error out before reaching this point.
	// https://github.com/crossplane/crossplane-runtime/pull/384/files#diff-97300a2543f95f5a2ada3560bf47dd7334e237e27976574d15d1cddef2e66c01R696
	// Note (lsviben) We are only using import instead of refresh if the
	// management policies do not contain create or update as they need the
	// required fields to be set, which is not the case for import.
	if !policySet.HasAny(xpv1.ManagementActionCreate, xpv1.ManagementActionUpdate, xpv1.ManagementActionAll) {
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

	// NOTE(lsviben) although the annotations were supposed to be set and the
	// managed resource updated during the Create step, we are checking and
	// updating the annotations here due to the fact that in most cases, the
	// Create step is done asynchronously and the managed resource is not
	// updated with the annotations. That is why below we are prioritizing the
	// annotations update before anything else. We are setting lateInitialized
	// to true so that the reconciler updates the managed resource. This
	// behavior conflicts with management policies in which LateInitialize is
	// turned off. To circumvent this, we are checking if the management policy
	// does not contain LateInitialize and if it does not, we are updating the
	// annotations manually.
	annotationsUpdated, err := resource.SetCriticalAnnotations(tr, e.config, tfstate, string(res.State.GetPrivateRaw()))
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot set critical annotations")
	}
	policyHasLateInit := policySet.HasAny(xpv1.ManagementActionLateInitialize, xpv1.ManagementActionAll)
	if annotationsUpdated && !policyHasLateInit {
		if err := e.kube.Update(ctx, mg); err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errUpdateAnnotations)
		}
		annotationsUpdated = false
	}
	conn, err := resource.GetConnectionDetails(tfstate, tr, e.config)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot get connection details")
	}

	var lateInitedParams bool
	if policyHasLateInit {
		lateInitedParams, err = tr.LateInitialize(res.State.GetAttributes())
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, "cannot late initialize parameters")
		}
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
		e.logger.Debug("Critical annotations have been updated.")
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
		e.logger.Debug("Resource is marked as available.")
		if e.eventHandler != nil {
			name := types.NamespacedName{
				Namespace: mg.GetNamespace(),
				Name:      mg.GetName(),
			}
			e.eventHandler.RequestReconcile(rateLimiterStatus, name, nil)
		}
		return managed.ExternalObservation{
			ResourceExists:    true,
			ResourceUpToDate:  true,
			ConnectionDetails: conn,
		}, nil
	// with the least priority wrt critical annotation updates and status updates
	// we allow a late-initialization before the Workspace.Plan call
	case lateInitedParams:
		e.logger.Debug("Resource is late-initialized.")
		return managed.ExternalObservation{
			ResourceExists:          true,
			ResourceUpToDate:        true,
			ConnectionDetails:       conn,
			ResourceLateInitialized: true,
		}, nil
	// now we do a Workspace.Refresh
	default:
		if e.eventHandler != nil {
			name := types.NamespacedName{
				Namespace: mg.GetNamespace(),
				Name:      mg.GetName(),
			}
			e.eventHandler.Forget(rateLimiterStatus, name)
		}

		// TODO(cem): Consider skipping diff calculation (terraform plan) to
		// avoid potential config validation errors in the import path. See
		// https://github.com/crossplane/upjet/pull/461
		plan, err := e.workspace.Plan(ctx)
		if err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errPlan)
		}

		resource.SetUpToDateCondition(mg, plan.UpToDate)
		e.logger.Debug("Called plan on the resource.", "upToDate", plan.UpToDate)

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
	name := types.NamespacedName{
		Namespace: mg.GetNamespace(),
		Name:      mg.GetName(),
	}
	requeued, err := e.scheduleProvider(name)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrapf(err, "cannot schedule a native provider during create: %s", mg.GetUID())
	}
	if requeued {
		return managed.ExternalCreation{}, nil
	}
	defer e.stopProvider()
	if e.config.UseAsync {
		return managed.ExternalCreation{}, errors.Wrap(e.workspace.ApplyAsync(e.callback.Create(name)), errStartAsyncApply)
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
	name := types.NamespacedName{
		Namespace: mg.GetNamespace(),
		Name:      mg.GetName(),
	}
	requeued, err := e.scheduleProvider(name)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrapf(err, "cannot schedule a native provider during update: %s", mg.GetUID())
	}
	if requeued {
		return managed.ExternalUpdate{}, nil
	}
	defer e.stopProvider()
	if e.config.UseAsync {
		return managed.ExternalUpdate{}, errors.Wrap(e.workspace.ApplyAsync(e.callback.Update(name)), errStartAsyncApply)
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

func (e *external) Delete(ctx context.Context, mg xpresource.Managed) (managed.ExternalDelete, error) {
	name := types.NamespacedName{
		Namespace: mg.GetNamespace(),
		Name:      mg.GetName(),
	}
	requeued, err := e.scheduleProvider(name)
	if err != nil {
		return managed.ExternalDelete{}, errors.Wrapf(err, "cannot schedule a native provider during delete: %s", mg.GetUID())
	}
	if requeued {
		return managed.ExternalDelete{}, nil
	}
	defer e.stopProvider()
	if e.config.UseAsync {
		return managed.ExternalDelete{}, errors.Wrap(e.workspace.DestroyAsync(e.callback.Destroy(name)), errStartAsyncDestroy)
	}
	return managed.ExternalDelete{}, errors.Wrap(e.workspace.Destroy(ctx), errDestroy)
}

func (e *external) Disconnect(_ context.Context) error {
	return nil
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
