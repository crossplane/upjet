// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package reconciliationpolicy

import (
	"context"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/apis/configuration/v1alpha1"
	"github.com/crossplane/upjet/v2/pkg/internal/ratelimiter"
)

// Source represents a configuration source for reconciliation policies.
type Source func(context.Context, client.Client, resource.Managed) (*v1alpha1.ReconciliationPolicy, error)

type ExponentialFailureRateLimiter struct {
	*ratelimiter.EncapsulatingRateLimiter[efrlKey]

	defaultBaseDelay time.Duration
	defaultMaxDelay  time.Duration
}

type efrlKey struct {
	maxDelay, baseDelay metav1.Duration
}
