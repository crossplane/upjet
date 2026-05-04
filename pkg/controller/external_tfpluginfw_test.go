// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
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
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
	"github.com/crossplane/upjet/v2/pkg/terraform"
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
	readDiags []*tfprotov6.Diagnostic

	applyErr   error
	applyDiags []*tfprotov6.Diagnostic

	planErr   error
	planDiags []*tfprotov6.Diagnostic

	// Identity fields for response mocking
	readNewIdentity     *tfprotov6.ResourceIdentityData
	planPlannedIdentity *tfprotov6.ResourceIdentityData
	applyNewIdentity    *tfprotov6.ResourceIdentityData

	// Capture fields for verifying identity in requests
	capturedReadRequest  **tfprotov6.ReadResourceRequest
	capturedPlanRequest  **tfprotov6.PlanResourceChangeRequest
	capturedApplyRequest **tfprotov6.ApplyResourceChangeRequest
}

func prepareTPFExternalWithTestConfig(testConfig testConfiguration) *terraformPluginFrameworkExternalClient {
	testConfig.cfg.TerraformPluginFrameworkResource = testConfig.r
	schemaResp := &resource.SchemaResponse{}
	testConfig.r.Schema(context.TODO(), resource.SchemaRequest{}, schemaResp)
	tfValueType := schemaResp.Schema.Type().TerraformType(context.TODO())

	currentStateVal, err := protov6DynamicValueFromMap(testConfig.currentStateMap, tfValueType)
	if err != nil {
		panic("cannot prepare TPF")
	}
	plannedStateVal, err := protov6DynamicValueFromMap(testConfig.plannedStateMap, tfValueType)
	if err != nil {
		panic("cannot prepare TPF")
	}
	newStateAfterApplyVal, err := protov6DynamicValueFromMap(testConfig.newStateMap, tfValueType)
	if err != nil {
		panic("cannot prepare TPF")
	}
	return &terraformPluginFrameworkExternalClient{
		ts: terraform.Setup{
			FrameworkProvider: &mockTPFProvider{},
		},
		config: testConfig.cfg,
		logger: logTest,
		// metricRecorder:             nil,
		opTracker: NewAsyncTracker(),
		resource:  testConfig.r,
		server: &mockTPFProviderServer{
			ReadResourceFn: func(ctx context.Context, request *tfprotov6.ReadResourceRequest) (*tfprotov6.ReadResourceResponse, error) {
				if testConfig.capturedReadRequest != nil {
					*testConfig.capturedReadRequest = request
				}
				return &tfprotov6.ReadResourceResponse{
					NewState:    currentStateVal,
					NewIdentity: testConfig.readNewIdentity,
					Diagnostics: testConfig.readDiags,
				}, testConfig.readErr
			},
			PlanResourceChangeFn: func(ctx context.Context, request *tfprotov6.PlanResourceChangeRequest) (*tfprotov6.PlanResourceChangeResponse, error) {
				if testConfig.capturedPlanRequest != nil {
					*testConfig.capturedPlanRequest = request
				}
				return &tfprotov6.PlanResourceChangeResponse{
					PlannedState:    plannedStateVal,
					PlannedIdentity: testConfig.planPlannedIdentity,
					Diagnostics:     testConfig.planDiags,
				}, testConfig.planErr
			},
			ApplyResourceChangeFn: func(ctx context.Context, request *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error) {
				if testConfig.capturedApplyRequest != nil {
					*testConfig.capturedApplyRequest = request
				}
				return &tfprotov6.ApplyResourceChangeResponse{
					NewState:    newStateAfterApplyVal,
					NewIdentity: testConfig.applyNewIdentity,
					Diagnostics: testConfig.applyDiags,
				}, testConfig.applyErr
			},
		},
		params:                     testConfig.params,
		planResponse:               &tfprotov6.PlanResourceChangeResponse{PlannedState: plannedStateVal},
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
				applyDiags: []*tfprotov6.Diagnostic{
					{
						Severity: tfprotov6.DiagnosticSeverityError,
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

// newTestIdentityData creates a *tfprotov6.ResourceIdentityData for testing.
func newTestIdentityData(id string) *tfprotov6.ResourceIdentityData {
	identityType := tftypes.Object{
		AttributeTypes: map[string]tftypes.Type{
			"id": tftypes.String,
		},
	}
	identityVal := tftypes.NewValue(identityType, map[string]tftypes.Value{
		"id": tftypes.NewValue(tftypes.String, id),
	})
	dv, err := tfprotov6.NewDynamicValue(identityType, identityVal)
	if err != nil {
		panic("cannot create test identity data: " + err.Error())
	}
	return &tfprotov6.ResourceIdentityData{
		IdentityData: &dv,
	}
}

func TestTPFObserveIdentityPropagation(t *testing.T) {
	t.Run("ObservePassesCurrentIdentityAndStoresNewIdentity", func(t *testing.T) {
		existingIdentity := newTestIdentityData("existing-id")
		newIdentity := newTestIdentityData("refreshed-id")

		var capturedReadReq *tfprotov6.ReadResourceRequest
		tc := testConfiguration{
			r:   newMockTPFResourceWithIdentity(),
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
			readNewIdentity:     newIdentity,
			capturedReadRequest: &capturedReadReq,
		}
		tpfExternal := prepareTPFExternalWithTestConfig(tc)
		// Pre-set identity on the tracker to verify it's passed in the request
		tpfExternal.opTracker.SetFrameworkIdentity(existingIdentity)

		_, err := tpfExternal.Observe(context.TODO(), &tc.obj)
		if err != nil {
			t.Fatalf("Observe returned unexpected error: %v", err)
		}
		// Verify CurrentIdentity was passed in the read request
		if capturedReadReq == nil {
			t.Fatal("ReadResource was not called")
		}
		if capturedReadReq.CurrentIdentity != existingIdentity {
			t.Error("ReadResourceRequest.CurrentIdentity was not set to the tracker's identity")
		}
		// Verify NewIdentity from response was stored in the tracker
		storedIdentity := tpfExternal.opTracker.GetFrameworkIdentity()
		if storedIdentity != newIdentity {
			t.Error("NewIdentity from ReadResourceResponse was not stored in the tracker")
		}
	})

	t.Run("ObserveHandlesNilIdentity", func(t *testing.T) {
		var capturedReadReq *tfprotov6.ReadResourceRequest
		tc := testConfiguration{
			r:   newMockTPFResourceWithIdentity(),
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
			readNewIdentity:     nil,
			capturedReadRequest: &capturedReadReq,
		}
		tpfExternal := prepareTPFExternalWithTestConfig(tc)

		_, err := tpfExternal.Observe(context.TODO(), &tc.obj)
		if err != nil {
			t.Fatalf("Observe returned unexpected error: %v", err)
		}
		// With no pre-set identity, request should have nil CurrentIdentity
		if capturedReadReq.CurrentIdentity != nil {
			t.Error("ReadResourceRequest.CurrentIdentity should be nil when tracker has no identity")
		}
		// With nil identity in response, tracker should store nil
		if tpfExternal.opTracker.GetFrameworkIdentity() != nil {
			t.Error("Tracker identity should be nil when response has no identity")
		}
	})
}

func TestTPFCreateIdentityPropagation(t *testing.T) {
	t.Run("CreatePassesPlannedIdentityAndStoresNewIdentity", func(t *testing.T) {
		plannedIdentity := newTestIdentityData("planned-id")
		newIdentity := newTestIdentityData("created-id")

		var capturedApplyReq *tfprotov6.ApplyResourceChangeRequest
		tc := testConfiguration{
			r:               newMockTPFResourceWithIdentity(),
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
			planPlannedIdentity:  plannedIdentity,
			applyNewIdentity:     newIdentity,
			capturedApplyRequest: &capturedApplyReq,
		}
		tpfExternal := prepareTPFExternalWithTestConfig(tc)
		// Pre-set plannedIdentity as it would be after a Plan call
		tpfExternal.plannedIdentity = plannedIdentity

		_, err := tpfExternal.Create(context.TODO(), &tc.obj)
		if err != nil {
			t.Fatalf("Create returned unexpected error: %v", err)
		}
		if capturedApplyReq == nil {
			t.Fatal("ApplyResourceChange was not called")
		}
		if capturedApplyReq.PlannedIdentity != plannedIdentity {
			t.Error("ApplyResourceChangeRequest.PlannedIdentity was not set to the planned identity")
		}
		storedIdentity := tpfExternal.opTracker.GetFrameworkIdentity()
		if storedIdentity != newIdentity {
			t.Error("NewIdentity from ApplyResourceChangeResponse was not stored in the tracker")
		}
	})

	t.Run("CreateStoresIdentityEvenOnDiagError", func(t *testing.T) {
		newIdentity := newTestIdentityData("partial-id")

		tc := testConfiguration{
			r:               newMockTPFResourceWithIdentity(),
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
			applyDiags: []*tfprotov6.Diagnostic{
				{
					Severity: tfprotov6.DiagnosticSeverityError,
					Summary:  "partial failure",
					Detail:   "resource partially created",
				},
			},
			applyNewIdentity: newIdentity,
		}
		tpfExternal := prepareTPFExternalWithTestConfig(tc)

		_, err := tpfExternal.Create(context.TODO(), &tc.obj)
		if err == nil {
			t.Fatal("Create should have returned an error on diag error")
		}
		// Identity should still be stored even on error (like state is)
		storedIdentity := tpfExternal.opTracker.GetFrameworkIdentity()
		if storedIdentity != newIdentity {
			t.Error("NewIdentity should be stored in tracker even when apply returns error diags")
		}
	})
}

func TestTPFUpdateIdentityPropagation(t *testing.T) {
	t.Run("UpdatePassesPlannedIdentityAndStoresNewIdentity", func(t *testing.T) {
		plannedIdentity := newTestIdentityData("planned-id")
		newIdentity := newTestIdentityData("updated-id")

		var capturedApplyReq *tfprotov6.ApplyResourceChangeRequest
		tc := testConfiguration{
			r:   newMockTPFResourceWithIdentity(),
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
			planPlannedIdentity:  plannedIdentity,
			applyNewIdentity:     newIdentity,
			capturedApplyRequest: &capturedApplyReq,
		}
		tpfExternal := prepareTPFExternalWithTestConfig(tc)
		// Pre-set plannedIdentity as it would be after a Plan call
		tpfExternal.plannedIdentity = plannedIdentity

		_, err := tpfExternal.Update(context.TODO(), &tc.obj)
		if err != nil {
			t.Fatalf("Update returned unexpected error: %v", err)
		}
		if capturedApplyReq == nil {
			t.Fatal("ApplyResourceChange was not called")
		}
		if capturedApplyReq.PlannedIdentity != plannedIdentity {
			t.Error("ApplyResourceChangeRequest.PlannedIdentity was not set to the planned identity")
		}
		storedIdentity := tpfExternal.opTracker.GetFrameworkIdentity()
		if storedIdentity != newIdentity {
			t.Error("NewIdentity from ApplyResourceChangeResponse was not stored in the tracker")
		}
	})
}

func TestTPFDeleteIdentityPropagation(t *testing.T) {
	t.Run("DeleteSendsNilPlannedIdentityAndStoresNewIdentity", func(t *testing.T) {
		plannedIdentity := newTestIdentityData("planned-id")

		var capturedApplyReq *tfprotov6.ApplyResourceChangeRequest
		tc := testConfiguration{
			r:   newMockTPFResourceWithIdentity(),
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
			newStateMap:          nil,
			applyNewIdentity:     nil,
			capturedApplyRequest: &capturedApplyReq,
		}
		tpfExternal := prepareTPFExternalWithTestConfig(tc)
		// Pre-set plannedIdentity to simulate state after a prior Observe/Plan
		// call. Delete should NOT forward this stale update-plan identity.
		tpfExternal.plannedIdentity = plannedIdentity

		_, err := tpfExternal.Delete(context.TODO(), &tc.obj)
		if err != nil {
			t.Fatalf("Delete returned unexpected error: %v", err)
		}
		if capturedApplyReq == nil {
			t.Fatal("ApplyResourceChange was not called")
		}
		// PlannedIdentity must be nil for delete: the planned state is null
		// (resource is going away) so there is no meaningful planned identity.
		if capturedApplyReq.PlannedIdentity != nil {
			t.Error("ApplyResourceChangeRequest.PlannedIdentity should be nil for delete operations")
		}
		// After delete, identity from response (nil) should be stored
		storedIdentity := tpfExternal.opTracker.GetFrameworkIdentity()
		if storedIdentity != nil {
			t.Error("Tracker identity should be nil after delete with nil response identity")
		}
	})
}

func TestHasMissingResourceIdentityDiagnostic(t *testing.T) {
	cases := map[string]struct {
		diags []*tfprotov6.Diagnostic
		want  bool
	}{
		"NilDiags": {
			diags: nil,
			want:  false,
		},
		"EmptyDiags": {
			diags: []*tfprotov6.Diagnostic{},
			want:  false,
		},
		"UnrelatedError": {
			diags: []*tfprotov6.Diagnostic{
				{Severity: tfprotov6.DiagnosticSeverityError, Summary: "Some other error"},
			},
			want: false,
		},
		"WarningNotError": {
			diags: []*tfprotov6.Diagnostic{
				{Severity: tfprotov6.DiagnosticSeverityWarning, Summary: diagSummaryMissingResourceIdentity},
			},
			want: false,
		},
		"MatchingDiagnostic": {
			diags: []*tfprotov6.Diagnostic{
				{Severity: tfprotov6.DiagnosticSeverityError, Summary: diagSummaryMissingResourceIdentity},
			},
			want: true,
		},
		"MatchingAmongMultiple": {
			diags: []*tfprotov6.Diagnostic{
				{Severity: tfprotov6.DiagnosticSeverityWarning, Summary: "some warning"},
				{Severity: tfprotov6.DiagnosticSeverityError, Summary: diagSummaryMissingResourceIdentity},
			},
			want: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := hasMissingResourceIdentityDiagnostic(tc.diags)
			if got != tc.want {
				t.Errorf("hasMissingResourceIdentityDiagnostic() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTPFObserveMissingIdentityTreatedAsNotFound(t *testing.T) {
	tc := testConfiguration{
		r:   newMockTPFResourceWithIdentity(),
		cfg: newBaseUpjetConfig(),
		obj: newBaseObject(),
		params: map[string]any{
			"name": "example",
		},
		currentStateMap: map[string]any{
			"id":   "example-id",
			"name": "example",
		},
		plannedStateMap: map[string]any{
			"name": "example",
		},
		readDiags: []*tfprotov6.Diagnostic{
			{Severity: tfprotov6.DiagnosticSeverityError, Summary: diagSummaryMissingResourceIdentity},
		},
	}

	tpfExternal := prepareTPFExternalWithTestConfig(tc)
	obs, err := tpfExternal.Observe(context.TODO(), &tc.obj)
	if err != nil {
		t.Fatalf("Observe returned unexpected error: %v", err)
	}
	if obs.ResourceExists {
		t.Error("Expected ResourceExists to be false when Missing Resource Identity diagnostic is returned")
	}
	storedIdentity := tpfExternal.opTracker.GetFrameworkIdentity()
	if storedIdentity != nil {
		t.Error("Expected tracker identity to be nil after Missing Resource Identity diagnostic")
	}
}

// TestTPFPlanIdentityPropagation tests identity propagation through
// getDiffPlanResponse, which is unexported and invoked internally by Observe.
func TestTPFPlanIdentityPropagation(t *testing.T) {
	t.Run("ObserveRehydratesIdentityBeforePlan", func(t *testing.T) {
		refreshedIdentity := newTestIdentityData("refreshed-id")
		plannedIdentity := newTestIdentityData("planned-id")

		var capturedReadReq *tfprotov6.ReadResourceRequest
		var capturedPlanReq *tfprotov6.PlanResourceChangeRequest
		tc := testConfiguration{
			r:   newMockTPFResourceWithIdentity(),
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
			readNewIdentity:     refreshedIdentity,
			planPlannedIdentity: plannedIdentity,
			capturedReadRequest: &capturedReadReq,
			capturedPlanRequest: &capturedPlanReq,
		}

		tpfExternal := prepareTPFExternalWithTestConfig(tc)
		// Do NOT pre-set identity on the tracker — simulates a restart
		_, err := tpfExternal.Observe(context.TODO(), &tc.obj)
		if err != nil {
			t.Fatalf("Observe returned unexpected error: %v", err)
		}
		if capturedReadReq == nil {
			t.Fatal("ReadResource was not called")
		}
		if capturedReadReq.CurrentIdentity != nil {
			t.Fatal("ReadResourceRequest.CurrentIdentity should be nil during rehydration")
		}
		if capturedPlanReq == nil {
			t.Fatal("PlanResourceChange was not called")
		}
		if diff := cmp.Diff(refreshedIdentity, capturedPlanReq.PriorIdentity); diff != "" {
			t.Fatalf("PlanResourceChangeRequest.PriorIdentity mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("PlanPassesPriorIdentityAndCapturesPlannedIdentity", func(t *testing.T) {
		existingIdentity := newTestIdentityData("existing-id")
		plannedIdentity := newTestIdentityData("planned-id")

		var capturedPlanReq *tfprotov6.PlanResourceChangeRequest
		tc := testConfiguration{
			r:   newMockTPFResourceWithIdentity(),
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
			readNewIdentity:     existingIdentity,
			planPlannedIdentity: plannedIdentity,
			capturedPlanRequest: &capturedPlanReq,
		}
		tpfExternal := prepareTPFExternalWithTestConfig(tc)
		// Pre-set identity on the tracker to verify it's passed as PriorIdentity
		tpfExternal.opTracker.SetFrameworkIdentity(existingIdentity)

		// Observe triggers getDiffPlanResponse internally
		_, err := tpfExternal.Observe(context.TODO(), &tc.obj)
		if err != nil {
			t.Fatalf("Observe returned unexpected error: %v", err)
		}
		if capturedPlanReq == nil {
			t.Fatal("PlanResourceChange was not called")
		}
		if capturedPlanReq.PriorIdentity != existingIdentity {
			t.Error("PlanResourceChangeRequest.PriorIdentity was not set to the tracker's identity")
		}
		// Verify planned identity was captured on the client
		if tpfExternal.plannedIdentity != plannedIdentity {
			t.Error("PlannedIdentity from PlanResourceChangeResponse was not stored on the client")
		}
	})
}

// Mocks

var _ resource.Resource = &mockTPFResource{}
var _ tfprotov6.ProviderServer = &mockTPFProviderServer{}
var _ provider.Provider = &mockTPFProvider{}

type mockTPFProviderServer struct {
	GetMetadataFn          func(ctx context.Context, request *tfprotov6.GetMetadataRequest) (*tfprotov6.GetMetadataResponse, error)
	GetProviderSchemaFn    func(ctx context.Context, request *tfprotov6.GetProviderSchemaRequest) (*tfprotov6.GetProviderSchemaResponse, error)
	ConfigureProviderFn    func(ctx context.Context, request *tfprotov6.ConfigureProviderRequest) (*tfprotov6.ConfigureProviderResponse, error)
	StopProviderFn         func(ctx context.Context, request *tfprotov6.StopProviderRequest) (*tfprotov6.StopProviderResponse, error)
	UpgradeResourceStateFn func(ctx context.Context, request *tfprotov6.UpgradeResourceStateRequest) (*tfprotov6.UpgradeResourceStateResponse, error)
	ReadResourceFn         func(ctx context.Context, request *tfprotov6.ReadResourceRequest) (*tfprotov6.ReadResourceResponse, error)
	PlanResourceChangeFn   func(ctx context.Context, request *tfprotov6.PlanResourceChangeRequest) (*tfprotov6.PlanResourceChangeResponse, error)
	ApplyResourceChangeFn  func(ctx context.Context, request *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error)
	ImportResourceStateFn  func(ctx context.Context, request *tfprotov6.ImportResourceStateRequest) (*tfprotov6.ImportResourceStateResponse, error)
	ReadDataSourceFn       func(ctx context.Context, request *tfprotov6.ReadDataSourceRequest) (*tfprotov6.ReadDataSourceResponse, error)
}

func (m *mockTPFProviderServer) ValidateProviderConfig(ctx context.Context, request *tfprotov6.ValidateProviderConfigRequest) (*tfprotov6.ValidateProviderConfigResponse, error) {
	panic("implement me")
}

func (m *mockTPFProviderServer) ValidateResourceConfig(ctx context.Context, request *tfprotov6.ValidateResourceConfigRequest) (*tfprotov6.ValidateResourceConfigResponse, error) {
	panic("implement me")
}

func (m *mockTPFProviderServer) ValidateDataResourceConfig(ctx context.Context, request *tfprotov6.ValidateDataResourceConfigRequest) (*tfprotov6.ValidateDataResourceConfigResponse, error) {
	panic("implement me")
}

func (m *mockTPFProviderServer) UpgradeResourceIdentity(_ context.Context, _ *tfprotov6.UpgradeResourceIdentityRequest) (*tfprotov6.UpgradeResourceIdentityResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) GetResourceIdentitySchemas(_ context.Context, _ *tfprotov6.GetResourceIdentitySchemasRequest) (*tfprotov6.GetResourceIdentitySchemasResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) MoveResourceState(_ context.Context, _ *tfprotov6.MoveResourceStateRequest) (*tfprotov6.MoveResourceStateResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) CallFunction(_ context.Context, _ *tfprotov6.CallFunctionRequest) (*tfprotov6.CallFunctionResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) GetFunctions(_ context.Context, _ *tfprotov6.GetFunctionsRequest) (*tfprotov6.GetFunctionsResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) ValidateEphemeralResourceConfig(_ context.Context, _ *tfprotov6.ValidateEphemeralResourceConfigRequest) (*tfprotov6.ValidateEphemeralResourceConfigResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) OpenEphemeralResource(_ context.Context, _ *tfprotov6.OpenEphemeralResourceRequest) (*tfprotov6.OpenEphemeralResourceResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) RenewEphemeralResource(_ context.Context, _ *tfprotov6.RenewEphemeralResourceRequest) (*tfprotov6.RenewEphemeralResourceResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) CloseEphemeralResource(_ context.Context, _ *tfprotov6.CloseEphemeralResourceRequest) (*tfprotov6.CloseEphemeralResourceResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) GetMetadata(_ context.Context, _ *tfprotov6.GetMetadataRequest) (*tfprotov6.GetMetadataResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) GetProviderSchema(_ context.Context, _ *tfprotov6.GetProviderSchemaRequest) (*tfprotov6.GetProviderSchemaResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) ConfigureProvider(_ context.Context, _ *tfprotov6.ConfigureProviderRequest) (*tfprotov6.ConfigureProviderResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) StopProvider(_ context.Context, _ *tfprotov6.StopProviderRequest) (*tfprotov6.StopProviderResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) UpgradeResourceState(_ context.Context, _ *tfprotov6.UpgradeResourceStateRequest) (*tfprotov6.UpgradeResourceStateResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) ReadResource(ctx context.Context, request *tfprotov6.ReadResourceRequest) (*tfprotov6.ReadResourceResponse, error) {
	if m.ReadResourceFn == nil {
		return nil, nil
	}
	return m.ReadResourceFn(ctx, request)
}

func (m *mockTPFProviderServer) PlanResourceChange(ctx context.Context, request *tfprotov6.PlanResourceChangeRequest) (*tfprotov6.PlanResourceChangeResponse, error) {
	if m.PlanResourceChangeFn == nil {
		return nil, nil
	}
	return m.PlanResourceChangeFn(ctx, request)
}

func (m *mockTPFProviderServer) ApplyResourceChange(ctx context.Context, request *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error) {
	if m.ApplyResourceChangeFn == nil {
		return nil, nil
	}
	return m.ApplyResourceChangeFn(ctx, request)
}

func (m *mockTPFProviderServer) ImportResourceState(_ context.Context, _ *tfprotov6.ImportResourceStateRequest) (*tfprotov6.ImportResourceStateResponse, error) {
	// TODO implement me
	panic("implement me")
}

func (m *mockTPFProviderServer) ReadDataSource(_ context.Context, _ *tfprotov6.ReadDataSourceRequest) (*tfprotov6.ReadDataSourceResponse, error) {
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

var _ resource.ResourceWithIdentity = &mockTPFResourceWithIdentity{}

func (r *mockTPFResourceWithIdentity) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, _ *resource.IdentitySchemaResponse) {
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
				{Value1: nil, Value2: strVal("Cluster")},  // child attr, Value1 nil
				{Value1: nullVal(), Value2: strVal("3")},  // parent object null → removal
			},
			want: true,
		},
	}

	client := &terraformPluginFrameworkExternalClient{}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := client.filteredDiffExists(tc.rawDiff)
			if got != tc.want {
				t.Errorf("filteredDiffExists() = %v, want %v", got, tc.want)
			}
		})
	}
}
