package migration

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
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
				},
			},
			want: want{
				ks: &KubernetesSource{
					items: []UnstructuredWithMetadata{
						{
							Object: unstructured.Unstructured{
								Object: unstructuredAwsVpc,
							},
						},
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(),
				&unstructured.Unstructured{Object: unstructuredAwsVpc},
				&unstructured.Unstructured{Object: unstructuredResourceGroup})
			ks, err := NewKubernetesSource(dynamicClient, tc.args.gvks)
			if diff := cmp.Diff(tc.want.err, err); diff != "" {
				t.Errorf("\nNext(...): -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.ks.items, ks.items); diff != "" {
				t.Errorf("\nNext(...): -want, +got:\n%s", diff)
			}
		})
	}
}
