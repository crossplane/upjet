// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package ratelimiter

import (
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// fakeRateLimiter is a workqueue.TypedRateLimiter[reconcile.Request] that
// records every call it receives. Each instance is named so that diffs against
// expected invocations stay readable when multiple fakes are registered.
type fakeRateLimiter struct {
	name         string
	whenDuration time.Duration
	requeueCount int

	mu           sync.Mutex
	whenCalls    []reconcile.Request
	forgetCalls  []reconcile.Request
	requeueCalls []reconcile.Request
}

var _ workqueue.TypedRateLimiter[reconcile.Request] = (*fakeRateLimiter)(nil)

func (f *fakeRateLimiter) When(item reconcile.Request) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.whenCalls = append(f.whenCalls, item)
	return f.whenDuration
}

func (f *fakeRateLimiter) Forget(item reconcile.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgetCalls = append(f.forgetCalls, item)
}

func (f *fakeRateLimiter) NumRequeues(item reconcile.Request) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requeueCalls = append(f.requeueCalls, item)
	return f.requeueCount
}

// snapshotCalls returns copies of the call records, taken under the fake's
// lock. Callers may compare these against expectations without racing the
// fake's own bookkeeping.
func (f *fakeRateLimiter) snapshotCalls() (whens, forgets, requeues []reconcile.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	whens = append([]reconcile.Request(nil), f.whenCalls...)
	forgets = append([]reconcile.Request(nil), f.forgetCalls...)
	requeues = append([]reconcile.Request(nil), f.requeueCalls...)
	return
}

// state captures the EncapsulatingRateLimiter's internal state for assertion
// via cmp.Diff. Field names are exported so the default cmp options descend
// into the struct.
type state struct {
	Inner        map[reconcile.Request]string
	RateLimiters map[string]workqueue.TypedRateLimiter[reconcile.Request]
	Requests     map[string]sets.Set[reconcile.Request]
}

func snapshotOf(c *EncapsulatingRateLimiter[string]) state {
	s := state{
		Inner:        map[reconcile.Request]string{},
		RateLimiters: map[string]workqueue.TypedRateLimiter[reconcile.Request]{},
		Requests:     map[string]sets.Set[reconcile.Request]{},
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k, v := range c.inner {
		s.Inner[k] = v
	}
	for k, v := range c.rateLimiters {
		s.RateLimiters[k] = v.rateLimiter
		s.Requests[k] = v.requests
	}
	return s
}

// rlIdentity compares rate-limiter interface values by identity. The
// EncapsulatingRateLimiter's contract is "store the exact instance passed to
// Add", so identity rather than structural equality is what we need to assert.
var rlIdentity = cmp.Comparer(func(a, b workqueue.TypedRateLimiter[reconcile.Request]) bool {
	return a == b
})

func newReq(name string) reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}
}

// addOp describes a single Add() invocation. Tests use this to declare both
// the pre-state of the EncapsulatingRateLimiter and the operation under test.
type addOp struct {
	key string
	rl  workqueue.TypedRateLimiter[reconcile.Request]
	req reconcile.Request
}

func applyAdds(c *EncapsulatingRateLimiter[string], ops []addOp) {
	for _, op := range ops {
		c.Add(op.key, op.rl, op.req)
	}
}

// assertForgetCalls verifies that every fake referenced by either `fakes` or
// the keys of `want` has exactly the Forget invocations recorded against it
// that `want` declares; a fake with no entry in `want` must have zero Forget
// invocations.
func assertForgetCalls(t *testing.T, reason string, want map[*fakeRateLimiter][]reconcile.Request, fakes []*fakeRateLimiter) {
	t.Helper()
	seen := map[*fakeRateLimiter]struct{}{}
	check := func(f *fakeRateLimiter) {
		if _, dup := seen[f]; dup {
			return
		}
		seen[f] = struct{}{}
		_, forgets, _ := f.snapshotCalls()
		if diff := cmp.Diff(want[f], forgets, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("\n%s\nForget calls on %q: -want, +got:\n%s", reason, f.name, diff)
		}
	}
	for _, f := range fakes {
		check(f)
	}
	for f := range want {
		check(f)
	}
}

func TestNewEncapsulatingRateLimiter(t *testing.T) {
	def := &fakeRateLimiter{name: "default"}
	c := NewEncapsulatingRateLimiter[string](def)

	if c == nil {
		t.Fatal("NewEncapsulatingRateLimiter returned nil")
	}
	if c.defaultRateLimiter != def {
		t.Errorf("defaultRateLimiter: want the supplied fake, got %v", c.defaultRateLimiter)
	}
	if c.inner == nil {
		t.Errorf("inner map must be initialised (non-nil) so Add can write to it without panicking")
	}
	if c.rateLimiters == nil {
		t.Errorf("rateLimiters map must be initialised (non-nil) so Add can write to it without panicking")
	}
	if len(c.inner) != 0 || len(c.rateLimiters) != 0 {
		t.Errorf("fresh EncapsulatingRateLimiter must have empty maps; got inner=%v, rateLimiters=%v", c.inner, c.rateLimiters)
	}
}

func TestEncapsulatingRateLimiterAdd(t *testing.T) {
	type args struct {
		pre []addOp
		op  addOp
	}
	type want struct {
		state      state
		forgetByRL map[*fakeRateLimiter][]reconcile.Request
	}
	cases := map[string]struct {
		reason string
		setup  func() (args, want)
	}{
		"NewKeyNewRequest": {
			reason: "Adding a request under a fresh key must register the supplied rate limiter and associate the request with that key.",
			setup: func() (args, want) {
				rl1 := &fakeRateLimiter{name: "rl1"}
				req1 := newReq("req-1")
				return args{
						op: addOp{key: "k1", rl: rl1, req: req1},
					}, want{
						state: state{
							Inner:        map[reconcile.Request]string{req1: "k1"},
							RateLimiters: map[string]workqueue.TypedRateLimiter[reconcile.Request]{"k1": rl1},
							Requests:     map[string]sets.Set[reconcile.Request]{"k1": sets.New[reconcile.Request](req1)},
						},
					}
			},
		},
		"ExistingKeyNewRequest": {
			reason: "Adding a new request under an existing key must associate it with that key while preserving the original rate limiter; the rl argument is ignored.",
			setup: func() (args, want) {
				rl1 := &fakeRateLimiter{name: "rl1"}
				rlIgnored := &fakeRateLimiter{name: "rl-ignored"}
				req1 := newReq("req-1")
				req2 := newReq("req-2")
				return args{
						pre: []addOp{{key: "k1", rl: rl1, req: req1}},
						op:  addOp{key: "k1", rl: rlIgnored, req: req2},
					}, want{
						state: state{
							Inner:        map[reconcile.Request]string{req1: "k1", req2: "k1"},
							RateLimiters: map[string]workqueue.TypedRateLimiter[reconcile.Request]{"k1": rl1},
							Requests:     map[string]sets.Set[reconcile.Request]{"k1": sets.New[reconcile.Request](req1, req2)},
						},
					}
			},
		},
		"ExistingKeySameRequest": {
			reason: "Re-adding the same request under the same key must be idempotent: state is unchanged and the original rate limiter is preserved.",
			setup: func() (args, want) {
				rl1 := &fakeRateLimiter{name: "rl1"}
				rlIgnored := &fakeRateLimiter{name: "rl-ignored"}
				req1 := newReq("req-1")
				return args{
						pre: []addOp{{key: "k1", rl: rl1, req: req1}},
						op:  addOp{key: "k1", rl: rlIgnored, req: req1},
					}, want{
						state: state{
							Inner:        map[reconcile.Request]string{req1: "k1"},
							RateLimiters: map[string]workqueue.TypedRateLimiter[reconcile.Request]{"k1": rl1},
							Requests:     map[string]sets.Set[reconcile.Request]{"k1": sets.New[reconcile.Request](req1)},
						},
					}
			},
		},
		"RequestMovesAndOldKeyDropped": {
			reason: "Adding an existing request under a new key must move the request, drop the old key (no remaining requests), and Forget the request on the old rate limiter.",
			setup: func() (args, want) {
				rl1 := &fakeRateLimiter{name: "rl1"}
				rl2 := &fakeRateLimiter{name: "rl2"}
				req1 := newReq("req-1")
				return args{
						pre: []addOp{{key: "k1", rl: rl1, req: req1}},
						op:  addOp{key: "k2", rl: rl2, req: req1},
					}, want{
						state: state{
							Inner:        map[reconcile.Request]string{req1: "k2"},
							RateLimiters: map[string]workqueue.TypedRateLimiter[reconcile.Request]{"k2": rl2},
							Requests:     map[string]sets.Set[reconcile.Request]{"k2": sets.New[reconcile.Request](req1)},
						},
						forgetByRL: map[*fakeRateLimiter][]reconcile.Request{
							rl1: {req1},
						},
					}
			},
		},
		"RequestMovesButOldKeyRetained": {
			reason: "Moving one of several requests off a key must Forget that request on the old rate limiter but keep the old key intact for the remaining requests.",
			setup: func() (args, want) {
				rl1 := &fakeRateLimiter{name: "rl1"}
				rl2 := &fakeRateLimiter{name: "rl2"}
				req1 := newReq("req-1")
				req2 := newReq("req-2")
				return args{
						pre: []addOp{
							{key: "k1", rl: rl1, req: req1},
							{key: "k1", rl: rl1, req: req2},
						},
						op: addOp{key: "k2", rl: rl2, req: req1},
					}, want{
						state: state{
							Inner:        map[reconcile.Request]string{req1: "k2", req2: "k1"},
							RateLimiters: map[string]workqueue.TypedRateLimiter[reconcile.Request]{"k1": rl1, "k2": rl2},
							Requests: map[string]sets.Set[reconcile.Request]{
								"k1": sets.New[reconcile.Request](req2),
								"k2": sets.New[reconcile.Request](req1),
							},
						},
						forgetByRL: map[*fakeRateLimiter][]reconcile.Request{
							rl1: {req1},
						},
					}
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a, w := tc.setup()
			c := NewEncapsulatingRateLimiter[string](nil)
			applyAdds(c, a.pre)
			c.Add(a.op.key, a.op.rl, a.op.req)

			if diff := cmp.Diff(w.state, snapshotOf(c), rlIdentity); diff != "" {
				t.Errorf("\n%s\nstate after Add(...): -want, +got:\n%s", tc.reason, diff)
			}

			fakes := collectFakes(append(a.pre, a.op)...)
			assertForgetCalls(t, tc.reason, w.forgetByRL, fakes)
		})
	}
}

func TestEncapsulatingRateLimiterRemove(t *testing.T) {
	type args struct {
		pre []addOp
		req reconcile.Request
	}
	type want struct {
		state      state
		forgetByRL map[*fakeRateLimiter][]reconcile.Request
	}
	cases := map[string]struct {
		reason string
		setup  func() (args, want)
	}{
		"UnknownRequest": {
			reason: "Removing an unknown request must be a no-op: no state change and no Forget calls.",
			setup: func() (args, want) {
				return args{
						req: newReq("req-1"),
					}, want{
						state: state{
							Inner:        map[reconcile.Request]string{},
							RateLimiters: map[string]workqueue.TypedRateLimiter[reconcile.Request]{},
							Requests:     map[string]sets.Set[reconcile.Request]{},
						},
					}
			},
		},
		"OnlyRequestForKey": {
			reason: "Removing the last request for a key must Forget the request on the rate limiter and drop the key from the registry.",
			setup: func() (args, want) {
				rl1 := &fakeRateLimiter{name: "rl1"}
				req1 := newReq("req-1")
				return args{
						pre: []addOp{{key: "k1", rl: rl1, req: req1}},
						req: req1,
					}, want{
						state: state{
							Inner:        map[reconcile.Request]string{},
							RateLimiters: map[string]workqueue.TypedRateLimiter[reconcile.Request]{},
							Requests:     map[string]sets.Set[reconcile.Request]{},
						},
						forgetByRL: map[*fakeRateLimiter][]reconcile.Request{
							rl1: {req1},
						},
					}
			},
		},
		"OneOfMultipleRequests": {
			reason: "Removing one of several requests for a key must Forget that request on the rate limiter while keeping the key and remaining requests intact.",
			setup: func() (args, want) {
				rl1 := &fakeRateLimiter{name: "rl1"}
				req1 := newReq("req-1")
				req2 := newReq("req-2")
				return args{
						pre: []addOp{
							{key: "k1", rl: rl1, req: req1},
							{key: "k1", rl: rl1, req: req2},
						},
						req: req1,
					}, want{
						state: state{
							Inner:        map[reconcile.Request]string{req2: "k1"},
							RateLimiters: map[string]workqueue.TypedRateLimiter[reconcile.Request]{"k1": rl1},
							Requests:     map[string]sets.Set[reconcile.Request]{"k1": sets.New[reconcile.Request](req2)},
						},
						forgetByRL: map[*fakeRateLimiter][]reconcile.Request{
							rl1: {req1},
						},
					}
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a, w := tc.setup()
			c := NewEncapsulatingRateLimiter[string](nil)
			applyAdds(c, a.pre)
			c.Remove(a.req)

			if diff := cmp.Diff(w.state, snapshotOf(c), rlIdentity); diff != "" {
				t.Errorf("\n%s\nstate after Remove(...): -want, +got:\n%s", tc.reason, diff)
			}

			assertForgetCalls(t, tc.reason, w.forgetByRL, collectFakes(a.pre...))
		})
	}
}

func TestEncapsulatingRateLimiterWhen(t *testing.T) {
	const defaultDur = 5 * time.Second
	const keyedDur = 7 * time.Second

	cases := map[string]struct {
		reason         string
		pre            func(keyedRL *fakeRateLimiter, req reconcile.Request) []addOp
		wantDuration   time.Duration
		wantCalledFake string // "default" or "keyed"
	}{
		"UnknownRequestUsesDefault": {
			reason: "When a request has no key registered, When must delegate to the default rate limiter.",
			pre: func(_ *fakeRateLimiter, _ reconcile.Request) []addOp {
				return nil
			},
			wantDuration:   defaultDur,
			wantCalledFake: "default",
		},
		"RegisteredRequestUsesKeyRateLimiter": {
			reason: "When a request has a key registered, When must delegate to that key's rate limiter and not the default.",
			pre: func(keyedRL *fakeRateLimiter, req reconcile.Request) []addOp {
				return []addOp{{key: "k1", rl: keyedRL, req: req}}
			},
			wantDuration:   keyedDur,
			wantCalledFake: "keyed",
		},
		"RegisteredButMissingRateLimiterFallsBackToDefault": {
			reason: "If a request's key lookup hits but the corresponding rate limiter has been removed, When must fall back to the default rate limiter (defensive lookup).",
			pre: func(keyedRL *fakeRateLimiter, req reconcile.Request) []addOp {
				// We can't easily produce this state through public API; the
				// closure below mutates internal state directly. Returning nil
				// here keeps applyAdds a no-op, and the test then sets up the
				// inconsistent state before calling When.
				return nil
			},
			wantDuration:   defaultDur,
			wantCalledFake: "default",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defaultRL := &fakeRateLimiter{name: "default", whenDuration: defaultDur}
			keyedRL := &fakeRateLimiter{name: "keyed", whenDuration: keyedDur}
			req := newReq("req-1")
			c := NewEncapsulatingRateLimiter[string](defaultRL)
			applyAdds(c, tc.pre(keyedRL, req))

			// Special-case: poison the registry to simulate "inner says key
			// exists, rateLimiters says it does not". This exercises the
			// !rOK branch of getRateLimiterFor.
			if name == "RegisteredButMissingRateLimiterFallsBackToDefault" {
				c.mu.Lock()
				c.inner[req] = "ghost-key"
				c.mu.Unlock()
			}

			got := c.When(req)

			if got != tc.wantDuration {
				t.Errorf("\n%s\nWhen(req): want %s, got %s", tc.reason, tc.wantDuration, got)
			}

			defaultWhens, _, _ := defaultRL.snapshotCalls()
			keyedWhens, _, _ := keyedRL.snapshotCalls()
			wantDefault := []reconcile.Request(nil)
			wantKeyed := []reconcile.Request(nil)
			switch tc.wantCalledFake {
			case "default":
				wantDefault = []reconcile.Request{req}
			case "keyed":
				wantKeyed = []reconcile.Request{req}
			}
			if diff := cmp.Diff(wantDefault, defaultWhens, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("\n%s\nWhen calls on default: -want, +got:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(wantKeyed, keyedWhens, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("\n%s\nWhen calls on keyed: -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestEncapsulatingRateLimiterForget(t *testing.T) {
	cases := map[string]struct {
		reason         string
		pre            func(keyedRL *fakeRateLimiter, req reconcile.Request) []addOp
		wantCalledFake string // "default" or "keyed"
	}{
		"UnknownRequestUsesDefault": {
			reason: "When a request has no key registered, Forget must be delegated to the default rate limiter.",
			pre: func(_ *fakeRateLimiter, _ reconcile.Request) []addOp {
				return nil
			},
			wantCalledFake: "default",
		},
		"RegisteredRequestUsesKeyRateLimiter": {
			reason: "When a request has a key registered, Forget must be delegated to that key's rate limiter.",
			pre: func(keyedRL *fakeRateLimiter, req reconcile.Request) []addOp {
				return []addOp{{key: "k1", rl: keyedRL, req: req}}
			},
			wantCalledFake: "keyed",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defaultRL := &fakeRateLimiter{name: "default"}
			keyedRL := &fakeRateLimiter{name: "keyed"}
			req := newReq("req-1")
			c := NewEncapsulatingRateLimiter[string](defaultRL)
			applyAdds(c, tc.pre(keyedRL, req))

			c.Forget(req)

			_, defaultForgets, _ := defaultRL.snapshotCalls()
			_, keyedForgets, _ := keyedRL.snapshotCalls()
			wantDefault := []reconcile.Request(nil)
			wantKeyed := []reconcile.Request(nil)
			switch tc.wantCalledFake {
			case "default":
				wantDefault = []reconcile.Request{req}
			case "keyed":
				wantKeyed = []reconcile.Request{req}
			}
			if diff := cmp.Diff(wantDefault, defaultForgets, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("\n%s\nForget calls on default: -want, +got:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(wantKeyed, keyedForgets, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("\n%s\nForget calls on keyed: -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestEncapsulatingRateLimiterNumRequeues(t *testing.T) {
	const defaultRequeues = 3
	const keyedRequeues = 11

	cases := map[string]struct {
		reason         string
		pre            func(keyedRL *fakeRateLimiter, req reconcile.Request) []addOp
		wantCount      int
		wantCalledFake string // "default" or "keyed"
	}{
		"UnknownRequestUsesDefault": {
			reason: "When a request has no key registered, NumRequeues must come from the default rate limiter.",
			pre: func(_ *fakeRateLimiter, _ reconcile.Request) []addOp {
				return nil
			},
			wantCount:      defaultRequeues,
			wantCalledFake: "default",
		},
		"RegisteredRequestUsesKeyRateLimiter": {
			reason: "When a request has a key registered, NumRequeues must come from that key's rate limiter.",
			pre: func(keyedRL *fakeRateLimiter, req reconcile.Request) []addOp {
				return []addOp{{key: "k1", rl: keyedRL, req: req}}
			},
			wantCount:      keyedRequeues,
			wantCalledFake: "keyed",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defaultRL := &fakeRateLimiter{name: "default", requeueCount: defaultRequeues}
			keyedRL := &fakeRateLimiter{name: "keyed", requeueCount: keyedRequeues}
			req := newReq("req-1")
			c := NewEncapsulatingRateLimiter[string](defaultRL)
			applyAdds(c, tc.pre(keyedRL, req))

			got := c.NumRequeues(req)

			if got != tc.wantCount {
				t.Errorf("\n%s\nNumRequeues(req): want %d, got %d", tc.reason, tc.wantCount, got)
			}

			_, _, defaultRequeueCalls := defaultRL.snapshotCalls()
			_, _, keyedRequeueCalls := keyedRL.snapshotCalls()
			wantDefault := []reconcile.Request(nil)
			wantKeyed := []reconcile.Request(nil)
			switch tc.wantCalledFake {
			case "default":
				wantDefault = []reconcile.Request{req}
			case "keyed":
				wantKeyed = []reconcile.Request{req}
			}
			if diff := cmp.Diff(wantDefault, defaultRequeueCalls, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("\n%s\nNumRequeues calls on default: -want, +got:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(wantKeyed, keyedRequeueCalls, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("\n%s\nNumRequeues calls on keyed: -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

// collectFakes returns the distinct *fakeRateLimiter instances referenced by
// the given Add operations, in order of first appearance.
func collectFakes(ops ...addOp) []*fakeRateLimiter {
	seen := map[*fakeRateLimiter]struct{}{}
	var out []*fakeRateLimiter
	for _, op := range ops {
		f, ok := op.rl.(*fakeRateLimiter)
		if !ok {
			continue
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}
