// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"sort"
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

func TestFilterInitExclusiveDiffs(t *testing.T) {
	// The schema and spec follow azurerm_postgresql_flexible_server: Azure
	// picks both availability zones and swaps them on a failover, so they
	// are set only in initProvider. high_availability is a singleton list in
	// Terraform that the reconciled CRD version serves as an embedded object.
	newCfg := func() *config.Resource {
		r := config.DefaultResource("test_flexible_server", &schema.Resource{
			Schema: map[string]*schema.Schema{
				"name": {
					Type:     schema.TypeString,
					Required: true,
				},
				"zone": {
					Type:     schema.TypeString,
					Optional: true,
				},
				"high_availability": {
					Type:     schema.TypeList,
					Optional: true,
					MaxItems: 1,
					Elem: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"mode": {
								Type:     schema.TypeString,
								Required: true,
							},
							"standby_availability_zone": {
								Type:     schema.TypeString,
								Optional: true,
							},
						},
					},
				},
			},
		}, nil, nil)
		r.AddSingletonListConversion("high_availability", "highAvailability")
		r.TerraformConversions = []config.TerraformConversion{config.NewTFSingletonConversion()}
		return r
	}
	// The zones after a failover: Azure swapped them.
	failedOver := &tf.InstanceState{
		ID: "example",
		Attributes: map[string]string{
			"id":                       "example",
			"name":                     "example",
			"zone":                     "1",
			"high_availability.#":      "1",
			"high_availability.0.mode": "ZoneRedundant",
			"high_availability.0.standby_availability_zone": "2",
		},
	}
	type args struct {
		forProvider  map[string]any
		initProvider map[string]any
		// tfConfig is what the provider applies: initProvider merged into
		// forProvider, converted to the Terraform schema.
		tfConfig map[string]any
	}
	cases := map[string]struct {
		reason string
		args
		want []string
	}{
		"InitProviderOnlyFieldsOfEmbeddedObject": {
			reason: "Fields set only in initProvider should not diff, including a field of a singleton list served as an embedded object.",
			args: args{
				forProvider: map[string]any{
					"name":              "example",
					"high_availability": map[string]any{"mode": "ZoneRedundant"},
				},
				initProvider: map[string]any{
					"zone":              "2",
					"high_availability": map[string]any{"standby_availability_zone": "1"},
				},
				tfConfig: map[string]any{
					"name": "example",
					"zone": "2",
					"high_availability": []any{map[string]any{
						"mode":                      "ZoneRedundant",
						"standby_availability_zone": "1",
					}},
				},
			},
			want: []string{},
		},
		"ForProviderFieldOfEmbeddedObject": {
			reason: "A forProvider field of the embedded object should still diff when its initProvider-only sibling is filtered.",
			args: args{
				forProvider: map[string]any{
					"name":              "example",
					"high_availability": map[string]any{"mode": "SameZone"},
				},
				initProvider: map[string]any{
					"zone":              "2",
					"high_availability": map[string]any{"standby_availability_zone": "1"},
				},
				tfConfig: map[string]any{
					"name": "example",
					"zone": "2",
					"high_availability": []any{map[string]any{
						"mode":                      "SameZone",
						"standby_availability_zone": "1",
					}},
				},
			},
			want: []string{"high_availability.0.mode"},
		},
		"FieldInForProviderAndInitProvider": {
			reason: "A field set in forProvider should diff even when initProvider also sets it.",
			args: args{
				forProvider: map[string]any{
					"name":              "example",
					"zone":              "2",
					"high_availability": map[string]any{"mode": "ZoneRedundant"},
				},
				initProvider: map[string]any{
					"zone":              "2",
					"high_availability": map[string]any{"standby_availability_zone": "1"},
				},
				tfConfig: map[string]any{
					"name": "example",
					"zone": "2",
					"high_availability": []any{map[string]any{
						"mode":                      "ZoneRedundant",
						"standby_availability_zone": "1",
					}},
				},
			},
			want: []string{"zone"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := newCfg()
			tr := &fake.Terraformed{
				Parameterizable: fake.Parameterizable{
					Parameters:     tc.args.forProvider,
					InitParameters: tc.args.initProvider,
				},
			}
			diff, err := schema.InternalMap(cfg.TerraformResource.Schema).Diff(context.TODO(), failedOver, tf.NewResourceConfigRaw(tc.args.tfConfig), nil, nil, false)
			if err != nil {
				t.Fatalf("cannot compute the instance diff: %v", err)
			}
			if err := filterInitExclusiveDiffs(tr, diff, cfg); err != nil {
				t.Fatalf("\n%s\nfilterInitExclusiveDiffs(...): unexpected error: %v", tc.reason, err)
			}
			got := make([]string, 0, len(diff.Attributes))
			for k := range diff.Attributes {
				got = append(got, k)
			}
			sort.Strings(got)
			if d := cmp.Diff(tc.want, got); d != "" {
				t.Errorf("\n%s\nfilterInitExclusiveDiffs(...): -want remaining diff keys, +got:\n%s", tc.reason, d)
			}
		})
	}
}
