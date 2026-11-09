// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"go/types"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
	"github.com/crossplane/upjet/v2/pkg/terraform"
)

var (
	zl      = zap.New(zap.UseDevMode(true))
	logTest = logging.NewLogrLogger(zl.WithName("provider-aws"))
	ots     = NewOperationStore(logTest)
	timeout = time.Duration(1200000000000)
	cfg     = &config.Resource{
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
	obj = fake.Terraformed{
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

func prepareTerraformPluginSDKExternal(r Resource, cfg *config.Resource) *terraformPluginSDKExternal {
	schemaBlock := cfg.TerraformResource.CoreConfigSchema()
	rawConfig, err := schema.JSONMapToStateValue(map[string]any{"name": "example"}, schemaBlock)
	if err != nil {
		panic(err)
	}
	return &terraformPluginSDKExternal{
		ts:             terraform.Setup{},
		resourceSchema: r,
		config:         cfg,
		params: map[string]any{
			"name": "example",
		},
		rawConfig: rawConfig,
		logger:    logTest,
		opTracker: NewAsyncTracker(),
	}
}

type mockResource struct {
	ApplyFn                 func(ctx context.Context, s *tf.InstanceState, d *tf.InstanceDiff, meta interface{}) (*tf.InstanceState, diag.Diagnostics)
	RefreshWithoutUpgradeFn func(ctx context.Context, s *tf.InstanceState, meta interface{}) (*tf.InstanceState, diag.Diagnostics)
}

func (m mockResource) Apply(ctx context.Context, s *tf.InstanceState, d *tf.InstanceDiff, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
	return m.ApplyFn(ctx, s, d, meta)
}

func (m mockResource) RefreshWithoutUpgrade(ctx context.Context, s *tf.InstanceState, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
	return m.RefreshWithoutUpgradeFn(ctx, s, meta)
}

func TestTerraformPluginSDKConnect(t *testing.T) {
	type args struct {
		setupFn terraform.SetupFn
		cfg     *config.Resource
		ots     *OperationTrackerStore
		obj     fake.Terraformed
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
				cfg: cfg,
				obj: obj,
				ots: ots,
			},
		},
		"HCL": {
			args: args{
				setupFn: func(_ context.Context, _ client.Client, _ xpresource.Managed) (terraform.Setup, error) {
					return terraform.Setup{}, nil
				},
				cfg: cfg,
				obj: fake.Terraformed{
					Parameterizable: fake.Parameterizable{
						Parameters: map[string]any{
							"name": "      ${jsonencode({\n          type = \"object\"\n        })}",
							"map": map[string]any{
								"key": "value",
							},
							"list": []any{"elem1", "elem2"},
						},
					},
					Observable: fake.Observable{
						Observation: map[string]any{},
					},
				},
				ots: ots,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewTerraformPluginSDKConnector(nil, tc.args.setupFn, tc.args.cfg, tc.args.ots, WithTerraformPluginSDKLogger(logTest))
			_, err := c.Connect(t.Context(), &tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestTerraformPluginSDKObserve(t *testing.T) {
	type args struct {
		r   Resource
		cfg *config.Resource
		obj fake.Terraformed
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
				cfg: cfg,
				obj: obj,
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
				cfg: cfg,
				obj: obj,
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
		"InitProvider": {
			args: args{
				r: mockResource{
					RefreshWithoutUpgradeFn: func(ctx context.Context, s *tf.InstanceState, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
						return &tf.InstanceState{ID: "example-id", Attributes: map[string]string{"name": "example2"}}, nil
					},
				},
				cfg: cfg,
				obj: fake.Terraformed{
					Parameterizable: fake.Parameterizable{
						Parameters: map[string]any{
							"name": "example",
							"map": map[string]any{
								"key": "value",
							},
							"list": []any{"elem1", "elem2"},
						},
						InitParameters: map[string]any{
							"list": []any{"elem1", "elem2", "elem3"},
						},
					},
					Observable: fake.Observable{
						Observation: map[string]any{},
					},
				},
			},
			want: want{
				obs: managed.ExternalObservation{
					ResourceExists:          true,
					ResourceUpToDate:        false,
					ResourceLateInitialized: true,
					ConnectionDetails:       nil,
					Diff:                    "",
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginSDKExternal := prepareTerraformPluginSDKExternal(tc.args.r, tc.args.cfg)
			observation, err := terraformPluginSDKExternal.Observe(t.Context(), &tc.args.obj)
			if diff := cmp.Diff(tc.want.obs, observation); diff != "" {
				t.Errorf("\n%s\nObserve(...): -want observation, +got observation:\n", diff)
			}
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

// TestTerraformPluginSDKObserveNotFound is a regression test
// for the "value is not an object" panic (e.g., in provider-aws).
// When Observe calls schema.Diff with an InstanceState whose RawPlan is
// the zero cty.Value, the AWS provider's setTagsAll() interceptor calls
// diff.GetRawPlan().GetAttr("tags"), and go-cty's Value.GetAttr panics with
// "value is not an object" on the nil cty.Value.
// The same applies to RawConfig and RawState.
func TestTerraformPluginSDKObserveNotFound(t *testing.T) {
	tagsTimeout := timeout
	cases := map[string]struct {
		customizeDiff schema.CustomizeDiffFunc
		description   string
	}{
		"RawConfig": {
			customizeDiff: func(_ context.Context, d *schema.ResourceDiff, _ interface{}) error {
				// Calling d.GetRawConfig().GetAttr on a zero RawConfig will panic.
				_ = d.GetRawConfig().GetAttr("name")
				return nil
			},
			description: "Observed a panic when reading from InstanceState.RawConfig",
		},
		"RawPlan": {
			customizeDiff: func(_ context.Context, d *schema.ResourceDiff, _ interface{}) error {
				// Calling d.GetRawPlan().GetAttr on a zero RawPlan will panic.
				_ = d.GetRawPlan().GetAttr("tags")
				return nil
			},
			description: "Observed a panic when reading from InstanceState.RawPlan",
		},
		"RawState": {
			customizeDiff: func(_ context.Context, d *schema.ResourceDiff, _ interface{}) error {
				// Terraform core hands providers a typed null prior state while
				// the resource does not exist, and providers guard their
				// GetRawState().GetAttr calls with IsNull or Id() == "". The
				// zero cty.Value has no type at all, so the guards do not help
				// and GetAttr panics with "value is not an object".
				rs := d.GetRawState()
				if !rs.Type().IsObjectType() || !rs.IsNull() {
					return errors.Errorf("RawState is %#v, want a null value of the resource object type", rs)
				}
				return nil
			},
			description: "RawState of a resource that does not exist is not a typed null object",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfgWithTags := &config.Resource{
				TerraformResource: &schema.Resource{
					Timeouts: &schema.ResourceTimeout{
						Create: &tagsTimeout,
						Read:   &tagsTimeout,
						Update: &tagsTimeout,
						Delete: &tagsTimeout,
					},
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"tags": {
							Type:     schema.TypeMap,
							Optional: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
						"tags_all": {
							Type:     schema.TypeMap,
							Computed: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
					},
					CustomizeDiff: tc.customizeDiff,
				},
				ExternalName: config.IdentifierFromProvider,
				Sensitive: config.Sensitive{AdditionalConnectionDetailsFn: func(_ map[string]any) (map[string][]byte, error) {
					return nil, nil
				}},
			}

			notFound := mockResource{
				RefreshWithoutUpgradeFn: func(_ context.Context, _ *tf.InstanceState, _ interface{}) (*tf.InstanceState, diag.Diagnostics) {
					// Force into the state where the op tracker cache exists
					// but resource is not found.
					return nil, nil
				},
			}

			ext := prepareTerraformPluginSDKExternal(notFound, cfgWithTags)
			cachedState := &tf.InstanceState{
				ID:         "example-id",
				Attributes: map[string]string{"name": "example"},
			}
			// A previous Observe of the then existing resource leaves its
			// raw state in the op tracker cache.
			rawState, err := cachedState.AttrsAsObjectValue(cfgWithTags.TerraformResource.CoreConfigSchema().ImpliedType())
			if err != nil {
				t.Fatalf("AttrsAsObjectValue(...) returned an unexpected error: %v", err)
			}
			cachedState.RawState = rawState
			ext.opTracker.SetTfState(cachedState)

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: %v", tc.description, r)
				}
			}()

			if _, err := ext.Observe(t.Context(), &obj); err != nil {
				t.Fatalf("Observe(...) returned an unexpected error: %v", err)
			}
		})
	}
}

// TestTerraformPluginSDKObserveExistingRawValues is a regression test for
// the "value is not an object" panic on an existing resource (e.g.,
// google_compute_subnetwork in provider-upjet-gcp, crossplane-contrib/provider-upjet-gcp#1002).
// Its CustomizeDiff calls diff.GetRawState().GetAttr("secondary_ip_range")
// on every diff computed after the resource has been created, so Observe
// must populate RawState, RawPlan and RawConfig from the refreshed state
// before computing the diff. The test also checks that the raw state carries
// the refreshed attribute values, not just an object of the right type.
func TestTerraformPluginSDKObserveExistingRawValues(t *testing.T) {
	type args struct {
		rawValue func(d *schema.ResourceDiff) cty.Value
	}
	type want struct {
		name string
	}
	cases := map[string]struct {
		args
		want
	}{
		"RawState": {
			args: args{rawValue: func(d *schema.ResourceDiff) cty.Value { return d.GetRawState() }},
			want: want{name: "example-refreshed"},
		},
		"RawPlan": {
			args: args{rawValue: func(d *schema.ResourceDiff) cty.Value { return d.GetRawPlan() }},
			want: want{name: "example-refreshed"},
		},
		"RawConfig": {
			args: args{rawValue: func(d *schema.ResourceDiff) cty.Value { return d.GetRawConfig() }},
			want: want{name: "example"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var observedName cty.Value
			cfgWithCustomizeDiff := &config.Resource{
				TerraformResource: &schema.Resource{
					Timeouts: cfg.TerraformResource.Timeouts,
					Schema:   cfg.TerraformResource.Schema,
					CustomizeDiff: func(_ context.Context, d *schema.ResourceDiff, _ interface{}) error {
						// Panics with "value is not an object" when the raw
						// value is the zero cty.Value.
						observedName = tc.args.rawValue(d).GetAttr("name")
						return nil
					},
				},
				ExternalName: config.IdentifierFromProvider,
				Sensitive: config.Sensitive{AdditionalConnectionDetailsFn: func(_ map[string]any) (map[string][]byte, error) {
					return nil, nil
				}},
			}
			existing := mockResource{
				RefreshWithoutUpgradeFn: func(_ context.Context, _ *tf.InstanceState, _ interface{}) (*tf.InstanceState, diag.Diagnostics) {
					return &tf.InstanceState{ID: "example-id", Attributes: map[string]string{"name": "example-refreshed"}}, nil
				},
			}
			ext := prepareTerraformPluginSDKExternal(existing, cfgWithCustomizeDiff)

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Observed a panic when reading the raw values of an existing resource: %v", r)
				}
			}()

			observation, err := ext.Observe(t.Context(), &obj)
			if err != nil {
				t.Fatalf("Observe(...) returned an unexpected error: %v", err)
			}
			if !observation.ResourceExists {
				t.Errorf("Observe(...): expected ResourceExists to be true")
			}
			if diff := cmp.Diff(cty.StringVal(tc.want.name), observedName, cmp.Comparer(func(a, b cty.Value) bool { return a.RawEquals(b) })); diff != "" {
				t.Errorf("\n%s\nObserve(...): -want raw name, +got raw name:\n", diff)
			}
		})
	}
}

func TestTerraformPluginSDKCreate(t *testing.T) {
	type args struct {
		r   Resource
		cfg *config.Resource
		obj fake.Terraformed
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"Unsuccessful": {
			args: args{
				r: mockResource{
					ApplyFn: func(ctx context.Context, s *tf.InstanceState, d *tf.InstanceDiff, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
						return nil, nil
					},
				},
				cfg: cfg,
				obj: obj,
			},
			want: want{
				err: errors.New("failed to read the ID of the new resource"),
			},
		},
		"Successful": {
			args: args{
				r: mockResource{
					ApplyFn: func(ctx context.Context, s *tf.InstanceState, d *tf.InstanceDiff, meta interface{}) (*tf.InstanceState, diag.Diagnostics) {
						return &tf.InstanceState{ID: "example-id"}, nil
					},
				},
				cfg: cfg,
				obj: obj,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginSDKExternal := prepareTerraformPluginSDKExternal(tc.args.r, tc.args.cfg)
			_, err := terraformPluginSDKExternal.Create(t.Context(), &tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestTerraformPluginSDKUpdate(t *testing.T) {
	type args struct {
		r   Resource
		cfg *config.Resource
		obj fake.Terraformed
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
				cfg: cfg,
				obj: obj,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginSDKExternal := prepareTerraformPluginSDKExternal(tc.args.r, tc.args.cfg)
			_, err := terraformPluginSDKExternal.Update(t.Context(), &tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestTerraformPluginSDKDelete(t *testing.T) {
	type args struct {
		r   Resource
		cfg *config.Resource
		obj fake.Terraformed
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
				cfg: cfg,
				obj: obj,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			terraformPluginSDKExternal := prepareTerraformPluginSDKExternal(tc.args.r, tc.args.cfg)
			_, err := terraformPluginSDKExternal.Delete(t.Context(), &tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestTerraformPluginSDKFromInstanceStateToJSONMap(t *testing.T) {
	// 2^63 - 1 and a typical Compute resource ID: both are above 2^53, so a
	// float64 cannot represent them exactly.
	const maxInt64 = "9223372036854775807"
	const resourceID = "8071108885042506151"
	sch := &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name":         {Type: schema.TypeString, Required: true},
			"generated_id": {Type: schema.TypeInt, Computed: true},
			"ratio":        {Type: schema.TypeFloat, Computed: true},
			"count":        {Type: schema.TypeInt, Computed: true},
			"block": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"numeric_id": {Type: schema.TypeInt, Computed: true},
					},
				},
			},
		},
	}
	stringType := types.NewPointer(types.Universe.Lookup("string").Type())
	attrs := map[string]string{
		"id":                 "example",
		"name":               "example",
		"generated_id":       resourceID,
		"ratio":              "0.1",
		"count":              resourceID,
		"block.#":            "2",
		"block.0.numeric_id": maxInt64,
		"block.1.numeric_id": resourceID,
	}

	type want struct {
		m   map[string]any
		err error
	}
	cases := map[string]struct {
		reason    string
		overrides []string
		want      want
	}{
		"NoOverrides": {
			reason: "Without a string override, numbers keep the float64 representation of StateValueToJSONMap.",
			want: want{
				m: map[string]any{
					"id":           "example",
					"name":         "example",
					"generated_id": float64(8071108885042506151),
					"ratio":        0.1,
					"count":        float64(8071108885042506151),
					"block": []any{
						map[string]any{"numeric_id": float64(9223372036854775807)},
						map[string]any{"numeric_id": float64(8071108885042506151)},
					},
				},
			},
		},
		"StringOverrides": {
			reason:    "Numbers whose generated type is overridden to string are rendered exactly from the Terraform state, at the top level and in nested blocks.",
			overrides: []string{"generated_id", "ratio", "block.numeric_id"},
			want: want{
				m: map[string]any{
					"id":           "example",
					"name":         "example",
					"generated_id": resourceID,
					"ratio":        "0.1",
					"count":        float64(8071108885042506151),
					"block": []any{
						map[string]any{"numeric_id": maxInt64},
						map[string]any{"numeric_id": resourceID},
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := config.DefaultResource("test_resource", sch, nil, nil)
			for _, p := range tc.overrides {
				if err := cfg.OverrideScalarFieldType(p, stringType); err != nil {
					t.Fatalf("OverrideScalarFieldType(%q): %v", p, err)
				}
			}
			n := &terraformPluginSDKExternal{config: cfg, logger: logTest}
			got, _, err := n.fromInstanceStateToJSONMap(&tf.InstanceState{ID: "example", Attributes: attrs})
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nfromInstanceStateToJSONMap(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.m, got); diff != "" {
				t.Errorf("\n%s\nfromInstanceStateToJSONMap(...): -want, +got:\n%s", tc.reason, diff)
			}
			if tc.overrides == nil {
				return
			}
			// The status is turned back into Terraform state when the
			// in-memory state is lost, e.g. after a provider restart: an exact
			// string must parse back into the exact number.
			back, err := schema.JSONMapToStateValue(got, sch.CoreConfigSchema())
			if err != nil {
				t.Fatalf("JSONMapToStateValue(...): %v", err)
			}
			if got, want := back.GetAttr("generated_id"), cty.MustParseNumberVal(resourceID); !got.RawEquals(want) {
				t.Errorf("\n%s\nJSONMapToStateValue(...): generated_id = %#v, want %#v", tc.reason, got, want)
			}
		})
	}
}
