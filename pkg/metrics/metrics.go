// Copyright 2023 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
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
		Buckets:   []float64{10, 15, 30, 60, 120, 300, 600, 1800, 3600},
	}, []string{"group", "version", "kind"})
)

func init() {
	metrics.Registry.MustRegister(CLITime, CLIExecutions, TFProcesses, TTRMeasurements)
}
