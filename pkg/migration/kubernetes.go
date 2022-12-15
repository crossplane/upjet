package migration

import (
	"context"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
// DynamicClient is used here to query resources.
// Elements of gvks (slice of GroupVersionKind) are passed to the Dynamic Client
// in a loop to get list of resources.
// An example element of gvks slice:
// Group:   "ec2.aws.upbound.io",
// Version: "v1beta1",
// Kind:    "VPC",
func NewKubernetesSource(r *Registry, dynamicClient dynamic.Interface) (*KubernetesSource, error) {
	ks := &KubernetesSource{
		dynamicClient: dynamicClient,
	}
	if err := ks.getResources(r.claimTypes, CategoryClaim); err != nil {
		return nil, errors.Wrap(err, "cannot get claims")
	}
	if err := ks.getResources(r.compositeTypes, CategoryComposite); err != nil {
		return nil, errors.Wrap(err, "cannot get composites")
	}
	if err := ks.getResources(r.GetCompositionGVKs(), CategoryComposition); err != nil {
		return nil, errors.Wrap(err, "cannot get compositions")
	}
	if err := ks.getResources(r.GetManagedResourceGVKs(), CategoryManaged); err != nil {
		return nil, errors.Wrap(err, "cannot get managed resources")
	}
	return ks, nil
}

func (ks *KubernetesSource) getResources(gvks []schema.GroupVersionKind, category Category) error {
	for _, gvk := range gvks {
		// TODO: we are not using discovery as of now (to be reconsidered).
		// This will not in all cases.
		pluralGVR, _ := meta.UnsafeGuessKindToResource(gvk)
		ri := ks.dynamicClient.Resource(pluralGVR)
		unstructuredList, err := ri.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return errors.Wrap(err, "cannot list resources")
		}
		for _, u := range unstructuredList.Items {
			ks.items = append(ks.items, UnstructuredWithMetadata{
				Object: u,
				Metadata: Metadata{
					Path:     string(u.GetUID()),
					Category: category,
				},
			})
		}
	}
	return nil
}

// HasNext checks the next item
func (ks *KubernetesSource) HasNext() (bool, error) {
	return ks.index < len(ks.items), nil
}

// Next returns the next item of slice
func (ks *KubernetesSource) Next() (UnstructuredWithMetadata, error) {
	if hasNext, _ := ks.HasNext(); hasNext {
		item := ks.items[ks.index]
		ks.index++
		return item, nil
	}
	return UnstructuredWithMetadata{}, errors.New("no more elements")
}

// InitializeDynamicClient returns a dynamic client
func InitializeDynamicClient(kubeconfigPath string) (dynamic.Interface, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create rest config object")
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "cannot initialize dynamic client")
	}
	return dynamicClient, nil
}
