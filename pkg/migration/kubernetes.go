package migration

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesSource is a source implementation to read resources from Kubernetes
// cluster.
type KubernetesSource struct {
	index         int
	items         []UnstructuredWithMetadata
	dynamicClient dynamic.Interface
}

// NewKubernetesSource returns a KubernetesSource
func NewKubernetesSource(dynamicClient dynamic.Interface, gvks []schema.GroupVersionKind) (*KubernetesSource, error) {
	ks := &KubernetesSource{
		dynamicClient: dynamicClient,
	}
	for _, gvk := range gvks {
		ri := dynamicClient.Resource(
			schema.GroupVersionResource{
				Group:   gvk.Group,
				Version: gvk.Version,
				// we need to add plural appendix to end of kind name
				Resource: strings.ToLower(gvk.Kind) + "s",
			})
		unstructuredList, err := ri.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, u := range unstructuredList.Items {
			ks.items = append(ks.items, UnstructuredWithMetadata{
				Object: u,
				Metadata: Metadata{
					Path: string(u.GetUID()),
				},
			})
		}
	}
	return ks, nil
}

// HasNext checks the next item
func (ks *KubernetesSource) HasNext() (bool, error) {
	if ks.index < len(ks.items) {
		return true, nil
	}
	return false, nil
}

// Next returns the next item of slice
func (ks *KubernetesSource) Next() (UnstructuredWithMetadata, error) {
	if hasNext, _ := ks.HasNext(); hasNext {
		item := ks.items[ks.index]
		ks.index++
		return item, nil
	}
	return UnstructuredWithMetadata{}, errors.New("failed to get next element")
}

// InitializeDynamicClient returns a dynamic client
func InitializeDynamicClient(kubeconfigPath string) (dynamic.Interface, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return dynamicClient, nil
}
