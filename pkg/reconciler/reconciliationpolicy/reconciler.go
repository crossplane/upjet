// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package reconciliationpolicy

import (
	"context"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/upjet/v2/pkg/internal/ratelimiter"
)

const (
	errConfigureRateLimiter    = "cannot configure failure rate limiter"
	errGetManaged              = "cannot get managed resource"
	errGetReconciliationPolicy = "cannot get reconciliation policy"
)

type targets struct {
	exponentialFailureRateLimiter *ExponentialFailureRateLimiter
}

// Reconciler wraps the supplied Reconciler and
// implements the reconciliation policy if a policy source is configured.
type Reconciler struct {
	inner   reconcile.Reconciler
	source  Source
	manager manager.Manager
	gvk     schema.GroupVersionKind

	targets targets
}

type ReconcilerOption func(*Reconciler)

func WithRateLimiter(rl *ExponentialFailureRateLimiter) ReconcilerOption {
	return func(r *Reconciler) {
		r.targets.exponentialFailureRateLimiter = rl
	}
}

func WithSource(s Source) ReconcilerOption {
	return func(r *Reconciler) {
		r.source = s
	}
}

// NewReconciler initializes a new Reconciler with the specified
// inner reconciler and manager for the given GVK.
func NewReconciler(inner reconcile.Reconciler, m manager.Manager, gvk schema.GroupVersionKind, o ...ReconcilerOption) *Reconciler {
	r := &Reconciler{
		manager: m,
		inner:   inner,
		gvk:     gvk,
	}

	for _, opt := range o {
		opt(r)
	}

	return r
}

// Reconcile the given request subject to the reconciler's Configurations.
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if err := r.setRateLimiter(ctx, req); err != nil {
		return reconcile.Result{}, errors.Wrap(err, errConfigureRateLimiter)
	}

	return r.inner.Reconcile(ctx, req)
}

func (r *Reconciler) setRateLimiter(ctx context.Context, req reconcile.Request) error {
	if r.source == nil || r.targets.exponentialFailureRateLimiter == nil {
		return nil
	}

	mg := resource.MustCreateObject(r.gvk, r.manager.GetScheme()).(resource.Managed)
	if err := r.manager.GetClient().Get(ctx, req.NamespacedName, mg); err != nil {
		// There's no need to requeue if the request no longer exists.
		// Otherwise, request will be requeued because an error is returned.
		// log.Debug("Cannot get managed resource", "error", err)
		return errors.Wrap(resource.IgnoreNotFound(err), errGetManaged)
	}

	rp, err := r.source(ctx, r.manager.GetClient(), mg)
	if err != nil {
		return errors.Wrap(err, errGetReconciliationPolicy)
	}

	if rp == nil || rp.ExponentialFailureRateLimiter == nil {
		return nil
	}

	rlKey := efrlKey{}
	if rp.ExponentialFailureRateLimiter.BaseDelay != nil {
		rlKey.baseDelay = *rp.ExponentialFailureRateLimiter.BaseDelay
	} else {
		rlKey.baseDelay = metav1.Duration{
			Duration: r.targets.exponentialFailureRateLimiter.defaultBaseDelay,
		}
	}
	if rp.ExponentialFailureRateLimiter.MaxDelay != nil {
		rlKey.maxDelay = *rp.ExponentialFailureRateLimiter.MaxDelay
	} else {
		rlKey.maxDelay = metav1.Duration{
			Duration: r.targets.exponentialFailureRateLimiter.defaultMaxDelay,
		}
	}

	r.targets.exponentialFailureRateLimiter.Add(
		rlKey,
		workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](rlKey.baseDelay.Duration, rlKey.maxDelay.Duration),
		req)
	return nil
}

func NewExponentialFailureRateLimiter(defaultBaseDelay time.Duration, defaultMaxDelay time.Duration) *ExponentialFailureRateLimiter {
	return &ExponentialFailureRateLimiter{
		EncapsulatingRateLimiter: ratelimiter.NewEncapsulatingRateLimiter[efrlKey](workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](defaultBaseDelay, defaultMaxDelay)),
		defaultBaseDelay:         defaultBaseDelay,
		defaultMaxDelay:          defaultMaxDelay,
	}
}
