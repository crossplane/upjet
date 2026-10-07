// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
)

func TestPreserveInitProviderExclusiveParams(t *testing.T) {
	tests := map[string]struct {
		reason      string
		forProvider map[string]any
		initParams  map[string]any
		params      map[string]any
		state       map[string]any
		tfType      tftypes.Type
		want        map[string]any
		wantChanged bool
	}{
		"InitExclusiveFieldsTakeStateValue": {
			reason: "initProvider-exclusive fields must keep the values of the external resource, so that they are not re-applied on update. Fields in forProvider must still be applied.",
			forProvider: map[string]any{
				"name":     "my-resource",
				"labels":   map[string]any{"a": "1"},
				"settings": []any{map[string]any{"enabled": true}},
			},
			initParams: map[string]any{
				"name":          "init-name",
				"initial_value": "v1",
				"labels":        map[string]any{"a": "1", "app.kubernetes.io/name": "x"},
				"settings":      []any{map[string]any{"enabled": true, "seed_value": float64(42)}},
			},
			params: map[string]any{
				"name":          "my-resource",
				"initial_value": "v1",
				"labels":        map[string]any{"a": "1", "app.kubernetes.io/name": "x"},
				"settings":      []any{map[string]any{"enabled": true, "seed_value": float64(42)}},
			},
			state: map[string]any{
				"name":          "changed-externally",
				"initial_value": "changed-externally",
				"labels":        map[string]any{"a": "1", "app.kubernetes.io/name": "y"},
				"settings":      []any{map[string]any{"enabled": true, "seed_value": int64(7)}},
			},
			want: map[string]any{
				"name":          "my-resource",
				"initial_value": "changed-externally",
				"labels":        map[string]any{"a": "1", "app.kubernetes.io/name": "y"},
				"settings":      []any{map[string]any{"enabled": true, "seed_value": int64(7)}},
			},
			wantChanged: true,
		},
		"InitExclusiveFieldsRemovedExternally": {
			reason: "initProvider-exclusive map keys and list elements that are not in the state must be removed from the config, so that they are not added again.",
			forProvider: map[string]any{
				"tags": map[string]any{"a": "1"},
				"list": []any{"a"},
			},
			initParams: map[string]any{
				"tags": map[string]any{"a": "1", "b": "2"},
				"list": []any{"a", "b", "c"},
			},
			params: map[string]any{
				"tags": map[string]any{"a": "1", "b": "2"},
				"list": []any{"a", "b", "c"},
			},
			state: map[string]any{
				"tags": map[string]any{"a": "1"},
				"list": []any{"a"},
			},
			want: map[string]any{
				"tags": map[string]any{"a": "1"},
				"list": []any{"a"},
			},
			wantChanged: true,
		},
		"ListElementsRemovedInDescendingIndexOrder": {
			reason:      "list[10] must be removed before list[9], so that a removal does not shift an element that must also be removed.",
			forProvider: map[string]any{"list": []any{"e0"}},
			initParams:  map[string]any{"list": []any{"e0", "e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8", "e9", "e10"}},
			params:      map[string]any{"list": []any{"e0", "e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8", "e9", "e10"}},
			state:       map[string]any{"list": []any{"e0", "e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8"}},
			want:        map[string]any{"list": []any{"e0", "e1", "e2", "e3", "e4", "e5", "e6", "e7", "e8"}},
			wantChanged: true,
		},
		"SetElementsLeftAsIs": {
			reason: "A set returned by Read has no stable order, so a spec index must not pick a set element from the state.",
			forProvider: map[string]any{
				"rule": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}},
				"strs": []any{"a"},
			},
			initParams: map[string]any{
				"rule": []any{map[string]any{"weight": float64(1)}, map[string]any{"weight": float64(2)}},
				"strs": []any{"a", "b"},
			},
			params: map[string]any{
				"rule": []any{map[string]any{"name": "a", "weight": float64(1)}, map[string]any{"name": "b", "weight": float64(2)}},
				"strs": []any{"a", "b"},
			},
			state: map[string]any{
				"rule": []any{map[string]any{"name": "b", "weight": int64(2)}, map[string]any{"name": "a", "weight": int64(1)}},
				"strs": []any{"b", "a"},
			},
			tfType: tftypes.Object{AttributeTypes: map[string]tftypes.Type{
				"rule": tftypes.Set{ElementType: tftypes.Object{AttributeTypes: map[string]tftypes.Type{
					"name":   tftypes.String,
					"weight": tftypes.Number,
				}}},
				"strs": tftypes.Set{ElementType: tftypes.String},
			}},
			want: map[string]any{
				"rule": []any{map[string]any{"name": "a", "weight": float64(1)}, map[string]any{"name": "b", "weight": float64(2)}},
				"strs": []any{"a", "b"},
			},
		},
		"UnparseablePathLeftAsIs": {
			reason:      "A map key that fieldpath cannot parse must not fail Observe on every reconcile.",
			forProvider: map[string]any{"tags": map[string]any{"a": "1"}},
			initParams:  map[string]any{"tags": map[string]any{"a": "1", "b[0]": "2"}},
			params:      map[string]any{"tags": map[string]any{"a": "1", "b[0]": "2"}},
			state:       map[string]any{"tags": map[string]any{"a": "1"}},
			want:        map[string]any{"tags": map[string]any{"a": "1", "b[0]": "2"}},
		},
		"EmptyInitProviderIsNoop": {
			reason:      "Without initProvider nothing changes, so that the config value is not rebuilt.",
			forProvider: map[string]any{"name": "my-resource"},
			params:      map[string]any{"name": "my-resource"},
			state:       map[string]any{"name": "changed-externally"},
			want:        map[string]any{"name": "my-resource"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			tr := &fake.Terraformed{
				Parameterizable: fake.Parameterizable{
					Parameters:     tc.forProvider,
					InitParameters: tc.initParams,
				},
			}
			changed, err := preserveInitProviderExclusiveParams(tr, tc.params, tc.state, &config.Resource{}, tc.tfType)
			if err != nil {
				t.Fatalf("\n%s\npreserveInitProviderExclusiveParams(...): unexpected error: %v", tc.reason, err)
			}
			if changed != tc.wantChanged {
				t.Errorf("\n%s\npreserveInitProviderExclusiveParams(...): want changed %v, got %v", tc.reason, tc.wantChanged, changed)
			}
			if diff := cmp.Diff(tc.want, tc.params); diff != "" {
				t.Errorf("\n%s\npreserveInitProviderExclusiveParams(...): -want +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestTPFObserveInitProviderExclusiveParams(t *testing.T) {
	obj := fake.Terraformed{
		Parameterizable: fake.Parameterizable{
			Parameters: map[string]any{
				"name": "example",
				"map":  map[string]any{"key": "value"},
				"list": []any{"elem1"},
			},
			InitParameters: map[string]any{
				"map": map[string]any{"key": "value", "init.only/key": "init"},
			},
		},
	}
	newParams := func() map[string]any {
		return map[string]any{
			"id":   "example-id",
			"name": "example",
			"map":  map[string]any{"key": "value", "init.only/key": "init"},
			"list": []any{"elem1"},
		}
	}
	existingState := map[string]any{
		"id":   "example-id",
		"name": "example",
		"map":  map[string]any{"key": "value", "init.only/key": "changed-externally"},
		"list": []any{"elem1"},
	}

	cases := map[string]struct {
		reason       string
		currentState map[string]any
		wantMap      map[string]any
	}{
		"ExistingResourceKeepsExternalValue": {
			reason:       "The plan config of an existing resource must have the current value of an initProvider-exclusive field, so that it is not re-applied.",
			currentState: existingState,
			wantMap:      map[string]any{"key": "value", "init.only/key": "changed-externally"},
		},
		"MissingResourceUsesInitProviderValue": {
			reason:       "The plan config of a resource that does not exist must have the initProvider value, so that the resource is created with it.",
			currentState: nil,
			wantMap:      map[string]any{"key": "value", "init.only/key": "init"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var planReq *tfprotov6.PlanResourceChangeRequest
			n := prepareTPFExternalWithTestConfig(testConfiguration{
				r:                   newMockBaseTPFResource(),
				cfg:                 newBaseUpjetConfig(),
				obj:                 obj,
				params:              newParams(),
				currentStateMap:     tc.currentState,
				plannedStateMap:     tc.currentState,
				capturedPlanRequest: &planReq,
			})
			n.isManagementPoliciesEnabled = true
			var err error
			n.resourceTerraformConfigValue, err = getResourceConfigTerraformValue(context.TODO(), n.config, n.resourceValueTerraformType, n.params, n.resourceSchema)
			if err != nil {
				t.Fatalf("getResourceConfigTerraformValue(...): unexpected error: %v", err)
			}

			if _, err := n.Observe(context.TODO(), &obj); err != nil {
				t.Fatalf("\n%s\nObserve(...): unexpected error: %v", tc.reason, err)
			}
			if planReq == nil {
				t.Fatal("PlanResourceChange was not called")
			}
			cfgVal, err := planReq.Config.Unmarshal(n.resourceValueTerraformType)
			if err != nil {
				t.Fatalf("cannot unmarshal plan config: %v", err)
			}
			got, err := tfValueToGoValue(cfgVal)
			if err != nil {
				t.Fatalf("cannot convert plan config: %v", err)
			}
			if diff := cmp.Diff(tc.wantMap, got.(map[string]any)["map"]); diff != "" {
				t.Errorf("\n%s\nObserve(...): plan config map -want +got:\n%s", tc.reason, diff)
			}
		})
	}
}
