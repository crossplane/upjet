// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	tfsdk "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestSetReconstructedTfStatePreservesAsyncCreateState(t *testing.T) {
	tracker := NewAsyncTracker()
	reconstructing := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Connect checked the cache before async create published its state.
		if tracker.HasState() {
			t.Error("expected no state before reconstruction")
		}
		close(reconstructing)
		<-resume
		tracker.SetReconstructedTfState(&tfsdk.InstanceState{})
	}()

	<-reconstructing
	created := &tfsdk.InstanceState{ID: "created-id"}
	tracker.SetTfState(created)
	close(resume)
	<-done

	if got := tracker.GetTfState(); got != created {
		t.Fatalf("stale reconstruction replaced async create state: got %v, want %v", got, created)
	}
	tracker.ResetReconstructedTfState()
	if got := tracker.GetTfState(); got != created {
		t.Fatalf("reset cleared async create state: got %v, want %v", got, created)
	}
}

func TestSetReconstructedTfState(t *testing.T) {
	cases := map[string]struct {
		initial       *tfsdk.InstanceState
		reconstructed *tfsdk.InstanceState
		preserve      bool
	}{
		"NilState": {
			reconstructed: &tfsdk.InstanceState{ID: "reconstructed-id"},
		},
		"EmptyID": {
			initial:       &tfsdk.InstanceState{},
			reconstructed: &tfsdk.InstanceState{ID: "reconstructed-id"},
		},
		"EmptyReconstructedID": {
			reconstructed: &tfsdk.InstanceState{},
		},
		"PreserveValidState": {
			initial:       &tfsdk.InstanceState{ID: "created-id"},
			reconstructed: &tfsdk.InstanceState{ID: "stale-id"},
			preserve:      true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tracker := NewAsyncTracker()
			tracker.SetTfState(tc.initial)
			tracker.SetReconstructedTfState(tc.reconstructed)
			want := tc.reconstructed
			if tc.preserve {
				want = tc.initial
			}
			if got := tracker.GetTfState(); got != want {
				t.Fatalf("unexpected state: got %v, want %v", got, want)
			}
			tracker.ResetReconstructedTfState()
			want = nil
			if tc.preserve {
				want = tc.initial
			}
			if got := tracker.GetTfState(); got != want {
				t.Fatalf("unexpected state after reset: got %v, want %v", got, want)
			}
		})
	}
}

// newTestIdentityData is defined in external_tfpluginfw_test.go

func TestAsyncTrackerFrameworkIdentity(t *testing.T) {
	t.Run("GetFrameworkIdentityReturnsNilByDefault", func(t *testing.T) {
		tracker := NewAsyncTracker()
		if tracker.GetFrameworkIdentity() != nil {
			t.Error("expected nil identity for new tracker")
		}
	})

	t.Run("SetAndGetFrameworkIdentity", func(t *testing.T) {
		tracker := NewAsyncTracker()
		identity := newTestIdentityData("test-id")
		tracker.SetFrameworkIdentity(identity)

		got := tracker.GetFrameworkIdentity()
		if got != identity {
			t.Error("GetFrameworkIdentity did not return the identity that was set")
		}
	})

	t.Run("SetFrameworkIdentityToNil", func(t *testing.T) {
		tracker := NewAsyncTracker()
		identity := newTestIdentityData("test-id")
		tracker.SetFrameworkIdentity(identity)
		tracker.SetFrameworkIdentity(nil)

		if tracker.GetFrameworkIdentity() != nil {
			t.Error("expected nil identity after setting to nil")
		}
	})

	t.Run("SetFrameworkIdentityOverwrite", func(t *testing.T) {
		tracker := NewAsyncTracker()
		id1 := newTestIdentityData("id-1")
		id2 := newTestIdentityData("id-2")

		tracker.SetFrameworkIdentity(id1)
		tracker.SetFrameworkIdentity(id2)

		got := tracker.GetFrameworkIdentity()
		if got != id2 {
			t.Error("GetFrameworkIdentity should return the most recently set identity")
		}
	})
}

func TestResetReconstructedFrameworkTFState(t *testing.T) {
	t.Run("ClearsReconstructedStateAndIdentity", func(t *testing.T) {
		tracker := NewAsyncTracker()
		// Simulate a reconstructed state (e.g. rehydrated after a restart)
		// together with an identity that was set on the same reconcile.
		tracker.SetReconstructedFrameworkTFState(&tfprotov6.DynamicValue{})
		tracker.SetFrameworkIdentity(newTestIdentityData("reconstructed-id"))

		tracker.ResetReconstructedFrameworkTFState()

		if tracker.HasFrameworkTFState() {
			t.Error("expected reconstructed framework state to be cleared")
		}
		if tracker.GetFrameworkIdentity() != nil {
			t.Error("expected framework identity to be cleared alongside reconstructed state")
		}
	})

	t.Run("NoOpForNonReconstructedState", func(t *testing.T) {
		tracker := NewAsyncTracker()
		// A non-reconstructed (normal) state and identity must be preserved.
		tracker.SetFrameworkTFState(&tfprotov6.DynamicValue{})
		identity := newTestIdentityData("live-id")
		tracker.SetFrameworkIdentity(identity)

		tracker.ResetReconstructedFrameworkTFState()

		if !tracker.HasFrameworkTFState() {
			t.Error("expected non-reconstructed framework state to be preserved")
		}
		if tracker.GetFrameworkIdentity() != identity {
			t.Error("expected non-reconstructed framework identity to be preserved")
		}
	})
}
