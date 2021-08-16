package terraform

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane-contrib/terrajet/pkg/conversion"
	"github.com/crossplane-contrib/terrajet/pkg/meta"
	"github.com/crossplane-contrib/terrajet/pkg/terraform/resource"
)

const (
	errUnexpectedObject = "the managed resource is not an Terraformed resource"
)

// ProviderConfigFn is a function that returns provider specific configuration
// like provider credentials used to connect to cloud APIs.
type ProviderConfigFn func(ctx context.Context, client client.Client, mg xpresource.Managed) ([]byte, error)

func SetupController(mgr ctrl.Manager, l logging.Logger, obj client.Object, of schema.GroupVersionKind, pcFn ProviderConfigFn) error {
	name := managed.ControllerName(of.GroupKind().String())

	rl := ratelimiter.NewDefaultProviderRateLimiter(ratelimiter.DefaultProviderRPS)
	o := controller.Options{
		RateLimiter: ratelimiter.NewDefaultManagedRateLimiter(rl),
	}

	r := managed.NewReconciler(mgr,
		xpresource.ManagedKind(of),
		managed.WithPollInterval(15*time.Second),
		managed.WithInitializers(),
		managed.WithExternalConnecter(&connector{kube: mgr.GetClient(), providerConfig: pcFn, logger: l}),
		managed.WithLogger(l.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o).
		For(obj).
		Complete(r)
}

type connector struct {
	kube           client.Client
	providerConfig ProviderConfigFn
	logger         logging.Logger
}

func (c *connector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return nil, errors.New(errUnexpectedObject)
	}

	// TODO(hasan): create and pass the implementation of tfcli builder once available
	/*	pc, err := c.providerConfig(ctx, c.kube, mg)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get provider config")
		}
		tfcb := tfcli.NewClientBuilder().
			WithLogger(c.logger).
			WithResourceName(tr.GetName()).
			WithHandle(string(tr.GetUID())).
			WithProviderConfiguration(pc).
			WithResourceType(tr.GetTerraformResourceType())*/

	return &external{
		kube:   c.kube,
		tf:     conversion.NewCli(c.logger, tr, nil),
		log:    c.logger,
		record: event.NewNopRecorder(),
	}, nil
}

// external manages lifecycle of a Terraform managed resource by implementing
// managed.ExternalClient interface.
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

	if xpmeta.GetExternalName(tr) == "" && meta.GetState(tr) == "" {
		tr.SetConditions(xpv1.Creating())
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	res, err := e.tf.Observe(ctx, tr)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot check if resource exists")
	}

	if !res.Completed {
		// Observation is in progress, do nothing
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}

	if res.UpToDate {
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
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errUnexpectedObject)
	}

	res, err := e.tf.Create(ctx, tr)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "failed to create")
	}
	if !res.Completed {
		// Creation is in progress, do nothing
		return managed.ExternalCreation{}, nil
	}

	if err := e.persistState(ctx, tr, res.State, res.ExternalName); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "cannot persist state")
	}

	return managed.ExternalCreation{
		ConnectionDetails: res.ConnectionDetails,
	}, err
}

func (e *external) Update(ctx context.Context, mg xpresource.Managed) (managed.ExternalUpdate, error) {
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errUnexpectedObject)
	}

	res, err := e.tf.Update(ctx, tr)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, "failed to update")
	}
	if !res.Completed {
		// Update is in progress, do nothing
		return managed.ExternalUpdate{}, nil
	}

	if meta.GetState(tr) != res.State {
		if err := e.persistState(ctx, tr, res.State, ""); err != nil {
			return managed.ExternalUpdate{}, errors.Wrap(err, "cannot persist state")
		}
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

func (e *external) persistState(ctx context.Context, tr resource.Terraformed, state, externalName string) error {
	// We will retry in all cases where the error comes from the api-server.
	// At one point, context deadline will be exceeded and we'll get out
	// of the loop. In that case, we warn the user that the external resource
	// might be leaked.
	err := retry.OnError(retry.DefaultRetry, xpresource.IsAPIError, func() error {
		nn := types.NamespacedName{Name: tr.GetName()}
		if err := e.kube.Get(ctx, nn, tr); err != nil {
			return err
		}
		if xpmeta.GetExternalName(tr) == "" {
			xpmeta.SetExternalName(tr, externalName)
		}
		meta.SetState(tr, state)
		return e.kube.Update(ctx, tr)
	})

	return errors.Wrap(err, "cannot update resource state")
}
