// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ExponentialFailureRateLimiter configures the
// Exponential Failure Rate Limiter configuration parameters.
// Currently, the base-delay and the max-delay are configurable.
// API Validations:
// - baseDelay must be >= 1s, if specified.
// - maxDelay must be >= 60s, if specified.
// - maxDelay must be >= baseDelay, if both are specified.
//
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="!has(self.maxDelay) || !has(self.baseDelay) || duration(self.maxDelay) >= duration(self.baseDelay)",message="maxDelay must be greater than or equal to baseDelay"
type ExponentialFailureRateLimiter struct {
	// MaxDelay is the maximum delay between retries.
	//
	// +optional
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('60s')",message="maxDelay must be at least 60s"
	MaxDelay *metav1.Duration `json:"maxDelay,omitempty"`

	// BaseDelay is the initial delay between retries.
	//
	// +optional
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1s')",message="baseDelay must be at least 1s"
	BaseDelay *metav1.Duration `json:"baseDelay,omitempty"`
}

// ReconciliationPolicy configures how a managed resource is reconciled.
// It currently allows overriding the controller's failure rate limiter
// parameters on a per-resource basis via ExponentialFailureRateLimiter.
//
// +kubebuilder:object:generate=true
type ReconciliationPolicy struct {
	// ExponentialFailureRateLimiter, when set, overrides the parameters of the
	// exponential failure rate limiter used to schedule retries for the
	// managed resource that this policy applies to.
	//
	// +optional
	ExponentialFailureRateLimiter *ExponentialFailureRateLimiter `json:"exponentialFailureRateLimiter"`
}
