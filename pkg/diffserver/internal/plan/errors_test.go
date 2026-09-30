// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"io"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/google/go-cmp/cmp"
)

func TestDiffComputationNotSupportedErrorError(t *testing.T) {
	cases := map[string]struct {
		reason string
		cause  error
		want   string
	}{
		"WithoutCause": {
			reason: "The sentinel message should be returned when there is no cause.",
			want:   "diff computation is not supported",
		},
		"WithCause": {
			reason: "The cause should be appended to the sentinel message so that it survives being rendered as a string.",
			cause:  io.EOF,
			want:   "diff computation is not supported: EOF",
		},
		"WithWrappedCause": {
			reason: "The cause should be rendered in full, including its own wrapping.",
			cause:  errors.Wrap(io.EOF, "cannot read the schema"),
			want:   "diff computation is not supported: cannot read the schema: EOF",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := NewDiffComputationNotSupportedError(tc.cause)
			if diff := cmp.Diff(tc.want, err.Error()); diff != "" {
				t.Errorf("\n%s\nError(): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

// TestErrorAlwaysCarriesSentinelText guards the invariant that
// IsDiffComputationNotSupportedError's substring fallback depends on: the
// rendered message must always contain the sentinel text, whatever the cause.
func TestErrorAlwaysCarriesSentinelText(t *testing.T) {
	for name, cause := range map[string]error{
		"NoCause":      nil,
		"SimpleCause":  io.EOF,
		"WrappedCause": errors.Wrap(io.EOF, "cannot read the schema"),
		"EmptyCause":   errors.New(""),
	} {
		t.Run(name, func(t *testing.T) {
			got := NewDiffComputationNotSupportedError(cause).Error()
			if !IsDiffComputationNotSupportedError(errors.New(got)) {
				t.Errorf("Error() = %q, which does not carry the sentinel text %q, so a flattened error would no longer be recognized",
					got, ErrDiffComputationNotSupported.Error())
			}
		})
	}
}

func TestDiffComputationNotSupportedErrorUnwrap(t *testing.T) {
	cases := map[string]struct {
		reason string
		cause  error
		want   error
	}{
		"NoCause": {
			reason: "Unwrap should report no wrapped error when the error was constructed without a cause.",
			want:   nil,
		},
		"WithCause": {
			reason: "Unwrap should expose the cause so that errors.Is and errors.As can traverse it.",
			cause:  io.EOF,
			want:   io.EOF,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// The comparison is intentionally by identity: Unwrap must return
			// the cause itself, so errors.Is would be too weak here.
			if got := errors.Unwrap(NewDiffComputationNotSupportedError(tc.cause)); got != tc.want { //nolint:errorlint // see above
				t.Errorf("\n%s\nUnwrap(): want %v, got %v", tc.reason, tc.want, got)
			}
		})
	}
}

func TestErrorsIs(t *testing.T) {
	withCause := NewDiffComputationNotSupportedError(io.EOF)
	noCause := NewDiffComputationNotSupportedError(nil)

	cases := map[string]struct {
		reason string
		err    error
		target error
		want   bool
	}{
		"Sentinel": {
			reason: "The Is method should report the error as equivalent to the sentinel.",
			err:    noCause,
			target: ErrDiffComputationNotSupported,
			want:   true,
		},
		"SentinelThroughWrapping": {
			reason: "The sentinel should still be matched once the error has been wrapped, because the wrapper preserves Unwrap.",
			err:    errors.Wrap(noCause, "outer"),
			target: ErrDiffComputationNotSupported,
			want:   true,
		},
		"CauseReachableViaUnwrap": {
			reason: "The cause should remain matchable, so that wrapping it in this type does not hide it.",
			err:    withCause,
			target: io.EOF,
			want:   true,
		},
		"CauseAbsent": {
			reason: "An error constructed without a cause should not match an unrelated target.",
			err:    noCause,
			target: io.EOF,
			want:   false,
		},
		"DistinctInstances": {
			reason: "Two independently constructed errors are different values, so errors.Is should not match them; that is what IsDiffComputationNotSupportedError is for.",
			err:    noCause,
			target: NewDiffComputationNotSupportedError(nil),
			want:   false,
		},
		"Identity": {
			reason: "An error should match itself by identity.",
			err:    noCause,
			target: noCause,
			want:   true,
		},
		"NilTarget": {
			reason: "A non-nil error should not match a nil target.",
			err:    noCause,
			target: nil,
			want:   false,
		},
		"UnrelatedErrorAgainstSentinel": {
			reason: "An unrelated error should not match the sentinel.",
			err:    io.EOF,
			target: ErrDiffComputationNotSupported,
			want:   false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, errors.Is(tc.err, tc.target)); diff != "" {
				t.Errorf("\n%s\nerrors.Is(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestIsDiffComputationNotSupportedError(t *testing.T) {
	cases := map[string]struct {
		reason string
		err    error
		want   bool
	}{
		"Nil": {
			reason: "A nil error does not denote an unsupported diff computation, and the check must not panic.",
			err:    nil,
			want:   false,
		},
		"Direct": {
			reason: "An error of this type should be recognized.",
			err:    NewDiffComputationNotSupportedError(nil),
			want:   true,
		},
		"WithCause": {
			reason: "A cause should not prevent the error from being recognized.",
			err:    NewDiffComputationNotSupportedError(io.EOF),
			want:   true,
		},
		"Wrapped": {
			reason: "The type should be recovered through a wrapper that preserves Unwrap.",
			err:    errors.Wrap(NewDiffComputationNotSupportedError(nil), "outer"),
			want:   true,
		},
		"DoublyWrapped": {
			reason: "The type should be recovered through any depth of Unwrap-preserving wrappers.",
			err:    errors.Wrap(errors.Wrap(NewDiffComputationNotSupportedError(io.EOF), "inner"), "outer"),
			want:   true,
		},
		"Unrelated": {
			reason: "An unrelated error should not be recognized.",
			err:    io.EOF,
			want:   false,
		},
		"Sentinel": {
			reason: "The bare sentinel denotes the condition, even though it is not an error of this type.",
			err:    ErrDiffComputationNotSupported,
			want:   true,
		},
		"Flattened": {
			reason: "Once the type information is lost only the message survives, and the fallback should still recognize it because Error() always carries the sentinel text.",
			err:    errors.New(NewDiffComputationNotSupportedError(io.EOF).Error()),
			want:   true,
		},
		"FlattenedAndRewrapped": {
			reason: "A flattened message that has been given further context should still be recognized.",
			err:    errors.Wrap(errors.New(NewDiffComputationNotSupportedError(nil).Error()), "cannot compute diff"),
			want:   true,
		},
		"UnrelatedErrorMentioningTheSentinelText": {
			reason: "The substring fallback also matches an unrelated error that merely mentions the phrase. Revisit this expectation if the fallback is ever tightened.",
			err:    errors.New("upstream said: diff computation is not supported for this backend"),
			want:   true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, recovered := safeIsDiffComputationNotSupportedError(tc.err)
			if recovered != nil {
				t.Fatalf("\n%s\nIsDiffComputationNotSupportedError(...): unexpected panic: %v", tc.reason, recovered)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nIsDiffComputationNotSupportedError(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

// safeIsDiffComputationNotSupportedError calls
// IsDiffComputationNotSupportedError, recovering from a panic so that one
// misbehaving case reports a failure instead of aborting the whole test binary.
func safeIsDiffComputationNotSupportedError(err error) (result bool, recovered any) {
	defer func() { recovered = recover() }()
	return IsDiffComputationNotSupportedError(err), nil
}
