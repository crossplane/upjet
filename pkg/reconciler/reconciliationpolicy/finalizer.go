// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package reconciliationpolicy

import (
	"context"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	errRemoveFinalizer = "cannot remove finalizer"
)

// Finalizer wraps an inner resource.Finalizer and tears down any
// reconciliation-policy state (e.g., per-resource rate limiter entries)
// after the inner Finalizer has successfully removed the Kubernetes
// finalizer. It must be used together with a Reconciler that has been
// configured with the same rate limiter target.
type Finalizer struct {
	resource.Finalizer
	targets targets
}

// FinalizerOption configures a Finalizer returned by NewFinalizer.
type FinalizerOption func(*Finalizer)

// WithFinalizerRateLimiter configures the Finalizer to remove the managed
// resource's entry from rl when its Kubernetes finalizer is removed.
// It should be paired with reconciler.WithRateLimiter passing the same rl
// so that per-resource state added during reconciliation is cleaned up on
// deletion.
func WithFinalizerRateLimiter(rl *ExponentialFailureRateLimiter) FinalizerOption {
	return func(f *Finalizer) {
		f.targets.exponentialFailureRateLimiter = rl
	}
}

// NewFinalizer initializes a new finalizer to be used with
// the Reconciler, which cleans up the reconciler's resources.
func NewFinalizer(inner resource.Finalizer, o ...FinalizerOption) *Finalizer {
	f := &Finalizer{
		Finalizer: inner,
	}

	for _, opt := range o {
		opt(f)
	}

	return f
}

// AddFinalizer to the supplied Managed resource.
func (cf *Finalizer) AddFinalizer(ctx context.Context, obj resource.Object) error {
	return cf.Finalizer.AddFinalizer(ctx, obj)
}

// RemoveFinalizer delegates to the inner Finalizer to remove the Kubernetes
// resource finalizer and, on success, cleans up the reconciler resources
// (e.g., per-resource rate limiter entries) associated with the object. If
// the inner Finalizer returns an error, the reconciler resources are left
// intact so that any per-resource retry state is preserved across requeues.
func (cf *Finalizer) RemoveFinalizer(ctx context.Context, obj resource.Object) error {
	if err := cf.Finalizer.RemoveFinalizer(ctx, obj); err != nil {
		return errors.Wrap(err, errRemoveFinalizer)
	}
	if cf.targets.exponentialFailureRateLimiter != nil {
		cf.targets.exponentialFailureRateLimiter.Remove(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: obj.GetNamespace(),
					Name:      obj.GetName(),
				},
			})
	}
	return nil
}
