// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package ratelimiter

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type requestSet struct {
	rateLimiter workqueue.TypedRateLimiter[reconcile.Request]
	requests sets.Set[reconcile.Request]
}

// EncapsulatingRateLimiter
type EncapsulatingRateLimiter[K comparable] struct {
	defaultRateLimiter workqueue.TypedRateLimiter[reconcile.Request]

	inner map[reconcile.Request]K
	rateLimiters map[K]requestSet
	mu sync.RWMutex
}

// Add registers the specified rate limiter with this EncapsulatingRateLimiter
// using the specified key and associates the given request with it.
// If there already exists a rate limiter for the given key, the existing
// rate limiter's state is preserved.
func (c *EncapsulatingRateLimiter[K]) Add(key K, rl workqueue.TypedRateLimiter[reconcile.Request], req reconcile.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if r, ok := c.rateLimiters[key]; !ok {
		c.rateLimiters[key] = requestSet{
			rateLimiter: rl,
			requests:    sets.New[reconcile.Request](req),
		}
	} else {
		r.requests.Insert(req)
	}
	// disassociate req from its previous rate limiter, if it exists.
	if prevKey, ok := c.inner[req]; ok && prevKey != key {
		c.cleanup(req)
	}
	c.inner[req] = key
}

// Remove removes the given request from its associated rate limiter.
// If the rate limiter has no remaining associated requests with it,
// it's removed from the registry.
func (c *EncapsulatingRateLimiter[K]) Remove(req reconcile.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// de-associate the request from all its rate limiter.
	c.cleanup(req)
}

// cleanup de-associates the given request from its current rate limiter.
// If the current rate limiter has zero requests associated with it after
// req has been disassociated from it, then it's removed from the registry.
// No action is performed if req has no previous rate limiter associated.
func (c *EncapsulatingRateLimiter[K]) cleanup(req reconcile.Request) {
	key, ok := c.inner[req]
	if !ok {
		return
	}
	delete(c.inner, req)
	rl, rOK := c.rateLimiters[key]
	if !rOK {
		return
	}
	// disassociate req from its current rate limiter.
	rl.requests.Delete(req)
	// forget the request in its current rate limiter.
	if rl.rateLimiter != nil {
		rl.rateLimiter.Forget(req)
	}
	// check if there are any remaining requests associated with
	// the rate limiter. If not, delete it from the registry.
	if rl.requests.Len() == 0 {
		// then remove the rate limiter from the registry.
		delete(c.rateLimiters, key)
	}
}

func (c *EncapsulatingRateLimiter[K]) getRateLimiterFor(req reconcile.Request) workqueue.TypedRateLimiter[reconcile.Request] {
	rl := c.defaultRateLimiter
	if key, ok := c.inner[req]; ok {
		if r, rOK := c.rateLimiters[key]; rOK {
			rl = r.rateLimiter
		}
	}
	return rl
}

func (c *EncapsulatingRateLimiter[K]) When(req reconcile.Request) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rl := c.getRateLimiterFor(req)
	return rl.When(req)
}

func (c *EncapsulatingRateLimiter[K]) Forget(req reconcile.Request) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	c.getRateLimiterFor(req).Forget(req)
}

func (c *EncapsulatingRateLimiter[K]) NumRequeues(req reconcile.Request) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.getRateLimiterFor(req).NumRequeues(req)
}

func NewEncapsulatingRateLimiter[K comparable](defaultRateLimiter workqueue.TypedRateLimiter[reconcile.Request]) *EncapsulatingRateLimiter[K] {
	return &EncapsulatingRateLimiter[K]{
		defaultRateLimiter: defaultRateLimiter,
		inner: make(map[reconcile.Request]K),
		rateLimiters: make(map[K]requestSet),
		mu: sync.RWMutex{},
	}
}
