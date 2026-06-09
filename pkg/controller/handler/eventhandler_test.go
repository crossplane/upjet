// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// noopRateLimiter is a stand-in TypedRateLimiter used to assert that the
// configured default limiter is the exact instance cached and shared across
// rateLimiterName keys.
type noopRateLimiter struct{}

func (l *noopRateLimiter) When(_ reconcile.Request) time.Duration { return 0 }
func (l *noopRateLimiter) Forget(_ reconcile.Request)             {}
func (l *noopRateLimiter) NumRequeues(_ reconcile.Request) int    { return 0 }

// newPrimedEventHandler returns an EventHandler whose internal queue is
// initialized via the public event-handler interface so that
// RequestReconcile is callable.
func newPrimedEventHandler(t *testing.T, opts ...Option) *EventHandler {
	t.Helper()
	opts = append([]Option{WithLogger(logging.NewNopLogger())}, opts...)
	eh := NewEventHandler(opts...)
	q := workqueue.NewTypedRateLimitingQueue[reconcile.Request](
		workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	eh.Create(context.Background(), event.CreateEvent{Object: &corev1.ConfigMap{}}, q)
	return eh
}

func TestEventHandlerRequestReconcileDefaultRateLimiter(t *testing.T) {
	type want struct {
		// shared indicates whether map entries across distinct
		// rateLimiterNames should reference the same limiter instance.
		shared bool
	}
	cases := map[string]struct {
		reason            string
		withCustomDefault bool
		want              want
	}{
		"FallbackPerName": {
			reason: "Without WithDefaultRateLimiter, each distinct rateLimiterName receives its own fresh DefaultTypedControllerRateLimiter instance.",
			want:   want{shared: false},
		},
		"SharedAcrossNames": {
			reason:            "With WithDefaultRateLimiter, the configured limiter instance is reused for every rateLimiterName that has no explicit entry.",
			withCustomDefault: true,
			want:              want{shared: true},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var custom workqueue.TypedRateLimiter[reconcile.Request]
			var opts []Option
			if tc.withCustomDefault {
				custom = &noopRateLimiter{}
				opts = append(opts, WithDefaultRateLimiter(custom))
			}
			eh := newPrimedEventHandler(t, opts...)

			if ok := eh.RequestReconcile("alpha", types.NamespacedName{Name: "a"}, nil); !ok {
				t.Fatalf("RequestReconcile(alpha) returned false, want true")
			}
			if ok := eh.RequestReconcile("beta", types.NamespacedName{Name: "b"}, nil); !ok {
				t.Fatalf("RequestReconcile(beta) returned false, want true")
			}

			eh.mu.RLock()
			alpha, ok := eh.rateLimiterMap["alpha"]
			if !ok {
				eh.mu.RUnlock()
				t.Fatalf("rateLimiterMap missing entry for 'alpha' after RequestReconcile")
			}
			beta, ok := eh.rateLimiterMap["beta"]
			if !ok {
				eh.mu.RUnlock()
				t.Fatalf("rateLimiterMap missing entry for 'beta' after RequestReconcile")
			}
			eh.mu.RUnlock()

			if alpha == nil || beta == nil {
				t.Fatalf("%s\nrate limiter entries must be non-nil; got alpha=%v, beta=%v", tc.reason, alpha, beta)
			}

			switch {
			case tc.want.shared:
				if alpha != custom {
					t.Errorf("%s\nrateLimiterMap['alpha'] is not the configured default limiter (got %T)", tc.reason, alpha)
				}
				if beta != custom {
					t.Errorf("%s\nrateLimiterMap['beta'] is not the configured default limiter (got %T)", tc.reason, beta)
				}
			default:
				if alpha == beta {
					t.Errorf("%s\nrateLimiterMap['alpha'] and ['beta'] are the same instance; expected distinct fallback limiters", tc.reason)
				}
			}

			// Caching: a subsequent call for the same name must reuse
			// the cached instance rather than allocate a new one.
			if ok := eh.RequestReconcile("alpha", types.NamespacedName{Name: "a"}, nil); !ok {
				t.Fatalf("RequestReconcile(alpha) second call returned false")
			}
			eh.mu.RLock()
			alphaAgain := eh.rateLimiterMap["alpha"]
			eh.mu.RUnlock()
			if alphaAgain != alpha {
				t.Errorf("%s\nrateLimiterMap['alpha'] was replaced on second call; expected the cached instance to be reused", tc.reason)
			}
		})
	}
}

func TestEventHandlerRequestReconcileConcurrent(t *testing.T) {
	// Exercises caching behavior under concurrent callers. Combined with
	// `go test -race` this also asserts the absence of data races on
	// rateLimiterMap.
	custom := &noopRateLimiter{}
	eh := newPrimedEventHandler(t, WithDefaultRateLimiter(custom))

	names := []string{"a", "b", "c", "d"}
	const goroutines = 64
	const callsPerGoroutine = 8

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				rl := names[(g+i)%len(names)]
				if !eh.RequestReconcile(rl, types.NamespacedName{Name: rl}, nil) {
					t.Errorf("RequestReconcile(%s) returned false", rl)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	eh.mu.RLock()
	defer eh.mu.RUnlock()
	if got, want := len(eh.rateLimiterMap), len(names); got != want {
		t.Errorf("rateLimiterMap has %d entries; want %d", got, want)
	}
	for _, n := range names {
		rl, ok := eh.rateLimiterMap[n]
		if !ok {
			t.Errorf("rateLimiterMap missing entry for %q", n)
			continue
		}
		if rl != custom {
			t.Errorf("rateLimiterMap[%q] is not the configured default limiter (got %T)", n, rl)
		}
	}
}
