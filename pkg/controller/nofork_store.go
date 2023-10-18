package controller

import (
	"sync"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	tfsdk "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"k8s.io/apimachinery/pkg/types"

	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/terraform"
)

type AsyncTracker struct {
	LastOperation *terraform.Operation
	logger        logging.Logger
	mu            *sync.Mutex
	tfID          string
	tfState       *tfsdk.InstanceState
}

type AsyncTrackerOption func(manager *AsyncTracker)

// WithAsyncTrackerLogger sets the logger of AsyncTracker.
func WithAsyncTrackerLogger(l logging.Logger) AsyncTrackerOption {
	return func(w *AsyncTracker) {
		w.logger = l
	}
}
func NewAsyncTracker(opts ...AsyncTrackerOption) *AsyncTracker {
	w := &AsyncTracker{
		LastOperation: &terraform.Operation{},
		logger:        logging.NewNopLogger(),
		mu:            &sync.Mutex{},
	}
	for _, f := range opts {
		f(w)
	}
	return w
}

func (a *AsyncTracker) GetTfState() *tfsdk.InstanceState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tfState
}

func (a *AsyncTracker) HasState() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tfState != nil && a.tfState.ID != ""
}

func (a *AsyncTracker) SetTfState(state *tfsdk.InstanceState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tfState = state
}

func (a *AsyncTracker) GetTfID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tfState == nil {
		return ""
	}
	return a.tfState.ID
}

func (a *AsyncTracker) SetTfID(tfId string) {
	//a.mu.Lock()
	//defer a.mu.Unlock()
	a.tfID = tfId
}

type OperationTrackerStore struct {
	store  map[types.UID]*AsyncTracker
	logger logging.Logger
	mu     *sync.Mutex
}

func NewOperationStore(l logging.Logger) *OperationTrackerStore {
	ops := &OperationTrackerStore{
		store:  map[types.UID]*AsyncTracker{},
		logger: l,
		mu:     &sync.Mutex{},
	}

	return ops
}

func (ops *OperationTrackerStore) Tracker(tr resource.Terraformed) *AsyncTracker {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	tracker, ok := ops.store[tr.GetUID()]
	if !ok {
		l := ops.logger.WithValues("trackerUID", tr.GetUID(), "resourceName", tr.GetName())
		ops.store[tr.GetUID()] = NewAsyncTracker(WithAsyncTrackerLogger(l))
		tracker = ops.store[tr.GetUID()]
	}
	return tracker
}

func (ops *OperationTrackerStore) RemoveTracker(obj xpresource.Object) error {
	ops.mu.Lock()
	defer ops.mu.Unlock()
	delete(ops.store, obj.GetUID())
	return nil
}
