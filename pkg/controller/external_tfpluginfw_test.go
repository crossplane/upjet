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
			err := tpfExternal.Delete(context.TODO(), &tc.testConfiguration.obj)
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
