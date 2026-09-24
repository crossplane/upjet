// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
)

func TestDeleteNestedParam(t *testing.T) {
	tests := map[string]struct {
		params map[string]any
		path   string
		want   map[string]any
	}{
		"MissingIntermediateKeyIsNoop": {
			params: map[string]any{"a": 1},
			path:   "b.c.d",
			want:   map[string]any{"a": 1},
		},
		"ArrayIndexOutOfBoundsIsNoop": {
			params: map[string]any{
				"items": []any{
					map[string]any{"x": 1},
				},
			},
			path: "items.5.x",
			want: map[string]any{
				"items": []any{
					map[string]any{"x": 1},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			deleteNestedParam(tc.params, tc.path)
			if diff := cmp.Diff(tc.want, tc.params); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}

func TestRemoveInitProviderExclusiveParams(t *testing.T) {
	tests := map[string]struct {
		forProvider map[string]any
		initParams map[string]any
		params     map[string]any
		want       map[string]any
	}{
		"InitExclusiveFieldRemovedSharedPreserved": {
			forProvider: map[string]any{"name": "my-resource"},
			initParams:  map[string]any{"name": "my-resource", "initial_value": "v1"},
			params:      map[string]any{"name": "my-resource", "initial_value": "v1"},
			want:        map[string]any{"name": "my-resource"},
		},
		"ArrayNestedInitExclusiveField": {
			forProvider: map[string]any{
				"settings": []any{
					map[string]any{"enabled": true},
				},
			},
			initParams: map[string]any{
				"settings": []any{
					map[string]any{"enabled": true, "seed_value": float64(42)},
				},
			},
			params: map[string]any{
				"settings": []any{
					map[string]any{"enabled": true, "seed_value": float64(42)},
				},
			},
			want: map[string]any{
				"settings": []any{
					map[string]any{"enabled": true},
				},
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
			cfg := &config.Resource{}
			if err := removeInitProviderExclusiveParams(tr, tc.params, cfg); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.want, tc.params); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}
