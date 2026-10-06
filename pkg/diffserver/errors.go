// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"github.com/crossplane/upjet/v2/pkg/diffserver/internal/plan"
)

// ErrDiffComputationNotSupported is the sentinel every error returned by
// NewDiffComputationNotSupportedError matches, so that a caller can test for
// the condition with errors.Is.
var ErrDiffComputationNotSupported = plan.ErrDiffComputationNotSupported

// NewDiffComputationNotSupportedError returns a new error representing a case
// when diff computation is not supported. A provider returns it from the hooks
// the diff server calls, such as a Terraform setup function that refuses to
// reach the network. The client needs to handle this case gracefully, e.g., by
// raw-diffing the managed resource manifests.
func NewDiffComputationNotSupportedError(cause error) error {
	return plan.NewDiffComputationNotSupportedError(cause)
}

// IsDiffComputationNotSupportedError checks whether a given error denotes that
// diff computation is not supported. Unlike errors.Is against
// ErrDiffComputationNotSupported, it only matches errors this package
// produced.
func IsDiffComputationNotSupportedError(target error) bool {
	return plan.IsDiffComputationNotSupportedError(target)
}
