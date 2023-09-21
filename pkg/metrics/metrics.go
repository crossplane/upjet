// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	promNSUpjet     = "upjet"
	promSysTF       = "terraform"
	promSysResource = "resource"
)

var (
	// CLITime is the Terraform CLI execution times histogram.
	CLITime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: promNSUpjet,
		Subsystem: promSysTF,
		Name:      "cli_duration",
		Help:      "Measures in seconds how long it takes a Terraform CLI invocation to complete",
		Buckets:   []float64{1.0, 3, 5, 10, 15, 30, 60, 120, 300},
	}, []string{"subcommand", "mode"})

	// ExternalAPITime is the SDK processing times histogram.
	ExternalAPITime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: promNSUpjet,
		Subsystem: promSysResource,
		Name:      "ext_api_duration",
		Help:      "Measures in seconds how long it takes a Cloud SDK call to complete",
		Buckets:   []float64{1, 5, 10, 15, 30, 60, 120, 300, 600, 1800, 3600},
	}, []string{"operation"})

	// DeletionTime is the histogram metric for collecting statistics on the
	// intervals between the deletion timestamp and the moment when
	// the resource is observed to be missing (actually deleted).
	DeletionTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: promNSUpjet,
		Subsystem: promSysResource,
		Name:      "deletion_seconds",
		Help:      "Measures in seconds how long it takes for a resource to be deleted",
		Buckets:   []float64{1, 5, 10, 15, 30, 60, 120, 300, 600, 1800, 3600},
	}, []string{"group", "version", "kind"})

	// ReconcileDelay is the histogram metric for collecting statistics on the
	// delays between when the expected reconciles of an up-to-date resource
	// should happen and when the resource is actually reconciled. Only
	// delays from the expected reconcile times are considered.
	ReconcileDelay = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: promNSUpjet,
		Subsystem: promSysResource,
		Name:      "reconcile_delay_seconds",
		Help:      "Measures in seconds how long the reconciles for a resource have been delayed from the configured poll periods",
		Buckets:   []float64{1, 5, 10, 15, 30, 60, 120, 300, 600, 1800, 3600},
	}, []string{"group", "version", "kind"})

	// CLIExecutions are the active number of terraform CLI invocations.
	CLIExecutions = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: promNSUpjet,
		Subsystem: promSysTF,
		Name:      "active_cli_invocations",
		Help:      "The number of active (running) Terraform CLI invocations",
	}, []string{"subcommand", "mode"})

	// TFProcesses are the active number of
	// terraform CLI & Terraform provider processes running.
	TFProcesses = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: promNSUpjet,
		Subsystem: promSysTF,
		Name:      "running_processes",
		Help:      "The number of running Terraform CLI and Terraform provider processes",
	}, []string{"type"})

	// TTRMeasurements are the time-to-readiness measurements for
	// the managed resources.
	TTRMeasurements = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: promNSUpjet,
		Subsystem: promSysResource,
		Name:      "ttr",
		Help:      "Measures in seconds the time-to-readiness (TTR) for managed resources",
		Buckets:   []float64{1, 5, 10, 15, 30, 60, 120, 300, 600, 1800, 3600},
	}, []string{"group", "version", "kind"})
)

var _ manager.Runnable = &MetricRecorder{}

type MetricRecorder struct {
	observations sync.Map
	gvk          schema.GroupVersionKind
	cluster      cluster.Cluster

	pollInterval time.Duration
}

type Observations struct {
	expectedReconcileTime *time.Time
	observeReconcileDelay bool
}

func NewMetricRecorder(gvk schema.GroupVersionKind, c cluster.Cluster, pollInterval time.Duration) *MetricRecorder {
	return &MetricRecorder{
		gvk:          gvk,
		cluster:      c,
		pollInterval: pollInterval,
	}
}

func (r *MetricRecorder) SetReconcileTime(name string) {
	if r == nil {
		return
	}
	o, ok := r.observations.Load(name)
	if !ok {
		o = &Observations{}
		r.observations.Store(name, o)
	}
	t := time.Now().Add(r.pollInterval)
	o.(*Observations).expectedReconcileTime = &t
	o.(*Observations).observeReconcileDelay = true
}

func (r *MetricRecorder) ObserveReconcileDelay(gvk schema.GroupVersionKind, name string) {
	if r == nil {
		return
	}
	o, _ := r.observations.Load(name)
	if o == nil || !o.(*Observations).observeReconcileDelay || o.(*Observations).expectedReconcileTime == nil {
		return
	}
	d := time.Now().Sub(*o.(*Observations).expectedReconcileTime)
	if d < 0 {
		d = 0
	}
	ReconcileDelay.WithLabelValues(gvk.Group, gvk.Version, gvk.Kind).Observe(d.Seconds())
	o.(*Observations).observeReconcileDelay = false
}

func (r *MetricRecorder) Start(ctx context.Context) error {
	inf, err := r.cluster.GetCache().GetInformerForKind(ctx, r.gvk)
	if err != nil {
		return errors.Wrapf(err, "cannot get informer for metric recorder for resource %s", r.gvk)
	}

	registered, err := inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			if final, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = final.Obj
			}
			managed := obj.(resource.Managed)
			r.observations.Delete(managed.GetName())
		},
	})
	if err != nil {
		return errors.Wrap(err, "cannot add delete event handler to informer for metric recorder")
	}
	defer inf.RemoveEventHandler(registered) //nolint:errcheck // this happens on destruction. We cannot do anything anyway.

	<-ctx.Done()

	return nil
}

func init() {
	metrics.Registry.MustRegister(CLITime, CLIExecutions, TFProcesses, TTRMeasurements, ExternalAPITime, DeletionTime, ReconcileDelay)
}
