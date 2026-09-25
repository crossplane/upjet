// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"testing"

	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kschema "k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
)

func terraformed(group, kind, tfType string) *fake.Terraformed {
	tr := &fake.Terraformed{
		MetadataProvider: fake.MetadataProvider{Type: tfType},
	}
	tr.TypeMeta = metav1.TypeMeta{APIVersion: group + "/v1beta1", Kind: kind}
	return tr
}

func TestSearchInRegexList(t *testing.T) {
	cases := map[string]struct {
		reason  string
		list    []string
		name    string
		want    bool
		wantErr bool
	}{
		"EmptyList": {
			reason: "Nothing matches an empty list.",
			list:   nil,
			name:   "aws_vpc",
		},
		"ExactMatch": {
			reason: "A pattern matching the whole name matches.",
			list:   []string{"aws_vpc"},
			name:   "aws_vpc",
			want:   true,
		},
		"PartialMatch": {
			reason: "Patterns are unanchored, so a substring pattern matches.",
			list:   []string{"vpc"},
			name:   "aws_vpc",
			want:   true,
		},
		"NoMatch": {
			reason: "A pattern that does not appear in the name does not match.",
			list:   []string{"aws_subnet", "aws_instance"},
			name:   "aws_vpc",
		},
		"SecondEntryMatches": {
			reason: "The whole list is searched, not only its first entry.",
			list:   []string{"aws_subnet", "aws_vpc$"},
			name:   "aws_vpc",
			want:   true,
		},
		"InvalidPattern": {
			reason:  "A pattern that does not compile is reported rather than skipped.",
			list:    []string{"aws_vpc[", "aws_vpc"},
			name:    "aws_vpc",
			wantErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := searchInRegexList(tc.list, tc.name)
			if tc.wantErr && err == nil {
				t.Fatalf("\n%s\nsearchInRegexList(...): want an error, got none", tc.reason)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("\n%s\nsearchInRegexList(...): want no error, got %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nsearchInRegexList(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestResourceType(t *testing.T) {
	cases := map[string]struct {
		reason  string
		pc      *config.Provider
		name    string
		want    config.ResourceType
		wantErr bool
	}{
		"NilProvider": {
			reason: "A nil provider classifies nothing.",
			pc:     nil,
			name:   "aws_vpc",
			want:   config.ResourceTypeUnknown,
		},
		"TerraformCLI": {
			reason: "The CLI include list is searched first.",
			pc:     &config.Provider{IncludeList: []string{"aws_vpc$"}},
			name:   "aws_vpc",
			want:   config.ResourceTypeTerraformCLI,
		},
		"TerraformPluginSDK": {
			reason: "A resource only in the plugin SDK list is an SDKv2 resource.",
			pc:     &config.Provider{TerraformPluginSDKIncludeList: []string{"aws_vpc$"}},
			name:   "aws_vpc",
			want:   config.ResourceTypeTerraformSDK,
		},
		"TerraformPluginFramework": {
			reason: "A resource only in the plugin framework list is a framework resource.",
			pc:     &config.Provider{TerraformPluginFrameworkIncludeList: []string{"aws_vpc$"}},
			name:   "aws_vpc",
			want:   config.ResourceTypeTerraformFramework,
		},
		"CLITakesPrecedence": {
			reason: "A resource in more than one list is classified by the first list searched.",
			pc: &config.Provider{
				IncludeList:                   []string{"aws_vpc$"},
				TerraformPluginSDKIncludeList: []string{"aws_vpc$"},
			},
			name: "aws_vpc",
			want: config.ResourceTypeTerraformCLI,
		},
		"InNoList": {
			reason: "A resource in none of the lists is unknown.",
			pc:     &config.Provider{TerraformPluginSDKIncludeList: []string{"aws_subnet$"}},
			name:   "aws_vpc",
			want:   config.ResourceTypeUnknown,
		},
		"InvalidPattern": {
			reason:  "A list entry that does not compile is reported.",
			pc:      &config.Provider{IncludeList: []string{"aws_vpc["}},
			name:    "aws_vpc",
			want:    config.ResourceTypeUnknown,
			wantErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := resourceType(tc.pc, tc.name)
			if tc.wantErr && err == nil {
				t.Fatalf("\n%s\nresourceType(...): want an error, got none", tc.reason)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("\n%s\nresourceType(...): want no error, got %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nresourceType(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestGetResourceConfiguration(t *testing.T) {
	sdkResource := &config.Resource{Name: "aws_vpc"}
	provider := &config.Provider{
		RootGroup:                     "aws.upbound.io",
		TerraformPluginSDKIncludeList: []string{"aws_vpc$"},
		Resources:                     map[string]*config.Resource{"aws_vpc": sdkResource},
	}

	cases := map[string]struct {
		reason   string
		configs  []*config.Provider
		mg       xpresource.Managed
		wantCfg  *config.Resource
		wantType config.ResourceType
		wantErr  bool
	}{
		"GroupWithoutASubdomain": {
			reason:   "A group with no dot cannot be split into a kind group and a root group.",
			configs:  []*config.Provider{provider},
			mg:       terraformed("aws", "VPC", "aws_vpc"),
			wantType: config.ResourceTypeUnknown,
			wantErr:  true,
		},
		"RootGroupDoesNotMatch": {
			reason:   "A provider configured for another root group does not supply the resource.",
			configs:  []*config.Provider{provider},
			mg:       terraformed("ec2.azure.upbound.io", "VPC", "aws_vpc"),
			wantType: config.ResourceTypeUnknown,
			wantErr:  true,
		},
		"ResourceNotRegistered": {
			reason:   "A resource the provider does not register is not found.",
			configs:  []*config.Provider{provider},
			mg:       terraformed("ec2.aws.upbound.io", "Subnet", "aws_subnet"),
			wantType: config.ResourceTypeUnknown,
			wantErr:  true,
		},
		"NoProviderConfigurations": {
			reason:   "With no provider configurations nothing can be found.",
			configs:  nil,
			mg:       terraformed("ec2.aws.upbound.io", "VPC", "aws_vpc"),
			wantType: config.ResourceTypeUnknown,
			wantErr:  true,
		},
		"RegisteredButInNoIncludeList": {
			reason: "A registered resource that no include list classifies is treated as not found, rather than returned with an unknown type.",
			configs: []*config.Provider{{
				RootGroup: "aws.upbound.io",
				Resources: map[string]*config.Resource{"aws_vpc": sdkResource},
			}},
			mg:       terraformed("ec2.aws.upbound.io", "VPC", "aws_vpc"),
			wantType: config.ResourceTypeUnknown,
			wantErr:  true,
		},
		"InvalidPatternInIncludeList": {
			reason: "A malformed include list entry is reported rather than treated as no match.",
			configs: []*config.Provider{{
				RootGroup:   "aws.upbound.io",
				IncludeList: []string{"aws_vpc["},
				Resources:   map[string]*config.Resource{"aws_vpc": sdkResource},
			}},
			mg:       terraformed("ec2.aws.upbound.io", "VPC", "aws_vpc"),
			wantType: config.ResourceTypeUnknown,
			wantErr:  true,
		},
		"Found": {
			reason:   "A registered resource classified by an include list is returned with its type.",
			configs:  []*config.Provider{provider},
			mg:       terraformed("ec2.aws.upbound.io", "VPC", "aws_vpc"),
			wantCfg:  sdkResource,
			wantType: config.ResourceTypeTerraformSDK,
		},
		"FoundInASecondProvider": {
			reason:   "Every configured provider is searched, not only the first.",
			configs:  []*config.Provider{{RootGroup: "azure.upbound.io"}, provider},
			mg:       terraformed("ec2.aws.upbound.io", "VPC", "aws_vpc"),
			wantCfg:  sdkResource,
			wantType: config.ResourceTypeTerraformSDK,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := &PlanService{providerConfigurations: tc.configs}
			cfg, got, err := s.getResourceConfiguration(tc.mg)
			if tc.wantErr && err == nil {
				t.Fatalf("\n%s\ngetResourceConfiguration(...): want an error, got none", tc.reason)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("\n%s\ngetResourceConfiguration(...): want no error, got %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.wantType, got); diff != "" {
				t.Errorf("\n%s\ngetResourceConfiguration(...): -want type, +got type:\n%s", tc.reason, diff)
			}
			if tc.wantCfg != cfg {
				t.Errorf("\n%s\ngetResourceConfiguration(...): want config %v, got %v", tc.reason, tc.wantCfg, cfg)
			}
		})
	}
}

func TestGetResourceConfigurationNotTerraformed(t *testing.T) {
	s := &PlanService{}
	mg := &notTerraformed{}
	mg.SetGroupVersionKind(terraformed("ec2.aws.upbound.io", "VPC", "aws_vpc").GroupVersionKind())

	if _, _, err := s.getResourceConfiguration(mg); err == nil {
		t.Error("getResourceConfiguration(...): want an error for a managed resource that is not Terraformed, got none")
	}
}

type notTerraformed struct {
	xpfake.Managed
	metav1.TypeMeta
}

func (n *notTerraformed) GetObjectKind() kschema.ObjectKind { return &n.TypeMeta }
