// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
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
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
	"github.com/crossplane/upjet/v2/pkg/terraform"
)

var (
	cfgAsync = &config.Resource{
		TerraformResource: &schema.Resource{
			Timeouts: &schema.ResourceTimeout{
				Create: &timeout,
				Read:   &timeout,
				Update: &timeout,
				Delete: &timeout,
			},
			Schema: map[string]*schema.Schema{
				"name": {
					Type:     schema.TypeString,
					Required: true,
				},
				"id": {
					Type:     schema.TypeString,
					Computed: true,
					Required: false,
				},
				"map": {
					Type: schema.TypeMap,
					Elem: &schema.Schema{
						Type: schema.TypeString,
					},
				},
				"list": {
					Type: schema.TypeList,
					Elem: &schema.Schema{
						Type: schema.TypeString,
					},
				},
			},
		},
		ExternalName: config.IdentifierFromProvider,
		Sensitive: config.Sensitive{AdditionalConnectionDetailsFn: func(attr map[string]any) (map[string][]byte, error) {
			return nil, nil
		}},
	}
)

func prepareTerraformPluginSDKAsyncExternal(r Resource, cfg *config.Resource, fns CallbackFns) *terraformPluginSDKAsyncExternal {
	schemaBlock := cfg.TerraformResource.CoreConfigSchema()
	rawConfig, err := schema.JSONMapToStateValue(map[string]any{"name": "example"}, schemaBlock)
	if err != nil {
		panic(err)
	}
	return &terraformPluginSDKAsyncExternal{
		terraformPluginSDKExternal: &terraformPluginSDKExternal{
			ts:             terraform.Setup{},
			resourceSchema: r,
			config:         cfg,
			params: map[string]any{
				"name": "example",
			},
			rawConfig: rawConfig,
			logger:    logTest,
			opTracker: NewAsyncTracker(),
		},
		callback: fns,
	}
}

func TestAsyncTerraformPluginSDKConnect(t *testing.T) {
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
					return terraform.Setup{}, nil
				},
				cfg: cfgAsync,
				obj: newObjAsync(),
				ots: ots,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewTerraformPluginSDKAsyncConnector(nil, tc.args.ots, tc.args.setupFn, tc.args.cfg, WithTerraformPluginSDKAsyncLogger(logTest))
			_, err := c.Connect(t.Context(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestAsyncTerraformPluginSDKObserve(t *testing.T) {
	type args struct {
		r   Resource
		cfg *config.Resource
		obj xpresource.Managed
	}
	type want struct {
		obs managed.ExternalObservation
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"NotExists": {
			args: args{
				r: mockResource{
					RefreshWithoutUpgradeFn: func(ctx context.Context, s *tf.InstanceState, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
						return nil, nil
					},
				},
				cfg: cfgAsync,
				obj: newObjAsync(),
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
			args: args{
				r: mockResource{
					RefreshWithoutUpgradeFn: func(ctx context.Context, s *tf.InstanceState, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
						return &tf.InstanceState{ID: "example-id", Attributes: map[string]string{"name": "example"}}, nil
					},
				},
				cfg: cfgAsync,
				obj: newObjAsync(),
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
			terraformPluginSDKAsyncExternal := prepareTerraformPluginSDKAsyncExternal(tc.args.r, tc.args.cfg, CallbackFns{})
			observation, err := terraformPluginSDKAsyncExternal.Observe(t.Context(), tc.args.obj)
			if diff := cmp.Diff(tc.want.obs, observation); diff != "" {
				t.Errorf("\n%s\nObserve(...): -want observation, +got observation:\n", diff)
			}
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestAsyncTerraformPluginSDKCreate(t *testing.T) {
	type args struct {
		r   Resource
		cfg *config.Resource
		obj xpresource.Managed
		fns CallbackFns
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
				r: mockResource{
					ApplyFn: func(ctx context.Context, s *tf.InstanceState, d *tf.InstanceDiff, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
						return &tf.InstanceState{ID: "example-id"}, nil
					},
				},
				cfg: cfgAsync,
				obj: newObjAsync(),
				fns: CallbackFns{
					CreateFn: func(nn types.NamespacedName) terraform.CallbackFn {
						return func(err error, ctx context.Context) error {
							return nil
						}
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginSDKAsyncExternal := prepareTerraformPluginSDKAsyncExternal(tc.args.r, tc.args.cfg, tc.args.fns)
			_, err := terraformPluginSDKAsyncExternal.Create(t.Context(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nterraformPluginSDKAsyncExternal.Create(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestAsyncTerraformPluginSDKUpdate(t *testing.T) {
	type args struct {
		r   Resource
		cfg *config.Resource
		obj xpresource.Managed
		fns CallbackFns
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
				r: mockResource{
					ApplyFn: func(ctx context.Context, s *tf.InstanceState, d *tf.InstanceDiff, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
						return &tf.InstanceState{ID: "example-id"}, nil
					},
				},
				cfg: cfgAsync,
				obj: newObjAsync(),
				fns: CallbackFns{
					UpdateFn: func(nn types.NamespacedName) terraform.CallbackFn {
						return func(err error, ctx context.Context) error {
							return nil
						}
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginSDKAsyncExternal := prepareTerraformPluginSDKAsyncExternal(tc.args.r, tc.args.cfg, tc.args.fns)
			_, err := terraformPluginSDKAsyncExternal.Update(t.Context(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nterraformPluginSDKAsyncExternal.Update(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestAsyncTerraformPluginSDKDelete(t *testing.T) {
	type args struct {
		r   Resource
		cfg *config.Resource
		obj xpresource.Managed
		fns CallbackFns
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
				r: mockResource{
					ApplyFn: func(ctx context.Context, s *tf.InstanceState, d *tf.InstanceDiff, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
						return &tf.InstanceState{ID: "example-id"}, nil
					},
				},
				cfg: cfgAsync,
				obj: newObjAsync(),
				fns: CallbackFns{
					DestroyFn: func(nn types.NamespacedName) terraform.CallbackFn {
						return func(err error, ctx context.Context) error {
							return nil
						}
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginSDKAsyncExternal := prepareTerraformPluginSDKAsyncExternal(tc.args.r, tc.args.cfg, tc.args.fns)
			_, err := terraformPluginSDKAsyncExternal.Delete(t.Context(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nterraformPluginSDKAsyncExternal.Delete(...): -want error, +got error:\n", diff)
			}
		})
	}
}

// TestAsyncTerraformPluginSDKCreateRace is a regression test for
// the data race on a managed resource's status, between upjet's async Create
// operation and the managed reconciler.
// Must be run with `go test -race`.
//
// Please also see: https://github.com/crossplane/upjet/issues/472.
func TestAsyncTerraformPluginSDKCreateRace(t *testing.T) {
	obj := newObjAsync()
	r := mockResource{
		ApplyFn: func(_ context.Context, _ *tf.InstanceState, _ *tf.InstanceDiff, _ interface{}) (*tf.InstanceState, diag.Diagnostics) {
			return &tf.InstanceState{ID: "example-id", Attributes: map[string]string{"name": "example"}}, nil
		},
	}

	extDone := make(chan struct{})
	ext := prepareTerraformPluginSDKAsyncExternal(r, cfgAsync, CallbackFns{
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
		t.Fatalf("terraformPluginSDKAsyncExternal.Create(...): unexpected error: %v", err)
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

// TestAsyncTerraformPluginSDKUpdateRace is a regression test for
// the data race on a managed resource's status, between upjet's async Update
// operation and the managed reconciler.
// Must be run with `go test -race`.
//
// Please also see: https://github.com/crossplane/upjet/issues/472.
func TestAsyncTerraformPluginSDKUpdateRace(t *testing.T) {
	obj := newObjAsync()
	r := mockResource{
		ApplyFn: func(_ context.Context, _ *tf.InstanceState, _ *tf.InstanceDiff, _ interface{}) (*tf.InstanceState, diag.Diagnostics) {
			return &tf.InstanceState{ID: "example-id", Attributes: map[string]string{"name": "example"}}, nil
		},
	}

	extDone := make(chan struct{})
	ext := prepareTerraformPluginSDKAsyncExternal(r, cfgAsync, CallbackFns{
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
		t.Fatalf("terraformPluginSDKAsyncExternal.Update(...): unexpected error: %v", err)
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

// TestAsyncTerraformPluginSDKDeleteRace is a guard test asserting that upjet's
// async Delete operation does not concurrently access a managed resource's
// status while the managed reconciler does. Current async client Delete
// implementation does not modify MR status or spec.
// Must be run with `go test -race`.
func TestAsyncTerraformPluginSDKDeleteRace(t *testing.T) {
	obj := newObjAsync()
	r := mockResource{
		ApplyFn: func(_ context.Context, _ *tf.InstanceState, _ *tf.InstanceDiff, _ interface{}) (*tf.InstanceState, diag.Diagnostics) {
			return &tf.InstanceState{ID: "example-id", Attributes: map[string]string{"name": "example"}}, nil
		},
	}

	extDone := make(chan struct{})
	ext := prepareTerraformPluginSDKAsyncExternal(r, cfgAsync, CallbackFns{
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
		t.Fatalf("terraformPluginSDKAsyncExternal.Delete(...): unexpected error: %v", err)
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

func newObjAsync() *fake.Terraformed {
	return &fake.Terraformed{
		Parameterizable: fake.Parameterizable{
			Parameters: map[string]any{
				"name": "example",
				"map": map[string]any{
					"key": "value",
				},
				"list": []any{"elem1", "elem2"},
			},
		},
		Observable: fake.Observable{
			Observation: map[string]any{},
		},
	}
}
