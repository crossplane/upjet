// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

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
		want        map[string]any
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
			if err := preserveInitProviderExclusiveParams(tr, tc.params, tc.state, &config.Resource{}); err != nil {
				t.Fatalf("\n%s\npreserveInitProviderExclusiveParams(...): unexpected error: %v", tc.reason, err)
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
