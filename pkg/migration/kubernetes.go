package migration

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	errKubernetesSourceInit = "failed to initialize the migration Kubernetes source"
)

var (
	_               Source = &KubernetesSource{}
	defaultCacheDir        = filepath.Join(homedir.HomeDir(), ".kube", "cache")
)

// KubernetesSource is a source implementation to read resources from Kubernetes
// cluster.
type KubernetesSource struct {
	registry              *Registry
	categories            []Category
	index                 int
	items                 []UnstructuredWithMetadata
	dynamicClient         dynamic.Interface
	cachedDiscoveryClient discovery.CachedDiscoveryInterface
	restMapper            meta.RESTMapper
	categoryExpander      restmapper.CategoryExpander
	cacheDir              string
}

// KubernetesSourceOption sets an option for a KubernetesSource.
type KubernetesSourceOption func(source *KubernetesSource)

// WithCacheDir sets the cache directory for the disk cached discovery client
// used by a KubernetesSource.
func WithCacheDir(cacheDir string) KubernetesSourceOption {
	return func(s *KubernetesSource) {
		s.cacheDir = cacheDir
	}
}

// WithRegistry configures a KubernetesSource to use the specified registry
// for determining the GVKs of resources which will be read from the
// Kubernetes API server.
func WithRegistry(r *Registry) KubernetesSourceOption {
	return func(s *KubernetesSource) {
		s.registry = r
	}
}

// WithCategories configures a KubernetesSource so that it will fetch
// all resources belonging to the specified categories.
func WithCategories(c []Category) KubernetesSourceOption {
	return func(s *KubernetesSource) {
		s.categories = c
	}
}

// NewKubernetesSourceFromKubeConfig initializes a new KubernetesSource using
// the specified kube config file and KubernetesSourceOptions.
func NewKubernetesSourceFromKubeConfig(kubeconfigPath string, opts ...KubernetesSourceOption) (*KubernetesSource, error) {
	ks := &KubernetesSource{}
	for _, o := range opts {
		o(ks)
	}
	var err error
	ks.dynamicClient, err = InitializeDynamicClient(kubeconfigPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to initialize a Kubernetes dynamic client from kubeconfig: %s", kubeconfigPath)
	}
	ks.cachedDiscoveryClient, err = InitializeDiscoveryClient(kubeconfigPath, ks.cacheDir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to initialize a Kubernetes discovery client from kubeconfig: %s", kubeconfigPath)
	}
	return ks, errors.Wrap(ks.init(), errKubernetesSourceInit)
}

// NewKubernetesSource returns a KubernetesSource
// DynamicClient is used here to query resources.
// Elements of gvks (slice of GroupVersionKind) are passed to the Dynamic Client
// in a loop to get list of resources.
// An example element of gvks slice:
// Group:   "ec2.aws.upbound.io",
// Version: "v1beta1",
// Kind:    "VPC",
func NewKubernetesSource(dynamicClient dynamic.Interface, discoveryClient discovery.CachedDiscoveryInterface, opts ...KubernetesSourceOption) (*KubernetesSource, error) {
	ks := &KubernetesSource{
		dynamicClient:         dynamicClient,
		cachedDiscoveryClient: discoveryClient,
	}
	for _, o := range opts {
		o(ks)
	}
	return ks, errors.Wrap(ks.init(), errKubernetesSourceInit)
}

func (ks *KubernetesSource) init() error {
	ks.restMapper = restmapper.NewDeferredDiscoveryRESTMapper(ks.cachedDiscoveryClient)
	ks.categoryExpander = restmapper.NewDiscoveryCategoryExpander(ks.cachedDiscoveryClient)

	for _, c := range ks.categories {
		if err := ks.getCategoryResources(c); err != nil {
			return errors.Wrapf(err, "cannot get resources of the category: %s", c)
		}
	}

	if ks.registry == nil {
		return nil
	}
	if err := ks.getGVKResources(ks.registry.claimTypes, CategoryClaim); err != nil {
		return errors.Wrap(err, "cannot get claims")
	}
	if err := ks.getGVKResources(ks.registry.compositeTypes, CategoryComposite); err != nil {
		return errors.Wrap(err, "cannot get composites")
	}
	if err := ks.getGVKResources(ks.registry.GetCompositionGVKs(), CategoryComposition); err != nil {
		return errors.Wrap(err, "cannot get compositions")
	}
	return errors.Wrap(ks.getGVKResources(ks.registry.GetManagedResourceGVKs(), CategoryManaged), "cannot get managed resources")
}

func (ks *KubernetesSource) getCategoryResources(c Category) error {
	grs, _ := ks.categoryExpander.Expand(c.String())
	for _, gr := range grs {
		gvrs, err := ks.restMapper.ResourcesFor(schema.GroupVersionResource{
			Group:    gr.Group,
			Resource: gr.Resource,
		})
		if err != nil {
			return errors.Wrapf(err, "cannot discover GVRs for GroupResource: %s", gr.String())
		}
		for _, gvr := range gvrs {
			if err := ks.getResourcesFor(gvr, c); err != nil {
				return errors.Wrapf(err, "cannot get resources of the category: %s", c.String())
			}
		}
	}
	return nil
}

func (ks *KubernetesSource) getGVKResources(gvks []schema.GroupVersionKind, category Category) error {
	for _, gvk := range gvks {
		m, err := ks.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return errors.Wrapf(err, "cannot get REST mappings for GVK: %s", gvk.String())
		}
		if err := ks.getResourcesFor(m.Resource, category); err != nil {
			return errors.Wrapf(err, "cannot get resources for GVK: %s", gvk.String())
		}
	}
	return nil
}

func (ks *KubernetesSource) getResourcesFor(gvr schema.GroupVersionResource, category Category) error {
	ri := ks.dynamicClient.Resource(gvr)
	unstructuredList, err := ri.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return errors.Wrapf(err, "cannot list resources of GVR: %s", gvr.String())
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

func InitializeDiscoveryClient(kubeconfigPath, cacheDir string) (*disk.CachedDiscoveryClient, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create rest config object")
	}

	if cacheDir == "" {
		cacheDir = defaultCacheDir
	}
	httpCacheDir := filepath.Join(cacheDir, "http")
	discoveryCacheDir := computeDiscoverCacheDir(filepath.Join(cacheDir, "discovery"), config.Host)
	return disk.NewCachedDiscoveryClientForConfig(config, discoveryCacheDir, httpCacheDir, 10*time.Minute)
}

// overlyCautiousIllegalFileCharacters matches characters that *might* not be supported.  Windows is really restrictive, so this is really restrictive
var overlyCautiousIllegalFileCharacters = regexp.MustCompile(`[^(\w/.)]`)

// computeDiscoverCacheDir takes the parentDir and the host and comes up with a "usually non-colliding" name.
func computeDiscoverCacheDir(parentDir, host string) string {
	// strip the optional scheme from host if its there:
	schemelessHost := strings.Replace(strings.Replace(host, "https://", "", 1), "http://", "", 1)
	// now do a simple collapse of non-AZ09 characters.  Collisions are possible but unlikely.  Even if we do collide the problem is short lived
	safeHost := overlyCautiousIllegalFileCharacters.ReplaceAllString(schemelessHost, "_")
	return filepath.Join(parentDir, safeHost)
}
