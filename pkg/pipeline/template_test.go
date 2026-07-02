// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/crossplane/upjet/v2/pkg/pipeline/templates"
)

func TestTemplateOrDefault(t *testing.T) {
	const custom = "// custom template"

	type args struct {
		override        string
		defaultTemplate string
	}
	type want struct {
		template string
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"ControllerDefault": {
			reason: "The built-in controller template should be used when no override is configured.",
			args:   args{override: "", defaultTemplate: templates.ControllerTemplate},
			want:   want{template: templates.ControllerTemplate},
		},
		"ControllerOverride": {
			reason: "The configured controller template override should be used.",
			args:   args{override: custom, defaultTemplate: templates.ControllerTemplate},
			want:   want{template: custom},
		},
		"SetupAggregatorDefault": {
			reason: "The built-in setup aggregator template should be used when no override is configured.",
			args:   args{override: "", defaultTemplate: templates.SetupTemplate},
			want:   want{template: templates.SetupTemplate},
		},
		"SetupAggregatorOverride": {
			reason: "The configured setup aggregator template override should be used.",
			args:   args{override: custom, defaultTemplate: templates.SetupTemplate},
			want:   want{template: custom},
		},
		"TerraformedDefault": {
			reason: "The built-in terraformed template should be used when no override is configured.",
			args:   args{override: "", defaultTemplate: templates.TerraformedTemplate},
			want:   want{template: templates.TerraformedTemplate},
		},
		"TerraformedOverride": {
			reason: "The configured terraformed template override should be used.",
			args:   args{override: custom, defaultTemplate: templates.TerraformedTemplate},
			want:   want{template: custom},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := templateOrDefault(tc.args.override, tc.args.defaultTemplate)
			if diff := cmp.Diff(tc.want.template, got); diff != "" {
				t.Errorf("\n%s\ntemplateOrDefault(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
