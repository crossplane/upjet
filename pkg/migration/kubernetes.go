package migration

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	_ Source = &KubernetesSource{}
)

// KubernetesSource is a source implementation to read resources from Kubernetes
// cluster.
type KubernetesSource struct {
	index           int
	items           []UnstructuredWithMetadata
	dynamicClient   dynamic.Interface
	discoveryClient discovery.DiscoveryInterface
	rootAPIGroup    string
}

// NewKubernetesSource returns a KubernetesSource
// DynamicClient is used here to query resources.
// Elements of gvks (slice of GroupVersionKind) are passed to the Dynamic Client
// in a loop to get list of resources.
// An example element of gvks slice:
// Group:   "ec2.aws.upbound.io",
// Version: "v1beta1",
// Kind:    "VPC",
func NewKubernetesSource(r *Registry, dynamicClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface, rootAPIGroup string) (*KubernetesSource, error) {
	ks := &KubernetesSource{
		dynamicClient:   dynamicClient,
		discoveryClient: discoveryClient,
		rootAPIGroup:    rootAPIGroup,
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
	if err := ks.getResources(nil, CategoryManaged); err != nil {
		return nil, errors.Wrap(err, "cannot get managed resources")
	}
	return ks, nil
}

func (ks *KubernetesSource) getResources(gvks []schema.GroupVersionKind, category Category) error {
	if category == CategoryManaged {
		if err := ks.getManagedResources(ks.rootAPIGroup); err != nil {
			return errors.Wrap(err, "cannot list resources")
		}
	} else {
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

// Reset resets the source so that resources can be reread from the beginning.
func (ks *KubernetesSource) Reset() error {
	ks.index = 0
	return nil
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

func InitializeDiscoveryClient(kubeconfigPath string) (*disk.CachedDiscoveryClient, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create rest config object")
	}
	return disk.NewCachedDiscoveryClientForConfig(config, "", "", time.Duration(10*time.Minute)) // nolint:unconvert
}

func (ks *KubernetesSource) getManagedResources(rootAPIGroup string) error {
	groups, err := ks.discoveryClient.ServerGroups()
	if err != nil {
		return errors.Wrap(err, "cannot get API groups")
	}
	for _, group := range groups.Groups {
		if strings.Contains(group.Name, rootAPIGroup) {
			gv := schema.GroupVersion{
				Group:   group.Name,
				Version: group.Versions[0].Version,
			}.String()
			resources, err := ks.discoveryClient.ServerResourcesForGroupVersion(gv)
			if err != nil {
				return errors.Wrap(err, "cannot get resources")
			}
			for _, resource := range resources.APIResources {
				// Exclude status subresources from discovery process
				if strings.Contains(resource.Name, "/status") {
					continue
				}
				pluralGVR := schema.GroupVersionResource{
					Group:    group.Name,
					Version:  group.Versions[0].Version,
					Resource: resource.Name,
				}
				list, err := ks.dynamicClient.Resource(pluralGVR).List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return errors.Wrap(err, "cannot list resources")
				}
				for _, l := range list.Items {
					ks.items = append(ks.items, UnstructuredWithMetadata{
						Object: l,
						Metadata: Metadata{
							Path:     string(l.GetUID()),
							Category: CategoryManaged,
						},
					})
				}
			}
		}
	}
	return nil
}
