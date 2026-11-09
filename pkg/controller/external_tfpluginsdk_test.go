// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
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

// TestTerraformPluginSDKUpdateCustomizeDiffNewComputed checks what a resource's
// Update sees for an attribute that its CustomizeDiff marked unknown, as
// aws_ssm_document does for default_version when content changes. Terraform
// applies a diff recomputed without CustomizeDiff, so the attribute keeps its
// prior value. The SDKv2 external client must give Update the same view.
func TestTerraformPluginSDKUpdateCustomizeDiffNewComputed(t *testing.T) {
	const attr = "computed"
	object := func(content string, computed cty.Value) cty.Value {
		return cty.ObjectVal(map[string]cty.Value{
			"id":      cty.StringVal("doc"),
			"content": cty.StringVal(content),
			attr:      computed,
		})
	}

	cases := map[string]struct {
		reason string
		schema *schema.Schema
		// prior is the flatmap form of the attribute in the prior state.
		prior map[string]string
		// priorVal is the prior value, and unknown the planned (unknown) one.
		priorVal cty.Value
		unknown  cty.Value
		// want is the value Update must see through d.Get.
		want any
	}{
		"Scalar": {
			reason:   "A computed string is dropped from the diff and keeps its prior value.",
			schema:   &schema.Schema{Type: schema.TypeString, Computed: true},
			prior:    map[string]string{attr: "1"},
			priorVal: cty.StringVal("1"),
			unknown:  cty.UnknownVal(cty.String),
			want:     "1",
		},
		"List": {
			reason:   "A computed list stays unknown, so Update sees it empty, as under Terraform.",
			schema:   &schema.Schema{Type: schema.TypeList, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
			prior:    map[string]string{attr + ".#": "1", attr + ".0": "p"},
			priorVal: cty.ListVal([]cty.Value{cty.StringVal("p")}),
			unknown:  cty.UnknownVal(cty.List(cty.String)),
			want:     []any{},
		},
		"Map": {
			reason:   "A computed map stays unknown, so Update sees it empty, as under Terraform.",
			schema:   &schema.Schema{Type: schema.TypeMap, Computed: true, Elem: &schema.Schema{Type: schema.TypeString}},
			prior:    map[string]string{attr + ".%": "1", attr + ".k": "v"},
			priorVal: cty.MapVal(map[string]cty.Value{"k": cty.StringVal("v")}),
			unknown:  cty.UnknownVal(cty.Map(cty.String)),
			want:     map[string]any{},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got any
			r := &schema.Resource{
				Schema: map[string]*schema.Schema{
					"content": {Type: schema.TypeString, Required: true},
					attr:      tc.schema,
				},
				CustomizeDiff: func(_ context.Context, d *schema.ResourceDiff, _ any) error {
					if d.HasChange("content") {
						return d.SetNewComputed(attr)
					}
					return nil
				},
				UpdateContext: func(_ context.Context, d *schema.ResourceData, _ any) diag.Diagnostics {
					got = d.Get(attr)
					return nil
				},
			}
			prior := func() *tf.InstanceState {
				attrs := map[string]string{"id": "doc", "content": "old"}
				for k, v := range tc.prior {
					attrs[k] = v
				}
				return &tf.InstanceState{ID: "doc", Attributes: attrs}
			}

			// Terraform: the apply-time diff is recomputed without CustomizeDiff.
			stripped := *r
			stripped.CustomizeDiff = nil
			applyDiff, err := schema.DiffFromValues(t.Context(),
				object("old", tc.priorVal), object("new", tc.unknown), object("new", cty.NullVal(tc.unknown.Type())), &stripped)
			if err != nil {
				t.Fatalf("\n%s\nDiffFromValues(...): %v", tc.reason, err)
			}
			if _, diags := r.Apply(t.Context(), prior(), applyDiff, nil); diags.HasError() {
				t.Fatalf("\n%s\nApply(...): %v", tc.reason, diags)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("\n%s\nTerraform reference: -want, +got:\n%s", tc.reason, diff)
			}

			// upjet: the diff computed with CustomizeDiff, as in getResourceDataDiff.
			got = nil
			params := map[string]any{"content": "new"}
			observed, err := schema.InternalMap(r.Schema).Diff(t.Context(), prior(), tf.NewResourceConfigRaw(params), r.CustomizeDiff, nil, false)
			if err != nil {
				t.Fatalf("\n%s\nDiff(...): %v", tc.reason, err)
			}
			if a := observed.Attributes[attr]; a == nil && len(observed.Attributes) < 2 {
				t.Fatalf("\n%s\nprecondition: the observed diff has no unknown %q: %+v", tc.reason, attr, observed.Attributes)
			}
			wantAttrs := len(observed.Attributes)
			rawConfig, err := schema.JSONMapToStateValue(params, r.CoreConfigSchema())
			if err != nil {
				t.Fatalf("\n%s\nJSONMapToStateValue(...): %v", tc.reason, err)
			}
			n := &terraformPluginSDKExternal{
				ts:             terraform.Setup{},
				resourceSchema: r,
				config: &config.Resource{
					TerraformResource: r,
					ExternalName:      config.IdentifierFromProvider,
					Sensitive: config.Sensitive{AdditionalConnectionDetailsFn: func(map[string]any) (map[string][]byte, error) {
						return nil, nil
					}},
				},
				params:       params,
				rawConfig:    rawConfig,
				logger:       logTest,
				opTracker:    NewAsyncTracker(),
				instanceDiff: observed,
			}
			n.opTracker.SetTfState(prior())
			obj := fake.Terraformed{
				Parameterizable: fake.Parameterizable{Parameters: params},
				Observable:      fake.Observable{Observation: map[string]any{}},
			}
			if _, err := n.Update(t.Context(), &obj); err != nil {
				t.Fatalf("\n%s\nUpdate(...): %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nUpdate(...): what the resource sees, -want, +got:\n%s", tc.reason, diff)
			}
			if len(n.instanceDiff.Attributes) != wantAttrs {
				t.Errorf("\n%s\nUpdate(...) must not modify the observed diff: had %d attributes, now %d", tc.reason, wantAttrs, len(n.instanceDiff.Attributes))
			}
		})
	}
}
