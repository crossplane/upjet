// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"fmt"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
)

// ErrDiffComputationNotSupported contains a sentinel error message that's used
// to check, in IsDiffComputationNotSupportedError, whether a target error is
// a diff computation error. In certain error stacks, we've observed that
// the type information can be lost because the original diff computation not
// supported error is not properly wrapped.
// ErrDiffComputationNotSupported is also used as the sentinel error
// in the implementation of diffComputationNotSupportedError.Is.
var ErrDiffComputationNotSupported = errors.New("diff computation is not supported")

type diffComputationNotSupportedError struct {
	cause error
}

// NewDiffComputationNotSupportedError returns a new error representing
// a case when diff computation is not supported.
// The client needs to handle this case gracefully, e.g., by raw-diffing
// the managed resource manifests.
func NewDiffComputationNotSupportedError(cause error) error {
	return &diffComputationNotSupportedError{
		cause: cause,
	}
}

func (d *diffComputationNotSupportedError) Error() string {
	if d.cause == nil {
		return ErrDiffComputationNotSupported.Error()
	}
	return fmt.Sprintf("%s: %v", ErrDiffComputationNotSupported.Error(), d.cause)
}

func (d *diffComputationNotSupportedError) Unwrap() error {
	return d.cause
}

func (d *diffComputationNotSupportedError) Is(target error) bool {
	return target == ErrDiffComputationNotSupported
}

// IsDiffComputationNotSupportedError checks whether a given error denotes
// that diff computation is not supported.
func IsDiffComputationNotSupportedError(target error) bool {
	if target == nil {
		return false
	}

	var e *diffComputationNotSupportedError
	ok := errors.As(target, &e)
	if ok {
		return true
	}

	// sometimes the original diffComputationNotSupportedError is not wrapped.
	// For such cases we also make a sentinel error message check.
	return strings.Contains(target.Error(), ErrDiffComputationNotSupported.Error())
}
