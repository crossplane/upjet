// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
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
	type args struct {
		initial       *tfsdk.InstanceState
		reconstructed *tfsdk.InstanceState
	}
	created := &tfsdk.InstanceState{ID: "created-id"}
	reconstructed := &tfsdk.InstanceState{ID: "reconstructed-id"}
	empty := &tfsdk.InstanceState{}
	cases := map[string]struct {
		args           args
		want           *tfsdk.InstanceState
		wantAfterReset *tfsdk.InstanceState
	}{
		"NilState": {
			args:           args{reconstructed: reconstructed},
			want:           reconstructed,
			wantAfterReset: nil,
		},
		"EmptyID": {
			args:           args{initial: empty, reconstructed: reconstructed},
			want:           reconstructed,
			wantAfterReset: nil,
		},
		"EmptyReconstructedID": {
			args:           args{reconstructed: empty},
			want:           empty,
			wantAfterReset: nil,
		},
		"PreserveValidState": {
			args:           args{initial: created, reconstructed: reconstructed},
			want:           created,
			wantAfterReset: created,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tracker := NewAsyncTracker()
			tracker.SetTfState(tc.args.initial)
			tracker.SetReconstructedTfState(tc.args.reconstructed)
			if got := tracker.GetTfState(); got != tc.want {
				t.Fatalf("unexpected state: got %v, want %v", got, tc.want)
			}
			tracker.ResetReconstructedTfState()
			if got := tracker.GetTfState(); got != tc.wantAfterReset {
				t.Fatalf("unexpected state after reset: got %v, want %v", got, tc.wantAfterReset)
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

func TestSetReconstructedFrameworkTFStatePreservesAsyncCreateState(t *testing.T) {
	tracker := NewAsyncTracker()
	reconstructing := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if tracker.HasFrameworkTFState() {
			t.Error("expected no state before reconstruction")
		}
		close(reconstructing)
		<-resume
		tracker.SetReconstructedFrameworkTFState(&tfprotov6.DynamicValue{JSON: []byte("null")})
	}()

	<-reconstructing
	created := newTestIdentityData("created-id").IdentityData
	identity := newTestIdentityData("created-id")
	tracker.SetFrameworkTFState(created)
	tracker.SetFrameworkIdentity(identity)
	close(resume)
	<-done

	if got := tracker.GetFrameworkTFState(); got != created {
		t.Fatalf("stale reconstruction replaced async create state: got %v, want %v", got, created)
	}
	tracker.ResetReconstructedFrameworkTFState()
	if got := tracker.GetFrameworkTFState(); got != created {
		t.Fatalf("reset cleared async create state: got %v, want %v", got, created)
	}
	if got := tracker.GetFrameworkIdentity(); got != identity {
		t.Fatalf("reset cleared async create identity: got %v, want %v", got, identity)
	}
}

func TestSetReconstructedFrameworkTFState(t *testing.T) {
	type args struct {
		initial              *tfprotov6.DynamicValue
		reconstructed        *tfprotov6.DynamicValue
		initialReconstructed bool
	}
	created := newTestIdentityData("created-id").IdentityData
	reconstructed := newTestIdentityData("reconstructed-id").IdentityData
	identity := newTestIdentityData("created-id")
	nullState, err := tfprotov6.NewDynamicValue(tftypes.String, tftypes.NewValue(tftypes.String, nil))
	if err != nil {
		t.Fatal(err)
	}
	jsonState := &tfprotov6.DynamicValue{JSON: []byte(`{"id":"created-id"}`)}
	invalidState := &tfprotov6.DynamicValue{}
	cases := map[string]struct {
		args                   args
		want                   *tfprotov6.DynamicValue
		wantAfterReset         *tfprotov6.DynamicValue
		wantIdentityAfterReset *tfprotov6.ResourceIdentityData
	}{
		"NilState": {
			args: args{reconstructed: reconstructed},
			want: reconstructed,
		},
		"NullJSONState": {
			args: args{initial: &tfprotov6.DynamicValue{JSON: []byte("null")}, reconstructed: reconstructed},
			want: reconstructed,
		},
		"NullMsgPackState": {
			args: args{initial: &nullState, reconstructed: reconstructed},
			want: reconstructed,
		},
		"PreserveMsgPackState": {
			args:                   args{initial: created, reconstructed: reconstructed},
			want:                   created,
			wantAfterReset:         created,
			wantIdentityAfterReset: identity,
		},
		"PreserveJSONState": {
			args:                   args{initial: jsonState, reconstructed: reconstructed},
			want:                   jsonState,
			wantAfterReset:         jsonState,
			wantIdentityAfterReset: identity,
		},
		"PreserveUnreadableState": {
			args:                   args{initial: invalidState, reconstructed: reconstructed},
			want:                   invalidState,
			wantAfterReset:         invalidState,
			wantIdentityAfterReset: identity,
		},
		"PreserveReconstructedMark": {
			args: args{initial: created, reconstructed: reconstructed, initialReconstructed: true},
			want: created,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tracker := NewAsyncTracker()
			if tc.args.initialReconstructed {
				tracker.SetReconstructedFrameworkTFState(tc.args.initial)
			} else {
				tracker.SetFrameworkTFState(tc.args.initial)
			}
			tracker.SetFrameworkIdentity(identity)
			tracker.SetReconstructedFrameworkTFState(tc.args.reconstructed)
			if got := tracker.GetFrameworkTFState(); got != tc.want {
				t.Fatalf("unexpected state: got %v, want %v", got, tc.want)
			}
			tracker.ResetReconstructedFrameworkTFState()
			if got := tracker.GetFrameworkTFState(); got != tc.wantAfterReset {
				t.Fatalf("unexpected state after reset: got %v, want %v", got, tc.wantAfterReset)
			}
			if got := tracker.GetFrameworkIdentity(); got != tc.wantIdentityAfterReset {
				t.Fatalf("unexpected identity after reset: got %v, want %v", got, tc.wantIdentityAfterReset)
			}
		})
	}
}
