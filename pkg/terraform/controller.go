package terraform

import (
	"context"

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

// SetupController setups controller for a Terraform managed resource
func SetupController(mgr ctrl.Manager, l logging.Logger, obj client.Object, of schema.GroupVersionKind, pcFn ProviderConfigFn) error {
	name := managed.ControllerName(of.GroupKind().String())

	rl := ratelimiter.NewDefaultProviderRateLimiter(ratelimiter.DefaultProviderRPS)
	o := controller.Options{
		RateLimiter: ratelimiter.NewDefaultManagedRateLimiter(rl),
	}

	r := managed.NewReconciler(mgr,
		xpresource.ManagedKind(of),
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

	// TODO(hasan): create and pass the implementation of tfcli builder once available

	/*	tr, ok := mg.(resource.Terraformed)
		if !ok {
			return nil, errors.New(errUnexpectedObject)
		}
		pc, err := c.providerConfig(ctx, c.kube, mg)
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
		tf:     conversion.NewCli(nil),
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

	if xpmeta.GetExternalName(tr) == "" && meta.GetState(tr) == "" {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	res, err := e.tf.Observe(tr)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "cannot check if resource exists")
	}

	if !res.Completed {
		// Note(hasan): If an async operation is not completed yet, we don't want to reconcile
		// with exponential backoff since this is not an error condition rather an expected situation.
		// And checking that frequently wouldn't make much sense since we expect terraform client
		// invokes to take some seconds. Here, we chose to check again after poll interval,
		// but to speed things up, we might want to consider adding a special wait that is less frequent
		// than exponential backoff but more frequent than poll interval.

		// Observation is in progress, do nothing. We will check again after the poll interval.
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}

	// During creation (i.e. apply), Terraform already waits until resource is ready.
	// So, I believe it would be safe to assume it is available if create step completed (i.e. resource exists).
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
	tr, ok := mg.(resource.Terraformed)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errUnexpectedObject)
	}

	res, err := e.tf.Create(tr)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "failed to create")
	}
	if !res.Completed {
		// Creation is in progress, do nothing. We will check again after the poll interval.
		return managed.ExternalCreation{}, nil
	}

	if err := e.persistState(ctx, tr); err != nil {
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

	res, err := e.tf.Update(tr)
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

	_, err := e.tf.Delete(tr)
	if err != nil {
		return errors.Wrap(err, "failed to delete")
	}

	return nil
}

func (e *external) persistState(ctx context.Context, obj xpresource.Object) error {
	externalName := xpmeta.GetExternalName(obj)
	newState := meta.GetState(obj)

	// We will retry in all cases where the error comes from the api-server.
	// At one point, context deadline will be exceeded and, we'll get out
	// of the loop. In that case, we warn the user that the external resource
	// might be leaked.
	err := retry.OnError(retry.DefaultRetry, xpresource.IsAPIError, func() error {
		nn := types.NamespacedName{Name: obj.GetName()}
		if err := e.kube.Get(ctx, nn, obj); err != nil {
			return err
		}

		// We know that external name would never change once it is set
		if xpmeta.GetExternalName(obj) != "" && xpmeta.GetExternalName(obj) != externalName {
			return errors.Errorf("external name should not change once it is set. current %s, new %s",
				xpmeta.GetExternalName(obj), externalName)
		}

		xpmeta.SetExternalName(obj, externalName)
		meta.SetState(obj, newState)
		return e.kube.Update(ctx, obj)
	})

	return errors.Wrap(err, "cannot update resource state")
}
