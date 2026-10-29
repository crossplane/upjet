// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/pkg/errors"
)

type fakeFinalizer struct {
	err error
}

func (f *fakeFinalizer) AddFinalizer(_ context.Context, _ xpresource.Object) error {
	return nil
}

func (f *fakeFinalizer) RemoveFinalizer(_ context.Context, _ xpresource.Object) error {
	return f.err
}

type fakeTrackerCleaner struct {
	removed int
	err     error
}

func (f *fakeTrackerCleaner) RemoveTracker(_ xpresource.Object) error {
	f.removed++
	return f.err
}

func TestOperationTrackerFinalizerRemoveFinalizer(t *testing.T) {
	errBoom := errors.New("boom")
	cases := map[string]struct {
		finalizerErr  error
		trackerErr    error
		wantRemoved   int
		wantErrIsBoom bool
	}{
		"Success": {
			wantRemoved: 1,
		},
		"FinalizerErrorRetainsTracker": {
			finalizerErr:  errBoom,
			wantRemoved:   0,
			wantErrIsBoom: true,
		},
		"TrackerError": {
			trackerErr:    errBoom,
			wantRemoved:   1,
			wantErrIsBoom: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cleaner := &fakeTrackerCleaner{err: tc.trackerErr}
			f := NewOperationTrackerFinalizer(cleaner, &fakeFinalizer{err: tc.finalizerErr})
			err := f.RemoveFinalizer(context.Background(), &xpfake.Managed{})
			if (errors.Is(err, errBoom)) != tc.wantErrIsBoom {
				t.Errorf("unexpected error: %v", err)
			}
			if cleaner.removed != tc.wantRemoved {
				t.Errorf("RemoveTracker calls: want %d, got %d", tc.wantRemoved, cleaner.removed)
			}
		})
	}
}
