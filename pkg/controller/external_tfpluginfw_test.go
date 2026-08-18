// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
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
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/resource/fake"
	"github.com/crossplane/upjet/pkg/terraform"
)

func newBaseObject() fake.Terraformed {
	return fake.Terraformed{
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

func newBaseSchema() rschema.Schema {
	return rschema.Schema{
		Attributes: map[string]rschema.Attribute{
			"name": rschema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"id": rschema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"map": rschema.MapAttribute{
				Required:    true,
				ElementType: types.StringType,
			},
			"list": rschema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func newMockBaseTPFResource() *mockTPFResource {
	return &mockTPFResource{
		SchemaMethod: func(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
			response.Schema = newBaseSchema()
		},
		ReadMethod: func(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
			response.State = tfsdk.State{
				Raw:    tftypes.Value{},
				Schema: nil,
			}
		},
	}
}

func newBaseUpjetConfig() *config.Resource {
	return &config.Resource{
		TerraformPluginFrameworkResource: newMockBaseTPFResource(),
		ExternalName:                     config.IdentifierFromProvider,
		Sensitive: config.Sensitive{AdditionalConnectionDetailsFn: func(attr map[string]any) (map[string][]byte, error) {
			return nil, nil
		}},
	}
}

type testConfiguration struct {
	r               resource.Resource
	cfg             *config.Resource
	obj             fake.Terraformed
	params          map[string]any
	currentStateMap map[string]any
	plannedStateMap map[string]any
	newStateMap     map[string]any

	readErr   error
	readDiags []*tfprotov5.Diagnostic

	applyErr   error
	applyDiags []*tfprotov5.Diagnostic

	planErr   error
	planDiags []*tfprotov5.Diagnostic
}

func prepareTPFExternalWithTestConfig(testConfig testConfiguration) *terraformPluginFrameworkExternalClient {
	testConfig.cfg.TerraformPluginFrameworkResource = testConfig.r
	schemaResp := &resource.SchemaResponse{}
	testConfig.r.Schema(context.TODO(), resource.SchemaRequest{}, schemaResp)
	tfValueType := schemaResp.Schema.Type().TerraformType(context.TODO())

	currentStateVal, err := protov5DynamicValueFromMap(testConfig.currentStateMap, tfValueType)
	if err != nil {
		panic("cannot prepare TPF")
	}
	plannedStateVal, err := protov5DynamicValueFromMap(testConfig.plannedStateMap, tfValueType)
	if err != nil {
		panic("cannot prepare TPF")
	}
	newStateAfterApplyVal, err := protov5DynamicValueFromMap(testConfig.newStateMap, tfValueType)
	if err != nil {
		panic("cannot prepare TPF")
	}
	return &terraformPluginFrameworkExternalClient{
		ts: terraform.Setup{
			FrameworkProvider: &mockTPFProvider{},
		},
		config: cfg,
		logger: logTest,
		// metricRecorder:             nil,
		opTracker: NewAsyncTracker(),
		resource:  testConfig.r,
		server: &mockTPFProviderServer{
			ReadResourceFn: func(ctx context.Context, request *tfprotov5.ReadResourceRequest) (*tfprotov5.ReadResourceResponse, error) {
				return &tfprotov5.ReadResourceResponse{
					NewState:    currentStateVal,
					Diagnostics: testConfig.readDiags,
				}, testConfig.readErr
			},
			PlanResourceChangeFn: func(ctx context.Context, request *tfprotov5.PlanResourceChangeRequest) (*tfprotov5.PlanResourceChangeResponse, error) {
				return &tfprotov5.PlanResourceChangeResponse{
					PlannedState: plannedStateVal,
					Diagnostics:  testConfig.planDiags,
				}, testConfig.planErr
			},
			ApplyResourceChangeFn: func(ctx context.Context, request *tfprotov5.ApplyResourceChangeRequest) (*tfprotov5.ApplyResourceChangeResponse, error) {
				return &tfprotov5.ApplyResourceChangeResponse{
					NewState:    newStateAfterApplyVal,
					Diagnostics: testConfig.applyDiags,
				}, testConfig.applyErr
			},
		},
		params:                     testConfig.params,
		planResponse:               &tfprotov5.PlanResourceChangeResponse{PlannedState: plannedStateVal},
		resourceSchema:             schemaResp.Schema,
		resourceValueTerraformType: tfValueType,
	}
}

func TestTPFConnect(t *testing.T) {
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
					return terraform.Setup{
						FrameworkProvider: &mockTPFProvider{},
					}, nil
				},
				cfg: newBaseUpjetConfig(),
				obj: newBaseObject(),
				ots: ots,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewTerraformPluginFrameworkConnector(nil, tc.args.setupFn, tc.args.cfg, tc.args.ots, WithTerraformPluginFrameworkLogger(logTest))
			_, err := c.Connect(context.TODO(), &tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestTPFObserve(t *testing.T) {
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
				obj:             obj,
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

		"LateInitialize": {
			testConfiguration: testConfiguration{
				r:   newMockBaseTPFResource(),
				cfg: newBaseUpjetConfig(),
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
				params: map[string]any{
					"id": "example-id",
				},
				currentStateMap: map[string]any{
					"id":   "example-id",
					"name": "example2",
				},
				plannedStateMap: map[string]any{
					"id":   "example-id",
					"name": "example2",
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
			tpfExternal := prepareTPFExternalWithTestConfig(tc.testConfiguration)
			observation, err := tpfExternal.Observe(context.TODO(), &tc.testConfiguration.obj)
			if diff := cmp.Diff(tc.want.obs, observation); diff != "" {
				t.Errorf("\n%s\nObserve(...): -want observation, +got observation:\n", diff)
			}
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestTPFCreate(t *testing.T) {
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
				obj:             obj,
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
		"EmptyStateAfterCreation": {
			testConfiguration: testConfiguration{
				r:               newMockBaseTPFResource(),
				cfg:             newBaseUpjetConfig(),
				obj:             obj,
				currentStateMap: nil,
				plannedStateMap: map[string]any{
					"name": "example",
				},
				params: map[string]any{
					"name": "example",
				},
				newStateMap: nil,
			},
			want: want{
				err: errors.New("new state is empty after creation"),
			},
		},
		"ApplyWithError": {
			testConfiguration: testConfiguration{
				r:               newMockBaseTPFResource(),
				cfg:             newBaseUpjetConfig(),
				obj:             obj,
				currentStateMap: nil,
				plannedStateMap: map[string]any{
					"name": "example",
				},
				params: map[string]any{
					"name": "example",
				},
				newStateMap: nil,
				applyErr:    errors.New("foo error"),
			},
			want: want{
				err: errors.Wrap(errors.New("foo error"), "cannot create resource"),
			},
		},
		"ApplyWithDiags": {
			testConfiguration: testConfiguration{
				r:               newMockBaseTPFResource(),
				cfg:             newBaseUpjetConfig(),
				obj:             obj,
				currentStateMap: nil,
				plannedStateMap: map[string]any{
					"name": "example",
				},
				params: map[string]any{
					"name": "example",
				},
				newStateMap: nil,
				applyDiags: []*tfprotov5.Diagnostic{
					{
						Severity: tfprotov5.DiagnosticSeverityError,
						Summary:  "foo summary",
						Detail:   "foo detail",
					},
				},
			},
			want: want{
				err: errors.Wrap(errors.New("foo summary: foo detail"), "resource creation call returned error diags"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tpfExternal := prepareTPFExternalWithTestConfig(tc.testConfiguration)
			_, err := tpfExternal.Create(context.TODO(), &tc.testConfiguration.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestTPFUpdate(t *testing.T) {
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
			tpfExternal := prepareTPFExternalWithTestConfig(tc.testConfiguration)
			_, err := tpfExternal.Update(context.TODO(), &tc.testConfiguration.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

func TestTPFDelete(t *testing.T) {

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
			tpfExternal := prepareTPFExternalWithTestConfig(tc.testConfiguration)
			_, err := tpfExternal.Delete(context.TODO(), &tc.testConfiguration.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n", diff)
			}
		})
	}
}

// Mocks

var _ resource.Resource = &mockTPFResource{}
var _ tfprotov5.ProviderServer = &mockTPFProviderServer{}
var _ provider.Provider = &mockTPFProvider{}

type mockTPFProviderServer struct {
	GetMetadataFn                func(ctx context.Context, request *tfprotov5.GetMetadataRequest) (*tfprotov5.GetMetadataResponse, error)
	GetProviderSchemaFn          func(ctx context.Context, request *tfprotov5.GetProviderSchemaRequest) (*tfprotov5.GetProviderSchemaResponse, error)
	PrepareProviderConfigFn      func(ctx context.Context, request *tfprotov5.PrepareProviderConfigRequest) (*tfprotov5.PrepareProviderConfigResponse, error)
	ConfigureProviderFn          func(ctx context.Context, request *tfprotov5.ConfigureProviderRequest) (*tfprotov5.ConfigureProviderResponse, error)
	StopProviderFn               func(ctx context.Context, request *tfprotov5.StopProviderRequest) (*tfprotov5.StopProviderResponse, error)
	ValidateResourceTypeConfigFn func(ctx context.Context, request *tfprotov5.ValidateResourceTypeConfigRequest) (*tfprotov5.ValidateResourceTypeConfigResponse, error)
	UpgradeResourceStateFn       func(ctx context.Context, request *tfprotov5.UpgradeResourceStateRequest) (*tfprotov5.UpgradeResourceStateResponse, error)
	ReadResourceFn               func(ctx context.Context, request *tfprotov5.ReadResourceRequest) (*tfprotov5.ReadResourceResponse, error)
	PlanResourceChangeFn         func(ctx context.Context, request *tfprotov5.PlanResourceChangeRequest) (*tfprotov5.PlanResourceChangeResponse, error)
	ApplyResourceChangeFn        func(ctx context.Context, request *tfprotov5.ApplyResourceChangeRequest) (*tfprotov5.ApplyResourceChangeResponse, error)
	ImportResourceStateFn        func(ctx context.Context, request *tfprotov5.ImportResourceStateRequest) (*tfprotov5.ImportResourceStateResponse, error)
	ValidateDataSourceConfigFn   func(ctx context.Context, request *tfprotov5.ValidateDataSourceConfigRequest) (*tfprotov5.ValidateDataSourceConfigResponse, error)
	ReadDataSourceFn             func(ctx context.Context, request *tfprotov5.ReadDataSourceRequest) (*tfprotov5.ReadDataSourceResponse, error)
}

func (m *mockTPFProviderServer) GetMetadata(_ context.Context, _ *tfprotov5.GetMetadataRequest) (*tfprotov5.GetMetadataResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) GetProviderSchema(_ context.Context, _ *tfprotov5.GetProviderSchemaRequest) (*tfprotov5.GetProviderSchemaResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) PrepareProviderConfig(_ context.Context, _ *tfprotov5.PrepareProviderConfigRequest) (*tfprotov5.PrepareProviderConfigResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) ConfigureProvider(_ context.Context, _ *tfprotov5.ConfigureProviderRequest) (*tfprotov5.ConfigureProviderResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) StopProvider(_ context.Context, _ *tfprotov5.StopProviderRequest) (*tfprotov5.StopProviderResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) ValidateResourceTypeConfig(_ context.Context, _ *tfprotov5.ValidateResourceTypeConfigRequest) (*tfprotov5.ValidateResourceTypeConfigResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) UpgradeResourceState(_ context.Context, _ *tfprotov5.UpgradeResourceStateRequest) (*tfprotov5.UpgradeResourceStateResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) ReadResource(ctx context.Context, request *tfprotov5.ReadResourceRequest) (*tfprotov5.ReadResourceResponse, error) {
	if m.ReadResourceFn == nil {
		return nil, nil
	}
	return m.ReadResourceFn(ctx, request)
}

func (m *mockTPFProviderServer) PlanResourceChange(ctx context.Context, request *tfprotov5.PlanResourceChangeRequest) (*tfprotov5.PlanResourceChangeResponse, error) {
	if m.PlanResourceChangeFn == nil {
		return nil, nil
	}
	return m.PlanResourceChangeFn(ctx, request)
}

func (m *mockTPFProviderServer) ApplyResourceChange(ctx context.Context, request *tfprotov5.ApplyResourceChangeRequest) (*tfprotov5.ApplyResourceChangeResponse, error) {
	if m.ApplyResourceChangeFn == nil {
		return nil, nil
	}
	return m.ApplyResourceChangeFn(ctx, request)
}

func (m *mockTPFProviderServer) ImportResourceState(_ context.Context, _ *tfprotov5.ImportResourceStateRequest) (*tfprotov5.ImportResourceStateResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) ValidateDataSourceConfig(_ context.Context, _ *tfprotov5.ValidateDataSourceConfigRequest) (*tfprotov5.ValidateDataSourceConfigResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) ReadDataSource(_ context.Context, _ *tfprotov5.ReadDataSourceRequest) (*tfprotov5.ReadDataSourceResponse, error) {
	// TODO implement me
	panic("implement me")
}

type mockTPFProvider struct {
	// Provider interface methods
	MetadataMethod    func(context.Context, provider.MetadataRequest, *provider.MetadataResponse)
	ConfigureMethod   func(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse)
	SchemaMethod      func(context.Context, provider.SchemaRequest, *provider.SchemaResponse)
	DataSourcesMethod func(context.Context) []func() datasource.DataSource
	ResourcesMethod   func(context.Context) []func() resource.Resource
}

// Configure satisfies the provider.Provider interface.
func (p *mockTPFProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	if p == nil || p.ConfigureMethod == nil {
		return
	}

	p.ConfigureMethod(ctx, req, resp)
}

// DataSources satisfies the provider.Provider interface.
func (p *mockTPFProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	if p == nil || p.DataSourcesMethod == nil {
		return nil
	}

	return p.DataSourcesMethod(ctx)
}

// Metadata satisfies the provider.Provider interface.
func (p *mockTPFProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	if p == nil || p.MetadataMethod == nil {
		return
	}

	p.MetadataMethod(ctx, req, resp)
}

// Schema satisfies the provider.Provider interface.
func (p *mockTPFProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	if p == nil || p.SchemaMethod == nil {
		return
	}

	p.SchemaMethod(ctx, req, resp)
}

// Resources satisfies the provider.Provider interface.
func (p *mockTPFProvider) Resources(ctx context.Context) []func() resource.Resource {
	if p == nil || p.ResourcesMethod == nil {
		return nil
	}

	return p.ResourcesMethod(ctx)
}

type mockTPFResource struct {
	// Resource interface methods
	MetadataMethod func(context.Context, resource.MetadataRequest, *resource.MetadataResponse)
	SchemaMethod   func(context.Context, resource.SchemaRequest, *resource.SchemaResponse)
	CreateMethod   func(context.Context, resource.CreateRequest, *resource.CreateResponse)
	DeleteMethod   func(context.Context, resource.DeleteRequest, *resource.DeleteResponse)
	ReadMethod     func(context.Context, resource.ReadRequest, *resource.ReadResponse)
	UpdateMethod   func(context.Context, resource.UpdateRequest, *resource.UpdateResponse)
}

// Metadata satisfies the resource.Resource interface.
func (r *mockTPFResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	if r.MetadataMethod == nil {
		return
	}

	r.MetadataMethod(ctx, req, resp)
}

// Schema satisfies the resource.Resource interface.
func (r *mockTPFResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	if r.SchemaMethod == nil {
		return
	}

	r.SchemaMethod(ctx, req, resp)
}

// Create satisfies the resource.Resource interface.
func (r *mockTPFResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.CreateMethod == nil {
		return
	}

	r.CreateMethod(ctx, req, resp)
}

// Delete satisfies the resource.Resource interface.
func (r *mockTPFResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.DeleteMethod == nil {
		return
	}

	r.DeleteMethod(ctx, req, resp)
}

// Read satisfies the resource.Resource interface.
func (r *mockTPFResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.ReadMethod == nil {
		return
	}

	r.ReadMethod(ctx, req, resp)
}

// Update satisfies the resource.Resource interface.
func (r *mockTPFResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.UpdateMethod == nil {
		return
	}

	r.UpdateMethod(ctx, req, resp)
}

// mockTPFResourceWithIdentity extends mockTPFResource by also implementing
// resource.ResourceWithIdentity, so supportsIdentity() returns true.
type mockTPFResourceWithIdentity struct {
	mockTPFResource
}

func newMockTPFResourceWithIdentity() *mockTPFResourceWithIdentity {
	return &mockTPFResourceWithIdentity{
		mockTPFResource: mockTPFResource{
			SchemaMethod: func(ctx context.Context, request resource.SchemaRequest, response *resource.SchemaResponse) {
				response.Schema = newBaseSchema()
			},
			ReadMethod: func(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
				response.State = tfsdk.State{
					Raw:    tftypes.Value{},
					Schema: nil,
				}
			},
		},
	}
}

func TestFilteredDiffExists(t *testing.T) {
	strVal := func(s string) *tftypes.Value {
		v := tftypes.NewValue(tftypes.String, s)
		return &v
	}
	nullVal := func() *tftypes.Value {
		v := tftypes.NewValue(tftypes.String, nil)
		return &v
	}
	unknownVal := func() *tftypes.Value {
		v := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
		return &v
	}

	cases := map[string]struct {
		rawDiff []tftypes.ValueDiff
		want    bool
	}{
		"EmptyDiff": {
			rawDiff: []tftypes.ValueDiff{},
			want:    false,
		},
		"PlannedNonNullPriorNull": {
			rawDiff: []tftypes.ValueDiff{
				{Value1: strVal("foo"), Value2: nullVal()},
			},
			want: true,
		},
		"PlannedNonNullPriorNonNull": {
			rawDiff: []tftypes.ValueDiff{
				{Value1: strVal("new"), Value2: strVal("old")},
			},
			want: true,
		},
		// Explicit removal: prior was set, planned is null. The fix ensures
		// this is not filtered out.
		"PlannedNullPriorNonNull": {
			rawDiff: []tftypes.ValueDiff{
				{Value1: nullVal(), Value2: strVal("foo")},
			},
			want: true,
		},
		// Field was never specified; both sides are null — no real diff.
		"PlannedNullPriorNull": {
			rawDiff: []tftypes.ValueDiff{
				{Value1: nullVal(), Value2: nullVal()},
			},
			want: false,
		},
		// Value1 nil means the child attribute has no individual planned value
		// (e.g. when its parent object is null). Should remain filtered.
		"PlannedNilPriorNonNull": {
			rawDiff: []tftypes.ValueDiff{
				{Value1: nil, Value2: strVal("foo")},
			},
			want: false,
		},
		// Unknown planned value corresponds to a computed field — filtered.
		"PlannedUnknownPriorNonNull": {
			rawDiff: []tftypes.ValueDiff{
				{Value1: unknownVal(), Value2: strVal("foo")},
			},
			want: false,
		},
		// Simulates optional nested object removal: child attribute diffs have
		// nil Value1, but the parent-level diff has null Value1 / non-null
		// Value2 and must be detected.
		"NestedObjectRemoval": {
			rawDiff: []tftypes.ValueDiff{
				{Value1: nil, Value2: strVal("ClusterIP")}, // child attr, Value1 nil
				{Value1: nil, Value2: strVal("Cluster")},   // child attr, Value1 nil
				{Value1: nullVal(), Value2: strVal("3")},   // parent object null → removal
			},
			want: true,
		},
	}

	client := &terraformPluginFrameworkExternalClient{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := client.filteredDiffExists(context.TODO(), tc.rawDiff)
			if got != tc.want {
				t.Errorf("filteredDiffExists() = %v, want %v", got, tc.want)
			}
		})
	}
}

// diffGatingSchema is a Terraform Plugin Framework resource schema that covers
// the schema node kinds a reported diff path can point at: the computed-only
// attributes and their descendants, the atomic collection attributes and their
// elements, the nested attributes, the blocks and the dynamic attributes.
func diffGatingSchema() rschema.Schema {
	nested := map[string]rschema.Attribute{"x": rschema.StringAttribute{Optional: true}}
	return rschema.Schema{
		Attributes: map[string]rschema.Attribute{
			"opt":              rschema.StringAttribute{Optional: true},
			"opt_computed":     rschema.StringAttribute{Optional: true, Computed: true},
			"computed_only":    rschema.StringAttribute{Computed: true},
			"tags":             rschema.MapAttribute{Optional: true, ElementType: types.StringType},
			"computed_tags":    rschema.MapAttribute{Computed: true, ElementType: types.StringType},
			"opt_parent":       rschema.SingleNestedAttribute{Optional: true, Attributes: nested},
			"computed_parent":  rschema.SingleNestedAttribute{Computed: true, Attributes: nested},
			"opt_dynamic":      rschema.DynamicAttribute{Optional: true},
			"computed_dynamic": rschema.DynamicAttribute{Computed: true},
			// the add-on shape of the reported issue: an optional root with the
			// computed children, so that the removal of the root is detectable
			"add_ons": rschema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]rschema.Attribute{
					"emissary": rschema.SingleNestedAttribute{
						Optional: true,
						Attributes: map[string]rschema.Attribute{
							"replicas": rschema.Int64Attribute{Optional: true, Computed: true},
						},
					},
				},
			},
		},
		Blocks: map[string]rschema.Block{
			"settings": rschema.SingleNestedBlock{Attributes: nested},
			"rules":    rschema.ListNestedBlock{NestedObject: rschema.NestedBlockObject{Attributes: nested}},
		},
	}
}

func TestFilteredDiffExistsComputedOnly(t *testing.T) {
	strVal := func(s string) *tftypes.Value {
		v := tftypes.NewValue(tftypes.String, s)
		return &v
	}
	nullVal := func() *tftypes.Value {
		v := tftypes.NewValue(tftypes.String, nil)
		return &v
	}
	unknownVal := func() *tftypes.Value {
		v := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
		return &v
	}
	// removal reports a diff where the previously set value at the given path
	// is now unset, i.e., the planned value is null and the prior value is not.
	removal := func(p *tftypes.AttributePath) tftypes.ValueDiff {
		return tftypes.ValueDiff{Path: p, Value1: nullVal(), Value2: strVal("prior")}
	}
	path := tftypes.NewAttributePath

	cases := map[string]struct {
		reason string
		diff   tftypes.ValueDiff
		want   bool
	}{
		"OptionalAttributeUnset": {
			reason: "Unsetting an optional attribute is a diff.",
			diff:   removal(path().WithAttributeName("opt")),
			want:   true,
		},
		"OptionalComputedAttributeUnset": {
			reason: "Unsetting an optional+computed attribute is a diff because it can be configured.",
			diff:   removal(path().WithAttributeName("opt_computed")),
			want:   true,
		},
		"ComputedOnlyAttributeUnset": {
			reason: "A computed-only attribute cannot be unset via the MR spec, so a null planned value is not a diff.",
			diff:   removal(path().WithAttributeName("computed_only")),
			want:   false,
		},
		"ComputedOnlyAttributeChanged": {
			reason: "A known and non-null planned value is a diff even for a computed-only attribute.",
			diff:   tftypes.ValueDiff{Path: path().WithAttributeName("computed_only"), Value1: strVal("planned"), Value2: strVal("prior")},
			want:   true,
		},
		"UnknownPlannedValue": {
			reason: "An unknown planned value corresponds to a computed field and is not a diff.",
			diff:   tftypes.ValueDiff{Path: path().WithAttributeName("opt"), Value1: unknownVal(), Value2: strVal("prior")},
			want:   false,
		},
		"OptionalMapElementUnset": {
			reason: "The elements of an atomic collection attribute are resolved to the enclosing attribute, which is configurable here.",
			diff:   removal(path().WithAttributeName("tags").WithElementKeyString("env")),
			want:   true,
		},
		"ComputedOnlyMapElementUnset": {
			reason: "The elements of an atomic collection attribute are resolved to the enclosing attribute, which is computed-only here.",
			diff:   removal(path().WithAttributeName("computed_tags").WithElementKeyString("env")),
			want:   false,
		},
		"AttributeUnderOptionalParentUnset": {
			reason: "A configurable attribute under a configurable parent can be unset.",
			diff:   removal(path().WithAttributeName("opt_parent").WithAttributeName("x")),
			want:   true,
		},
		"AttributeUnderComputedOnlyParentUnset": {
			reason: "Terraform does not allow a computed-only attribute to be configured, so its descendants cannot be unset either.",
			diff:   removal(path().WithAttributeName("computed_parent").WithAttributeName("x")),
			want:   false,
		},
		"OptionalNestedAttributeUnset": {
			reason: "Unsetting an optional nested attribute is a diff, which is the case reported for the add-ons.",
			diff:   removal(path().WithAttributeName("add_ons").WithAttributeName("emissary")),
			want:   true,
		},
		"ComputedChildOfOptionalNestedAttributeUnset": {
			reason: "An optional+computed child of a configurable nested attribute can be configured, so unsetting it is a diff.",
			diff:   removal(path().WithAttributeName("add_ons").WithAttributeName("emissary").WithAttributeName("replicas")),
			want:   true,
		},
		"DynamicSubPathUnderOptionalUnset": {
			reason: "A path under a dynamic attribute is resolved to the dynamic attribute itself, which is configurable here.",
			diff:   removal(path().WithAttributeName("opt_dynamic").WithAttributeName("foo")),
			want:   true,
		},
		"DynamicSubPathUnderComputedOnlyUnset": {
			reason: "A path under a dynamic attribute is resolved to the dynamic attribute itself, which is computed-only here.",
			diff:   removal(path().WithAttributeName("computed_dynamic").WithAttributeName("foo")),
			want:   false,
		},
		"BlockUnset": {
			reason: "A block is always configurable, so unsetting it is a diff.",
			diff:   removal(path().WithAttributeName("settings")),
			want:   true,
		},
		"BlockElementUnset": {
			reason: "A block element does not resolve to an attribute and the diff is preserved.",
			diff:   removal(path().WithAttributeName("rules").WithElementKeyInt(0)),
			want:   true,
		},
		"RootPathUnset": {
			reason: "The root path does not resolve to an attribute and the diff is preserved.",
			diff:   removal(path()),
			want:   true,
		},
	}

	n := &terraformPluginFrameworkExternalClient{resourceSchema: diffGatingSchema()}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := n.filteredDiffExists(context.TODO(), []tftypes.ValueDiff{tc.diff})
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("%s\nfilteredDiffExists(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

// TestFilteredDiffExistsNestedAttributeRemoval reproduces the reported
// scenario, where an optional nested attribute with the computed children is
// removed from the MR spec, on the raw diff computed by Terraform itself. The
// child attributes of the removed object have no planned values at all, so only
// the diff reported for the removed object carries the null planned value.
func TestFilteredDiffExistsNestedAttributeRemoval(t *testing.T) {
	ctx := context.TODO()
	// addOnsSchema returns a resource schema with an "add_ons.emissary" nested
	// attribute whose children are computed. The emissary attribute itself is
	// either configurable or computed-only.
	addOnsSchema := func(emissaryComputedOnly bool) rschema.Schema {
		emissary := rschema.SingleNestedAttribute{
			Optional: true,
			Attributes: map[string]rschema.Attribute{
				"replicas":     rschema.Int64Attribute{Optional: true, Computed: true},
				"service_type": rschema.StringAttribute{Optional: true, Computed: true},
			},
		}
		if emissaryComputedOnly {
			emissary.Optional = false
			emissary.Computed = true
		}
		return rschema.Schema{
			Attributes: map[string]rschema.Attribute{
				"add_ons": rschema.SingleNestedAttribute{
					Optional:   true,
					Attributes: map[string]rschema.Attribute{"emissary": emissary},
				},
			},
		}
	}

	cases := map[string]struct {
		reason string
		sch    rschema.Schema
		want   bool
	}{
		"ConfigurableAddOnRemoved": {
			reason: "Removing a configurable nested attribute must be reported as a diff so that an update is issued.",
			sch:    addOnsSchema(false),
			want:   true,
		},
		"ComputedOnlyAddOnRemoved": {
			reason: "A computed-only nested attribute cannot be removed via the MR spec, so the raw diff of the provider is filtered.",
			sch:    addOnsSchema(true),
			want:   false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resourceType, ok := tc.sch.Type().TerraformType(ctx).(tftypes.Object)
			if !ok {
				t.Fatalf("%s\nresource schema type is not a tftypes.Object", tc.reason)
			}
			addOnsType := resourceType.AttributeTypes["add_ons"].(tftypes.Object)  //nolint:forcetypeassert // the type is known from the schema above
			emissaryType := addOnsType.AttributeTypes["emissary"].(tftypes.Object) //nolint:forcetypeassert // the type is known from the schema above
			addOns := func(emissary tftypes.Value) tftypes.Value {
				return tftypes.NewValue(resourceType, map[string]tftypes.Value{
					"add_ons": tftypes.NewValue(addOnsType, map[string]tftypes.Value{"emissary": emissary}),
				})
			}
			// the external resource has the add-on configured
			prior := addOns(tftypes.NewValue(emissaryType, map[string]tftypes.Value{
				"replicas":     tftypes.NewValue(tftypes.Number, 3),
				"service_type": tftypes.NewValue(tftypes.String, "ClusterIP"),
			}))
			// the add-on has been removed from the MR spec
			planned := addOns(tftypes.NewValue(emissaryType, nil))

			rawDiff, err := planned.Diff(prior)
			if err != nil {
				t.Fatalf("%s\ncannot diff the planned and the prior values: %v", tc.reason, err)
			}
			if len(rawDiff) == 0 {
				t.Fatalf("%s\nexpected a non-empty raw diff for the removed add-on", tc.reason)
			}

			n := &terraformPluginFrameworkExternalClient{resourceSchema: tc.sch}
			got := n.filteredDiffExists(ctx, rawDiff)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("%s\nfilteredDiffExists(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
