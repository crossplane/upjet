package migration

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	errCategoryGetFmt       = "failed to get resources of category %q"
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
	restConfig            *rest.Config
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
	ks.restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create rest config object")
	}
	ks.restConfig.ContentConfig = resource.UnstructuredPlusDefaultContentConfig()

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
	if err := ks.getGVKResources(ks.registry.GetCrossplanePackageGVKs(), CategoryCrossplanePackage); err != nil {
		return errors.Wrap(err, "cannot get Crossplane packages")
	}
	return errors.Wrap(ks.getGVKResources(ks.registry.GetManagedResourceGVKs(), CategoryManaged), "cannot get managed resources")
}

func (ks *KubernetesSource) getMappingFor(gr schema.GroupResource) (*meta.RESTMapping, error) {
	r := fmt.Sprintf("%s.%s", gr.Resource, gr.Group)
	fullySpecifiedGVR, groupResource := schema.ParseResourceArg(r)
	gvk := schema.GroupVersionKind{}
	if fullySpecifiedGVR != nil {
		gvk, _ = ks.restMapper.KindFor(*fullySpecifiedGVR)
	}
	if gvk.Empty() {
		gvk, _ = ks.restMapper.KindFor(groupResource.WithVersion(""))
	}
	if !gvk.Empty() {
		return ks.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	}
	fullySpecifiedGVK, groupKind := schema.ParseKindArg(r)
	if fullySpecifiedGVK == nil {
		gvk := groupKind.WithVersion("")
		fullySpecifiedGVK = &gvk
	}

	if !fullySpecifiedGVK.Empty() {
		if mapping, err := ks.restMapper.RESTMapping(fullySpecifiedGVK.GroupKind(), fullySpecifiedGVK.Version); err == nil {
			return mapping, nil
		}
	}

	mapping, err := ks.restMapper.RESTMapping(groupKind, gvk.Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, errors.Errorf("the server doesn't have a resource type %q", groupResource.Resource)
		}
		return nil, err
	}
	return mapping, nil
}

// parts of this implement are taken from the implementation of
// the "kubectl get" command:
// https://github.com/kubernetes/kubernetes/tree/master/staging/src/k8s.io/kubectl/pkg/cmd/get
func (ks *KubernetesSource) getCategoryResources(c Category) error {
	if ks.restConfig == nil {
		return errors.New("rest.Config not initialized")
	}
	grs, _ := ks.categoryExpander.Expand(c.String())
	for _, gr := range grs {
		mapping, err := ks.getMappingFor(gr)
		if err != nil {
			return errors.Wrapf(err, errCategoryGetFmt, c.String())
		}
		gv := mapping.GroupVersionKind.GroupVersion()
		ks.restConfig.GroupVersion = &gv
		if len(gv.Group) == 0 {
			ks.restConfig.APIPath = "/api"
		} else {
			ks.restConfig.APIPath = "/apis"
		}
		client, err := rest.RESTClientFor(ks.restConfig)
		if err != nil {
			return errors.Wrapf(err, errCategoryGetFmt, c.String())
		}
		helper := resource.NewHelper(client, mapping)
		list, err := helper.List("", mapping.GroupVersionKind.GroupVersion().String(), &metav1.ListOptions{})
		if err != nil {
			return errors.Wrapf(err, errCategoryGetFmt, c.String())
		}
		ul, ok := list.(*unstructured.UnstructuredList)
		if !ok {
			return errors.New("expecting list to be of type *unstructured.UnstructuredList")
		}
		for _, u := range ul.Items {
			ks.items = append(ks.items, UnstructuredWithMetadata{
				Object: u,
				Metadata: Metadata{
					Path:     string(u.GetUID()),
					Category: c,
				},
			})
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
