// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic/fake"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
)

func TestNewKubernetesSource(t *testing.T) {
	type args struct {
		gvks []schema.GroupVersionKind
	}
	type want struct {
		ks  *KubernetesSource
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Successful": {
			args: args{
				gvks: []schema.GroupVersionKind{
					{
						Group:   "ec2.aws.crossplane.io",
						Version: "v1beta1",
						Kind:    "VPC",
					},
					{
						Group:   "azure.crossplane.io",
						Version: "v1beta1",
						Kind:    "ResourceGroup",
					},
				},
			},
			want: want{
				ks: &KubernetesSource{
					items: []UnstructuredWithMetadata{
						{
							Object: unstructured.Unstructured{
								Object: unstructuredAwsVpc,
							},
							Metadata: Metadata{
								Category: CategoryManaged,
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: unstructuredResourceGroup,
							},
							Metadata: Metadata{
								Category: CategoryManaged,
							},
						},
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			r := NewRegistry(s)
			// register a dummy converter for the test GVKs
			r.resourceConverters = map[schema.GroupVersionKind]ResourceConverter{}
			for _, gvk := range tc.args.gvks {
				r.resourceConverters[gvk] = nil
			}
			dynamicClient := fake.NewSimpleDynamicClientWithCustomListKinds(s,
				map[schema.GroupVersionResource]string{
					{
						Group:    "ec2.aws.crossplane.io",
						Version:  "v1beta1",
						Resource: "vpcs",
					}: "VPCList",
					{
						Group:    "azure.crossplane.io",
						Version:  "v1beta1",
						Resource: "resourcegroups",
					}: "ResourceGroupList",
				},
				&unstructured.Unstructured{Object: unstructuredAwsVpc},
				&unstructured.Unstructured{Object: unstructuredResourceGroup})
			client := fakeclientset.NewSimpleClientset(
				&unstructured.Unstructured{Object: unstructuredAwsVpc},
				&unstructured.Unstructured{Object: unstructuredResourceGroup},
			)
			client.Fake.Resources = []*metav1.APIResourceList{
				{
					GroupVersion: "ec2.aws.crossplane.io/v1beta1",
					APIResources: []metav1.APIResource{
						{
							Name: "vpcs",
							Kind: "VPC",
						},
					},
				},
				{
					GroupVersion: "azure.crossplane.io/v1beta1",
					APIResources: []metav1.APIResource{
						{
							Name: "resourcegroups",
							Kind: "ResourceGroup",
						},
					},
				},
			}

			ks, err := NewKubernetesSource(dynamicClient, memory.NewMemCacheClient(client.Discovery()), WithRegistry(r))
			if diff := cmp.Diff(tc.want.err, err); diff != "" {
				t.Errorf("\nNext(...): -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.ks.items, ks.items); diff != "" {
				t.Errorf("\nNext(...): -want, +got:\n%s", diff)
			}
		})
	}
}
