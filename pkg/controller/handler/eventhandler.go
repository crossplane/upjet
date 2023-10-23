// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
)

const NoRateLimiter = ""

// EventHandler handles Kubernetes events by queueing reconcile requests for
// objects and allows upjet components to queue reconcile requests.
type EventHandler struct {
	innerHandler   handler.EventHandler
	queue          workqueue.RateLimitingInterface
	rateLimiterMap map[string]workqueue.RateLimiter
	logger         logging.Logger
	mu             *sync.RWMutex
}

// Option configures an option for the EventHandler.
type Option func(eventHandler *EventHandler)

// WithLogger configures the logger for the EventHandler.
func WithLogger(logger logging.Logger) Option {
	return func(eventHandler *EventHandler) {
		eventHandler.logger = logger
	}
}

// NewEventHandler initializes a new EventHandler instance.
func NewEventHandler(opts ...Option) *EventHandler {
	eh := &EventHandler{
		innerHandler:   &handler.EnqueueRequestForObject{},
		mu:             &sync.RWMutex{},
		rateLimiterMap: make(map[string]workqueue.RateLimiter),
	}
	for _, o := range opts {
		o(eh)
	}
	return eh
}

// RequestReconcile requeues a reconciliation request for the specified name.
// Returns true if the reconcile request was successfully queued.
func (e *EventHandler) RequestReconcile(rateLimiterName, name string, failureLimit *int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.queue == nil {
		return false
	}
	logger := e.logger.WithValues("name", name)
	item := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: name,
		},
	}
	var when time.Duration = 0
	if rateLimiterName != NoRateLimiter {
		rateLimiter := e.rateLimiterMap[rateLimiterName]
		if rateLimiter == nil {
			rateLimiter = workqueue.DefaultControllerRateLimiter()
			e.rateLimiterMap[rateLimiterName] = rateLimiter
		}
		if failureLimit != nil && rateLimiter.NumRequeues(item) > *failureLimit {
			logger.Info("Failure limit has been exceeded.", "failureLimit", *failureLimit, "numRequeues", rateLimiter.NumRequeues(item))
			return false
		}
		when = rateLimiter.When(item)
	}
	e.queue.AddAfter(item, when)
	logger.Debug("Reconcile request has been requeued.", "rateLimiterName", rateLimiterName, "when", when)
	return true
}

// Forget indicates that the reconcile retries is finished for
// the specified name.
func (e *EventHandler) Forget(rateLimiterName, name string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	rateLimiter := e.rateLimiterMap[rateLimiterName]
	if rateLimiter == nil {
		return
	}
	rateLimiter.Forget(reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: name,
		},
	})
}

func (e *EventHandler) setQueue(limitingInterface workqueue.RateLimitingInterface) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.queue == nil {
		e.queue = limitingInterface
	}
}

func (e *EventHandler) Create(ctx context.Context, ev event.CreateEvent, limitingInterface workqueue.RateLimitingInterface) {
	e.setQueue(limitingInterface)
	e.logger.Debug("Calling the inner handler for Create event.", "name", ev.Object.GetName(), "queueLength", limitingInterface.Len())
	e.innerHandler.Create(ctx, ev, limitingInterface)
}

func (e *EventHandler) Update(ctx context.Context, ev event.UpdateEvent, limitingInterface workqueue.RateLimitingInterface) {
	e.setQueue(limitingInterface)
	e.logger.Debug("Calling the inner handler for Update event.", "name", ev.ObjectOld.GetName(), "queueLength", limitingInterface.Len())
	e.innerHandler.Update(ctx, ev, limitingInterface)
}

func (e *EventHandler) Delete(ctx context.Context, ev event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {
	e.setQueue(limitingInterface)
	e.logger.Debug("Calling the inner handler for Delete event.", "name", ev.Object.GetName(), "queueLength", limitingInterface.Len())
	e.innerHandler.Delete(ctx, ev, limitingInterface)
}

func (e *EventHandler) Generic(ctx context.Context, ev event.GenericEvent, limitingInterface workqueue.RateLimitingInterface) {
	e.setQueue(limitingInterface)
	e.logger.Debug("Calling the inner handler for Generic event.", "name", ev.Object.GetName(), "queueLength", limitingInterface.Len())
	e.innerHandler.Generic(ctx, ev, limitingInterface)
}
