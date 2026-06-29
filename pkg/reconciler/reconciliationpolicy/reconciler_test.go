// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package reconciliationpolicy

import (
	"context"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/upjet/v2/apis/configuration/v1alpha1"
)

const (
	testDefaultBaseDelay = 1 * time.Second
	testDefaultMaxDelay  = 60 * time.Second

	testCustomBaseDelay = 5 * time.Second
	testCustomMaxDelay  = 600 * time.Second
)

var errBoom = errors.New("boom")

// durPtr returns a pointer to a metav1.Duration wrapping d. It is the
// pointer-returning counterpart of the dur helper in types_test.go.
func durPtr(d time.Duration) *metav1.Duration {
	return &metav1.Duration{Duration: d}
}

// constSource returns a Source that yields the given policy and no error.
func constSource(rp *v1alpha1.ReconciliationPolicy) Source {
	return func(_ context.Context, _ client.Client, _ xpresource.Managed) (*v1alpha1.ReconciliationPolicy, error) {
		return rp, nil
	}
}

// errSource returns a Source that yields the given error.
func errSource(err error) Source {
	return func(_ context.Context, _ client.Client, _ xpresource.Managed) (*v1alpha1.ReconciliationPolicy, error) {
		return nil, err
	}
}

func TestSetRateLimiter(t *testing.T) {
	req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "test-ns", Name: "test-name"}}
	gvk := xpfake.GV.WithKind("Managed")
	scheme := xpfake.SchemeWith(&xpfake.Managed{})

	okClient := func() *test.MockClient {
		return &test.MockClient{MockGet: test.NewMockGetFn(nil)}
	}
	notFoundClient := func() *test.MockClient {
		return &test.MockClient{
			MockGet: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
				return kerrors.NewNotFound(schema.GroupResource{Group: "g", Resource: "managed"}, "test-name")
			},
		}
	}
	boomGetClient := func() *test.MockClient {
		return &test.MockClient{
			MockGet: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
				return errBoom
			},
		}
	}

	// addOverride seeds rl with a per-request override whose base delay is
	// the supplied baseDelay (and a matching max delay). It lets a test
	// represent the state left behind by a previous reconcile that observed
	// a ReconciliationPolicy.
	addOverride := func(baseDelay, maxDelay time.Duration) func(rl *ExponentialFailureRateLimiter, req reconcile.Request) {
		return func(rl *ExponentialFailureRateLimiter, req reconcile.Request) {
			key := efrlKey{
				baseDelay: metav1.Duration{Duration: baseDelay},
				maxDelay:  metav1.Duration{Duration: maxDelay},
			}
			rl.Add(key, workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](baseDelay, maxDelay), req)
		}
	}

	type args struct {
		mgr            *xpfake.Manager
		gvk            schema.GroupVersionKind
		source         Source
		useRateLimiter bool
		req            reconcile.Request
		// prePopulate, if non-nil, is invoked against the freshly constructed
		// ExponentialFailureRateLimiter before setRateLimiter is called. It is
		// used to seed a prior per-request override so the test can assert
		// that setRateLimiter clears it on policy removal.
		prePopulate func(rl *ExponentialFailureRateLimiter, req reconcile.Request)
	}
	type want struct {
		err error
		// expectedDelay is the duration returned by ExponentialFailureRateLimiter.When(req)
		// after the call. The first call to a freshly created
		// workqueue.NewTypedItemExponentialFailureRateLimiter yields the configured
		// baseDelay, which lets us probe whether the per-request rate limiter was
		// registered with the expected base delay. When useRateLimiter is false,
		// this check is skipped.
		expectedDelay time.Duration
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NilSource": {
			reason: "If the Source is nil, setRateLimiter must return no error and must not register a per-request rate limiter (the default rate limiter remains in effect).",
			args: args{
				mgr:            &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk:            gvk,
				source:         nil,
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: nil, expectedDelay: testDefaultBaseDelay},
		},
		"NilRateLimiterTarget": {
			reason: "If no ExponentialFailureRateLimiter target is configured, setRateLimiter must return no error and must not invoke the Source or the client.",
			args: args{
				mgr:            &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk:            gvk,
				source:         constSource(&v1alpha1.ReconciliationPolicy{}),
				useRateLimiter: false,
				req:            req,
			},
			want: want{err: nil},
		},
		"GetManagedError": {
			reason: "A non-not-found error returned from the managed resource Get must be wrapped with errGetManaged and returned.",
			args: args{
				mgr:            &xpfake.Manager{Client: boomGetClient(), Scheme: scheme},
				gvk:            gvk,
				source:         constSource(&v1alpha1.ReconciliationPolicy{}),
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: errors.Wrap(errBoom, errGetManaged), expectedDelay: testDefaultBaseDelay},
		},
		"GetManagedNotFound": {
			reason: "A not-found error from the managed resource Get must be ignored: no error must be returned and no per-request rate limiter must be registered.",
			args: args{
				mgr:            &xpfake.Manager{Client: notFoundClient(), Scheme: scheme},
				gvk:            gvk,
				source:         constSource(&v1alpha1.ReconciliationPolicy{}),
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: nil, expectedDelay: testDefaultBaseDelay},
		},
		"SourceError": {
			reason: "An error returned by the Source must be wrapped with errGetReconciliationPolicy and returned.",
			args: args{
				mgr:            &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk:            gvk,
				source:         errSource(errBoom),
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: errors.Wrap(errBoom, errGetReconciliationPolicy), expectedDelay: testDefaultBaseDelay},
		},
		"NilReconciliationPolicy": {
			reason: "A nil ReconciliationPolicy returned by the Source must yield a no-op: no error and no per-request rate limiter registered.",
			args: args{
				mgr:            &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk:            gvk,
				source:         constSource(nil),
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: nil, expectedDelay: testDefaultBaseDelay},
		},
		"NilExponentialFailureRateLimiterInPolicy": {
			reason: "A ReconciliationPolicy with a nil ExponentialFailureRateLimiter must yield a no-op: no error and no per-request rate limiter registered.",
			args: args{
				mgr:            &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk:            gvk,
				source:         constSource(&v1alpha1.ReconciliationPolicy{}),
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: nil, expectedDelay: testDefaultBaseDelay},
		},
		"NilReconciliationPolicyClearsPriorOverride": {
			reason: "When a prior reconcile registered a per-request override and the Source now returns a nil ReconciliationPolicy, setRateLimiter must clear the override so subsequent When(req) calls fall back to the default rate limiter.",
			args: args{
				mgr:            &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk:            gvk,
				source:         constSource(nil),
				useRateLimiter: true,
				req:            req,
				prePopulate:    addOverride(testCustomBaseDelay, testCustomMaxDelay),
			},
			want: want{err: nil, expectedDelay: testDefaultBaseDelay},
		},
		"NilExponentialFailureRateLimiterInPolicyClearsPriorOverride": {
			reason: "When a prior reconcile registered a per-request override and the policy no longer specifies an ExponentialFailureRateLimiter, setRateLimiter must clear the override so subsequent When(req) calls fall back to the default rate limiter.",
			args: args{
				mgr:            &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk:            gvk,
				source:         constSource(&v1alpha1.ReconciliationPolicy{}),
				useRateLimiter: true,
				req:            req,
				prePopulate:    addOverride(testCustomBaseDelay, testCustomMaxDelay),
			},
			want: want{err: nil, expectedDelay: testDefaultBaseDelay},
		},
		"DefaultsAppliedWhenDelaysAbsent": {
			reason: "When the policy has an ExponentialFailureRateLimiter without delays, the configured defaults must be used for the per-request rate limiter.",
			args: args{
				mgr: &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk: gvk,
				source: constSource(&v1alpha1.ReconciliationPolicy{
					ExponentialFailureRateLimiter: &v1alpha1.ExponentialFailureRateLimiter{},
				}),
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: nil, expectedDelay: testDefaultBaseDelay},
		},
		"BothDelaysOverriddenByPolicy": {
			reason: "When both base and max delays are specified by the policy, the per-request rate limiter must be constructed with those values.",
			args: args{
				mgr: &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk: gvk,
				source: constSource(&v1alpha1.ReconciliationPolicy{
					ExponentialFailureRateLimiter: &v1alpha1.ExponentialFailureRateLimiter{
						BaseDelay: durPtr(testCustomBaseDelay),
						MaxDelay:  durPtr(testCustomMaxDelay),
					},
				}),
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: nil, expectedDelay: testCustomBaseDelay},
		},
		"OnlyMaxDelayOverriddenByPolicy": {
			reason: "When only the max delay is specified by the policy, the default base delay must still be applied.",
			args: args{
				mgr: &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk: gvk,
				source: constSource(&v1alpha1.ReconciliationPolicy{
					ExponentialFailureRateLimiter: &v1alpha1.ExponentialFailureRateLimiter{
						MaxDelay: durPtr(testCustomMaxDelay),
					},
				}),
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: nil, expectedDelay: testDefaultBaseDelay},
		},
		"OnlyBaseDelayOverriddenByPolicy": {
			reason: "When only the base delay is specified by the policy, the per-request rate limiter must honour the configured base delay (the default max delay is preserved).",
			args: args{
				mgr: &xpfake.Manager{Client: okClient(), Scheme: scheme},
				gvk: gvk,
				source: constSource(&v1alpha1.ReconciliationPolicy{
					ExponentialFailureRateLimiter: &v1alpha1.ExponentialFailureRateLimiter{
						BaseDelay: durPtr(testCustomBaseDelay),
					},
				}),
				useRateLimiter: true,
				req:            req,
			},
			want: want{err: nil, expectedDelay: testCustomBaseDelay},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			opts := []ReconcilerOption{}
			if tc.args.source != nil {
				opts = append(opts, WithSource(tc.args.source))
			}
			var rl *ExponentialFailureRateLimiter
			if tc.args.useRateLimiter {
				rl = NewExponentialFailureRateLimiter(testDefaultBaseDelay, testDefaultMaxDelay)
				opts = append(opts, WithRateLimiter(rl))
			}
			if tc.args.prePopulate != nil {
				tc.args.prePopulate(rl, tc.args.req)
			}
			r := NewReconciler(nil, tc.args.mgr, tc.args.gvk, opts...)

			err := r.setRateLimiter(context.Background(), tc.args.req)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nsetRateLimiter(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if rl == nil || tc.want.expectedDelay == 0 {
				return
			}
			if got := rl.When(tc.args.req); got != tc.want.expectedDelay {
				t.Errorf("\n%s\nWhen(req) after setRateLimiter(...): want %s, got %s", tc.reason, tc.want.expectedDelay, got)
			}
		})
	}
}
