// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/terraform"
)

func prepareTerraformPluginFrameworkAsyncExternal(testConfig testConfiguration, fns CallbackFns) *terraformPluginFrameworkAsyncExternalClient {
	return &terraformPluginFrameworkAsyncExternalClient{
		terraformPluginFrameworkExternalClient: prepareTPFExternalWithTestConfig(testConfig),
		callback:                               fns,
	}
}

func TestAsyncTerraformPluginFrameworkConnect(t *testing.T) {
	type args struct {
		setupFn terraform.SetupFn
		cfg     *config.Resource
		ots     *OperationTrackerStore
		obj     xpresource.Managed
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"Successful": {
			args: args{
				setupFn: func(_ context.Context, _ client.Client, _ xpresource.Managed) (terraform.Setup, error) {
					return terraform.Setup{
						FrameworkProvider: &mockTPFProvider{},
					}, nil
				},
				cfg: newBaseUpjetConfig(),
				obj: newObjAsync(),
				ots: ots,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewTerraformPluginFrameworkAsyncConnector(nil, tc.args.ots, tc.args.setupFn, tc.args.cfg, WithTerraformPluginFrameworkAsyncLogger(logTest))
			_, err := c.Connect(t.Context(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestAsyncTerraformPluginFrameworkObserve(t *testing.T) {
	type want struct {
		obs managed.ExternalObservation
		err error
	}
	cases := map[string]struct {
		testConfiguration
		want
	}{
		"NotExists": {
			testConfiguration: testConfiguration{
				r:               newMockBaseTPFResource(),
				cfg:             newBaseUpjetConfig(),
				obj:             newBaseObject(),
				currentStateMap: nil,
				plannedStateMap: map[string]any{
					"name": "example",
				},
				params: map[string]any{
					"name": "example",
				},
			},
			want: want{
				obs: managed.ExternalObservation{
					ResourceExists:          false,
					ResourceUpToDate:        false,
					ResourceLateInitialized: false,
					ConnectionDetails:       nil,
					Diff:                    "",
				},
			},
		},
		"UpToDate": {
			testConfiguration: testConfiguration{
				r:   newMockBaseTPFResource(),
				cfg: newBaseUpjetConfig(),
				obj: newBaseObject(),
				params: map[string]any{
					"id":   "example-id",
					"name": "example",
				},
				currentStateMap: map[string]any{
					"id":   "example-id",
					"name": "example",
				},
				plannedStateMap: map[string]any{
					"id":   "example-id",
					"name": "example",
				},
			},
			want: want{
				obs: managed.ExternalObservation{
					ResourceExists:          true,
					ResourceUpToDate:        true,
					ResourceLateInitialized: true,
					ConnectionDetails:       nil,
					Diff:                    "",
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginFrameworkAsyncExternal := prepareTerraformPluginFrameworkAsyncExternal(tc.testConfiguration, CallbackFns{})
			observation, err := terraformPluginFrameworkAsyncExternal.Observe(t.Context(), &tc.testConfiguration.obj)
			if diff := cmp.Diff(tc.want.obs, observation); diff != "" {
				t.Errorf("\n%s\nObserve(...): -want observation, +got observation:\n", diff)
			}
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestAsyncTerraformPluginFrameworkCreate(t *testing.T) {
	type want struct {
		err error
	}
	cases := map[string]struct {
		testConfiguration
		want
	}{
		"Successful": {
			testConfiguration: testConfiguration{
				r:               newMockBaseTPFResource(),
				cfg:             newBaseUpjetConfig(),
				obj:             newBaseObject(),
				currentStateMap: nil,
				plannedStateMap: map[string]any{
					"name": "example",
				},
				params: map[string]any{
					"name": "example",
				},
				newStateMap: map[string]any{
					"name": "example",
					"id":   "example-id",
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginFrameworkAsyncExternal := prepareTerraformPluginFrameworkAsyncExternal(tc.testConfiguration, CallbackFns{
				CreateFn: func(_ types.NamespacedName) terraform.CallbackFn {
					return func(err error, ctx context.Context) error {
						return nil
					}
				},
			})
			_, err := terraformPluginFrameworkAsyncExternal.Create(t.Context(), &tc.testConfiguration.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nterraformPluginFrameworkAsyncExternalClient.Create(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestAsyncTerraformPluginFrameworkUpdate(t *testing.T) {
	type want struct {
		err error
	}
	cases := map[string]struct {
		testConfiguration
		want
	}{
		"Successful": {
			testConfiguration: testConfiguration{
				r:   newMockBaseTPFResource(),
				cfg: newBaseUpjetConfig(),
				obj: newBaseObject(),
				currentStateMap: map[string]any{
					"name": "example",
					"id":   "example-id",
				},
				plannedStateMap: map[string]any{
					"name": "example-updated",
					"id":   "example-id",
				},
				params: map[string]any{
					"name": "example-updated",
				},
				newStateMap: map[string]any{
					"name": "example-updated",
					"id":   "example-id",
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginFrameworkAsyncExternal := prepareTerraformPluginFrameworkAsyncExternal(tc.testConfiguration, CallbackFns{
				UpdateFn: func(_ types.NamespacedName) terraform.CallbackFn {
					return func(err error, ctx context.Context) error {
						return nil
					}
				},
			})
			_, err := terraformPluginFrameworkAsyncExternal.Update(t.Context(), &tc.testConfiguration.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nterraformPluginFrameworkAsyncExternalClient.Update(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestAsyncTerraformPluginFrameworkDelete(t *testing.T) {
	type want struct {
		err error
	}
	cases := map[string]struct {
		testConfiguration
		want
	}{
		"Successful": {
			testConfiguration: testConfiguration{
				r:   newMockBaseTPFResource(),
				cfg: newBaseUpjetConfig(),
				obj: newBaseObject(),
				currentStateMap: map[string]any{
					"name": "example",
					"id":   "example-id",
				},
				plannedStateMap: nil,
				params: map[string]any{
					"name": "example",
				},
				newStateMap: nil,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginFrameworkAsyncExternal := prepareTerraformPluginFrameworkAsyncExternal(tc.testConfiguration, CallbackFns{
				DestroyFn: func(_ types.NamespacedName) terraform.CallbackFn {
					return func(err error, ctx context.Context) error {
						return nil
					}
				},
			})
			_, err := terraformPluginFrameworkAsyncExternal.Delete(t.Context(), &tc.testConfiguration.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nterraformPluginFrameworkAsyncExternalClient.Delete(...): -want error, +got error:\n", diff)
			}
		})
	}
}

// TestAsyncTerraformPluginFrameworkCreateRace is a regression test for
// the data race on a managed resource's status, between upjet's async Create
// operation and the managed reconciler.
// Must be run with `go test -race`.
//
// Please also see: https://github.com/crossplane/upjet/issues/472.
func TestAsyncTerraformPluginFrameworkCreateRace(t *testing.T) {
	obj := newObjAsync()
	tc := testConfiguration{
		r:               newMockBaseTPFResource(),
		cfg:             newBaseUpjetConfig(),
		currentStateMap: nil,
		plannedStateMap: map[string]any{
			"name": "example",
		},
		params: map[string]any{
			"name": "example",
		},
		newStateMap: map[string]any{
			"name": "example",
			"id":   "example-id",
		},
	}

	extDone := make(chan struct{})
	ext := prepareTerraformPluginFrameworkAsyncExternal(tc, CallbackFns{
		CreateFn: func(_ types.NamespacedName) terraform.CallbackFn {
			return func(_ error, _ context.Context) error {
				// Signal the async operation of the external client has completed.
				close(extDone)
				return nil
			}
		},
	})
	// This call starts the async worker that will race with
	// the managed reconciler below.
	if _, err := ext.Create(t.Context(), obj); err != nil {
		t.Fatalf("terraformPluginFrameworkAsyncExternalClient.Create(...): unexpected error: %v", err)
	}

	// Simulate the managed reconciler concurrently writing to the status of
	// the same MR (obj above).
	mrDone := make(chan struct{})
	go func() {
		_ = obj.DeepCopyObject()
		_ = obj.SetObservation(map[string]any{"name": "example"})
		// Signal the managed reconciler has completed.
		close(mrDone)
	}()
	<-extDone
	<-mrDone
}

// TestAsyncTerraformPluginFrameworkUpdateRace is a regression test for
// the data race on a managed resource's status, between upjet's async Update
// operation and the managed reconciler.
// Must be run with `go test -race`.
//
// Please also see: https://github.com/crossplane/upjet/issues/472.
func TestAsyncTerraformPluginFrameworkUpdateRace(t *testing.T) {
	obj := newObjAsync()
	tc := testConfiguration{
		r:   newMockBaseTPFResource(),
		cfg: newBaseUpjetConfig(),
		currentStateMap: map[string]any{
			"name": "example",
			"id":   "example-id",
		},
		plannedStateMap: map[string]any{
			"name": "example-updated",
			"id":   "example-id",
		},
		params: map[string]any{
			"name": "example-updated",
		},
		newStateMap: map[string]any{
			"name": "example-updated",
			"id":   "example-id",
		},
	}

	extDone := make(chan struct{})
	ext := prepareTerraformPluginFrameworkAsyncExternal(tc, CallbackFns{
		UpdateFn: func(_ types.NamespacedName) terraform.CallbackFn {
			return func(_ error, _ context.Context) error {
				// Signal the async operation of the external client has completed.
				close(extDone)
				return nil
			}
		},
	})
	// This call starts the async worker that will race with
	// the managed reconciler below.
	if _, err := ext.Update(t.Context(), obj); err != nil {
		t.Fatalf("terraformPluginFrameworkAsyncExternalClient.Update(...): unexpected error: %v", err)
	}

	// Simulate the managed reconciler concurrently writing to the status of
	// the same MR (obj above).
	mrDone := make(chan struct{})
	go func() {
		_ = obj.DeepCopyObject()
		_ = obj.SetObservation(map[string]any{"name": "example"})
		// Signal the managed reconciler has completed.
		close(mrDone)
	}()
	<-extDone
	<-mrDone
}

// TestAsyncTerraformPluginFrameworkDeleteRace is a guard test asserting that
// upjet's async Delete operation does not concurrently access a managed
// resource's status while the managed reconciler does.
// Current async client Delete does not modify MR status or spec.
// Must be run with `go test -race`.
func TestAsyncTerraformPluginFrameworkDeleteRace(t *testing.T) {
	obj := newObjAsync()
	tc := testConfiguration{
		r:   newMockBaseTPFResource(),
		cfg: newBaseUpjetConfig(),
		currentStateMap: map[string]any{
			"name": "example",
			"id":   "example-id",
		},
		plannedStateMap: nil,
		params: map[string]any{
			"name": "example",
		},
		newStateMap: nil,
	}

	extDone := make(chan struct{})
	ext := prepareTerraformPluginFrameworkAsyncExternal(tc, CallbackFns{
		DestroyFn: func(_ types.NamespacedName) terraform.CallbackFn {
			return func(_ error, _ context.Context) error {
				// Signal the async operation of the external client has completed.
				close(extDone)
				return nil
			}
		},
	})
	// This call starts the async worker that will race with
	// the managed reconciler below.
	if _, err := ext.Delete(t.Context(), obj); err != nil {
		t.Fatalf("terraformPluginFrameworkAsyncExternalClient.Delete(...): unexpected error: %v", err)
	}

	// Simulate the managed reconciler concurrently writing to the status of
	// the same MR (obj above).
	mrDone := make(chan struct{})
	go func() {
		_ = obj.DeepCopyObject()
		// Managed reconciler does not call SetObservation during deletion.
		// This is an extra check at the moment.
		_ = obj.SetObservation(map[string]any{"name": "example"})
		// Signal the managed reconciler has completed.
		close(mrDone)
	}()
	<-extDone
	<-mrDone
}
