// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package plan

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/diffserver/internal"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

const (
	errNoDesiredResource      = "the desired resource is not set in the plan request"
	errMarshalStruct          = "cannot marshal the resource as JSON"
	errDecode                 = "cannot decode the resource into a registered API type"
	errDesiredResource        = "cannot read the desired resource"
	errActualResource         = "cannot read the actual resource"
	errInMemoryClient         = "cannot initialize the in-memory Kubernetes API client"
	errResourceConfigNotFound = "cannot find the resource configuration"

	fmtErrNotManaged      = "the API type %q registered for the resource is not a managed resource"
	fmtErrNotObject       = "the API type %q registered for the resource is not a metav1.Object"
	fmtErrConvertProtoBuf = "cannot convert %s unstructured object from protobuf"

	violationDiffComputationNotSupported = "DIFF_COMPUTATION_NOT_SUPPORTED"

	errCLIDiffNotImplemented       = "diff support for Terraform CLI resources is not implemented yet"
	errFrameworkDiffNotImplemented = "diff support for Terraform Plugin Framework resources is not implemented yet"

	fmtErrEmptyGroupName         = "empty API group name for GVK %q"
	fmtErrNotTerraformed         = "the API type %q is not a Terraformed resource"
	fmtErrResourceConfigNotFound = "no resource configuration for the API type %q is registered in provider configurations"
	fmtErrResourceTypeMatch      = "cannot match resource name %q to regex %q"
	fmtErrGVKMismatch            = "the GVKs of both the desired and the actual resources must match, desired has %q, actual has %q"
	fmtErrAPIGroupNotServed      = "the API group %q is not served by this provider package, which serves: %s"
)

// PlanService implements the upjet.diff.v1alpha1.PlanService gRPC service.
type PlanService struct {
	diffv1alpha1.UnimplementedPlanServiceServer

	scheme                 *runtime.Scheme
	decoder                runtime.Decoder
	log                    logging.Logger
	setupFn                terraform.SetupFn
	providerConfigurations []*config.Provider
	// apiGroups holds the resource API groups this service serves, keyed by
	// the first label of a resource's API group. It's empty when the service
	// serves every group.
	apiGroups map[string]struct{}
}

func NewPlanService(scheme *runtime.Scheme, decoder runtime.Decoder, log logging.Logger, setupFn terraform.SetupFn, apiGroups []string, providerConfigurations ...*config.Provider) *PlanService {
	groups := make(map[string]struct{}, len(apiGroups))
	for _, g := range apiGroups {
		groups[g] = struct{}{}
	}
	return &PlanService{
		scheme:                 scheme,
		decoder:                decoder,
		log:                    log,
		setupFn:                setupFn,
		providerConfigurations: providerConfigurations,
		apiGroups:              groups,
	}
}

// Plan computes a diff between the desired and the actual resources supplied in
// the request.
func (s *PlanService) Plan(ctx context.Context, req *diffv1alpha1.PlanRequest) (*diffv1alpha1.PlanResponse, error) { //nolint:gocyclo // easier to follow as a unit
	if req.GetDesiredResource() == nil {
		return nil, status.Error(codes.InvalidArgument, errNoDesiredResource)
	}
	desired, desiredGVK, err := s.managed(req.GetDesiredResource())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, errors.Wrap(err, errDesiredResource).Error())
	}

	// The actual resource is unset for a resource that does not exist yet,
	// which plans as a create.
	actual, actualGVK, err := s.managed(req.GetActualResource())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, errors.Wrap(err, errActualResource).Error())
	}

	s.log.Debug("Received a plan request",
		"desired-gvk", desiredGVK.String(), "desired-name", desired.GetName(),
		"actual-gvk", actualGVK.String())

	// A provider package's scheme and provider configuration both cover every
	// API group the provider has, so decoding the resource says nothing about
	// whether this package is the one that reconciles it. Decline the request
	// instead of answering it from a configuration whose controllers do not
	// run here, so that a misrouted request fails rather than misleads.
	if !s.servesAPIGroup(desiredGVK) {
		return nil, status.Error(codes.NotFound, errors.Errorf(fmtErrAPIGroupNotServed, desiredGVK.Group, strings.Join(s.servedAPIGroups(), ", ")).Error())
	}

	// We currently require that the whole GVKs of desired and actual states
	// match. Version skews are not allowed.
	// TODO: Relax this constraint by incorporating CRD API conversion chains
	// in the in-memory client.
	if actual != nil && actualGVK != desiredGVK {
		return nil, status.Error(codes.InvalidArgument, errors.Errorf(fmtErrGVKMismatch, desiredGVK.String(), actualGVK.String()).Error())
	}

	kc, err := s.inMemoryClient(req)
	if err != nil {
		return nil, status.Error(codes.Internal, errors.Wrap(err, errInMemoryClient).Error())
	}

	cfg, t, err := s.getResourceConfiguration(desired)
	if err != nil {
		return nil, status.Error(codes.NotFound, errors.Wrap(err, errResourceConfigNotFound).Error())
	}

	var errorMsg string
	var rsp *diffv1alpha1.PlanResponse
	switch t {
	case config.ResourceTypeTerraformCLI:
		return nil, s.preconditionFailure(nil, errCLIDiffNotImplemented, desiredGVK)

	case config.ResourceTypeTerraformFramework:
		return nil, s.preconditionFailure(nil, errFrameworkDiffNotImplemented, desiredGVK)

	case config.ResourceTypeTerraformSDK:
		errorMsg = errDiffPluginSDKv2
		rsp, err = s.diffTerraformPluginSDK(ctx, kc, cfg, desired, actual)

	case config.ResourceTypeUnknown:
		fallthrough
	default:
		return nil, s.preconditionFailure(nil, "", desiredGVK)
	}

	// err from the diff implementation above.
	if err != nil {
		if IsDiffComputationNotSupportedError(err) {
			return nil, s.preconditionFailure(err, errorMsg, desiredGVK)
		}
		return nil, status.Error(codes.Internal, errors.Wrap(err, errorMsg).Error())
	}
	return rsp, nil
}

func (s *PlanService) preconditionFailure(err error, msg string, desiredGVK schema.GroupVersionKind) error {
	var sErr error
	msg = strings.TrimSpace(msg)
	if len(msg) > 0 {
		if err != nil {
			sErr = errors.Wrap(err, msg)
		} else {
			sErr = errors.New(msg)
		}
	} else {
		sErr = err
	}

	if sErr == nil {
		sErr = ErrDiffComputationNotSupported
	}
	st := status.New(codes.FailedPrecondition, sErr.Error())
	ds, dErr := st.WithDetails(&errdetails.PreconditionFailure{
		Violations: []*errdetails.PreconditionFailure_Violation{
			{
				Type:        violationDiffComputationNotSupported,
				Subject:     desiredGVK.String(),
				Description: sErr.Error(),
			},
		},
	})
	if dErr != nil {
		// never let detail marshaling mask the original failure.
		s.log.Debug("cannot attach status details", "error", dErr)
		return st.Err()
	}
	return ds.Err()
}

// managed deserializes the given resource into an MR type
// registered for its apiVersion and kind, also returning its GVK.
// Both the returned MR and the GVK are zero if the resource is unset.
func (s *PlanService) managed(st *structpb.Struct) (xpresource.Managed, schema.GroupVersionKind, error) {
	o, gvk, err := s.object(st)
	if err != nil {
		return nil, schema.GroupVersionKind{}, err
	}
	if o == nil {
		return nil, schema.GroupVersionKind{}, nil
	}
	mg, ok := o.(xpresource.Managed)
	if !ok {
		return nil, schema.GroupVersionKind{}, errors.Errorf(fmtErrNotManaged, gvk.String())
	}
	return mg, gvk, nil
}

func (s *PlanService) object(st *structpb.Struct) (metav1.Object, schema.GroupVersionKind, error) {
	if st == nil {
		return nil, schema.GroupVersionKind{}, nil
	}
	// structpb.Struct marshals itself as the JSON object it represents, which
	// is the resource's manifest.
	buff, err := st.MarshalJSON()
	if err != nil {
		return nil, schema.GroupVersionKind{}, errors.Wrap(err, errMarshalStruct)
	}
	o, gvk, err := s.decoder.Decode(buff, nil, nil)
	if err != nil {
		return nil, schema.GroupVersionKind{}, errors.Wrap(err, errDecode)
	}
	mo, ok := o.(metav1.Object)
	if !ok {
		return nil, schema.GroupVersionKind{}, errors.Errorf(fmtErrNotObject, gvk.String())
	}
	return mo, *gvk, nil
}

func (s *PlanService) inMemoryClient(req *diffv1alpha1.PlanRequest) (kclient.Client, error) {
	pStore := req.GetKubernetesObjectStore()
	store := make([]kunstructured.Unstructured, 0, len(pStore))
	for _, st := range pStore {
		o := &kunstructured.Unstructured{}
		if err := internal.FromStruct(o, st); err != nil {
			return nil, errors.Wrapf(err, fmtErrConvertProtoBuf, "ProviderConfig")
		}
		store = append(store, *o)
	}

	kc := internal.NewInMemoryClient(s.scheme, store...)
	return kc, nil
}

// servesAPIGroup reports whether this service serves the given resource's API
// group. The group is named by its first label, the same way
// getResourceConfiguration splits it, so that one name covers a resource's
// cluster-scoped and namespaced API groups alike. A service that declares no
// groups serves all of them.
func (s *PlanService) servesAPIGroup(gvk schema.GroupVersionKind) bool {
	if len(s.apiGroups) == 0 {
		return true
	}
	_, ok := s.apiGroups[strings.SplitN(gvk.Group, ".", 2)[0]]
	return ok
}

// servedAPIGroups returns the API groups this service serves, sorted, for
// reporting them in an error.
func (s *PlanService) servedAPIGroups() []string {
	groups := make([]string, 0, len(s.apiGroups))
	for g := range s.apiGroups {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups
}

func (s *PlanService) getResourceConfiguration(m xpresource.Managed) (*config.Resource, config.ResourceType, error) {
	gvk := m.GetObjectKind().GroupVersionKind()
	parts := strings.SplitN(gvk.Group, ".", 2)
	if len(parts) != 2 {
		return nil, config.ResourceTypeUnknown, errors.Errorf(fmtErrEmptyGroupName, gvk.String())
	}

	tr, ok := m.(resource.Terraformed)
	if !ok {
		return nil, config.ResourceTypeUnknown, errors.Errorf(fmtErrNotTerraformed, gvk.String())
	}
	tfName := tr.GetTerraformResourceType()
	for _, pc := range s.providerConfigurations {
		if pc.RootGroup != parts[1] {
			continue
		}
		if c, ok := pc.Resources[tfName]; ok {
			t, err := resourceType(pc, c.Name)
			if err != nil {
				return nil, config.ResourceTypeUnknown, err
			}
			if t == config.ResourceTypeUnknown {
				// Treat unknown resource type as not found.
				return nil, config.ResourceTypeUnknown, errors.Errorf(fmtErrResourceConfigNotFound, gvk.String())
			}
			// We always return a valid resource config with a known resource type.
			return c, t, nil
		}
	}
	return nil, config.ResourceTypeUnknown, errors.Errorf(fmtErrResourceConfigNotFound, gvk.String())
}

func resourceType(pc *config.Provider, name string) (config.ResourceType, error) {
	if pc == nil {
		return config.ResourceTypeUnknown, nil
	}

	// Search in TF CLI resources.
	ok, err := searchInRegexList(pc.IncludeList, name)
	if err != nil {
		return config.ResourceTypeUnknown, errors.Wrap(err, "cannot search resource in Terraform CLI resources list")
	}
	if ok {
		return config.ResourceTypeTerraformCLI, nil
	}

	// Search in TF plugin SDK resources.
	ok, err = searchInRegexList(pc.TerraformPluginSDKIncludeList, name)
	if err != nil {
		return config.ResourceTypeUnknown, errors.Wrap(err, "cannot search resource in Terraform Plugin SDK resources list")
	}
	if ok {
		return config.ResourceTypeTerraformSDK, nil
	}

	// Search in TF plugin framework resources.
	ok, err = searchInRegexList(pc.TerraformPluginFrameworkIncludeList, name)
	if err != nil {
		return config.ResourceTypeUnknown, errors.Wrap(err, "cannot search resource in Terraform Plugin Framework resources list")
	}
	if ok {
		return config.ResourceTypeTerraformFramework, nil
	}
	return config.ResourceTypeUnknown, nil
}

func searchInRegexList(l []string, name string) (bool, error) {
	for _, r := range l {
		ok, err := regexp.MatchString(r, name)
		if err != nil {
			return false, errors.Errorf(fmtErrResourceTypeMatch, name, r)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}
