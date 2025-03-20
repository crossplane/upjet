// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/resource/fake"
	"github.com/crossplane/upjet/pkg/terraform"
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
	objAsync = &fake.Terraformed{
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
				obj: objAsync,
				ots: ots,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewTerraformPluginSDKAsyncConnector(nil, tc.args.ots, tc.args.setupFn, tc.args.cfg, WithTerraformPluginSDKAsyncLogger(logTest))
			_, err := c.Connect(context.TODO(), tc.args.obj)
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
				obj: objAsync,
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
				obj: objAsync,
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
			observation, err := terraformPluginSDKAsyncExternal.Observe(context.TODO(), tc.args.obj)
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
				obj: objAsync,
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
			_, err := terraformPluginSDKAsyncExternal.Create(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
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
				obj: objAsync,
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
			_, err := terraformPluginSDKAsyncExternal.Update(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
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
				obj: objAsync,
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
			_, err := terraformPluginSDKAsyncExternal.Delete(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}
