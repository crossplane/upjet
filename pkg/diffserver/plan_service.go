// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"context"
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
	fmtErrEmptyGroupName         = "empty API group name for GVK %q"
	fmtErrNotTerraformed         = "the API type %q is not a Terraformed resource"
	fmtErrResourceConfigNotFound = "no resource configuration for the API type %q is registered in provider configurations"

	fmtErrGVKMismatch = "the GVKs of both the desired and the actual resources must match, desired has %q, actual has %q"
)

// PlanService implements the upjet.diff.v1alpha1.PlanService gRPC service.
type PlanService struct {
	diffv1alpha1.UnimplementedPlanServiceServer

	scheme                 *runtime.Scheme
	decoder                runtime.Decoder
	log                    logging.Logger
	setupFn                terraform.SetupFn
	providerConfigurations []*config.Provider
}

// Plan computes a diff between the desired and the actual resources supplied in
// the request.
func (s *PlanService) Plan(ctx context.Context, req *diffv1alpha1.PlanRequest) (*diffv1alpha1.PlanResponse, error) {
	if req.GetDesiredResource() == nil {
		return nil, status.Error(codes.InvalidArgument, errNoDesiredResource)
	}
	desired, desiredGVK, err := s.managed(req.GetDesiredResource())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, errors.Wrap(err, errDesiredResource).Error())
	}

	// The actual resource is unset for a resource that does not exist yet,
	// which plans as a create.
	actual, actualGVK, err := s.managed(req.GetLiveResource())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, errors.Wrap(err, errActualResource).Error())
	}

	s.log.Debug("Received a plan request",
		"desired-gvk", desiredGVK.String(), "desired-name", desired.GetName(),
		"actual-gvk", actualGVK.String())

	// We currently require that the whole GVKs of desired and actual states
	// match. Version skews are not allowed.
	// TODO: Relax this constraint by incorporating CRD API conversion chains
	// in the in-memory client.
	if actualGVK != desiredGVK {
		return nil, status.Error(codes.InvalidArgument, errors.Errorf(fmtErrGVKMismatch, desiredGVK.String(), actualGVK.String()).Error())
	}

	kc, err := s.inMemoryClient(req)
	if err != nil {
		return nil, status.Error(codes.Internal, errors.Wrap(err, errInMemoryClient).Error())
	}

	cfg, err := s.getResourceConfiguration(actual)
	if err != nil {
		return nil, status.Error(codes.NotFound, errors.Wrap(err, errResourceConfigNotFound).Error())
	}

	if err := s.diffTerraformPluginSDK(ctx, kc, cfg, desired, actual); err != nil {
		if IsDiffComputationNotSupportedError(err) {
			st := status.New(codes.FailedPrecondition, errors.Wrap(err, errDiffPluginSDKv2).Error())
			ds, dErr := st.WithDetails(&errdetails.PreconditionFailure{
				Violations: []*errdetails.PreconditionFailure_Violation{
					{
						Type:        violationDiffComputationNotSupported,
						Subject:     desiredGVK.String(),
						Description: err.Error(),
					},
				},
			})
			if dErr != nil {
				// never let detail marshaling mask the original failure.
				s.log.Debug("cannot attach status details", "error", dErr)
				return nil, st.Err()
			}
			return nil, ds.Err()
		}

		return nil, status.Error(codes.Internal, errors.Wrap(err, errDiffPluginSDKv2).Error())
	}

	return &diffv1alpha1.PlanResponse{}, nil
}

// managed deserializes the given resource into an MR type
// registered for its apiVersion and kind, also returning its GVK.
// Both the returned MR and the GVK are zero if the resource is unset.
func (s *PlanService) managed(st *structpb.Struct) (xpresource.Managed, schema.GroupVersionKind, error) {
	o, gvk, err := s.object(st)
	if err != nil {
		return nil, schema.GroupVersionKind{}, err
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

func (s *PlanService) getResourceConfiguration(m xpresource.Managed) (*config.Resource, error) {
	gvk := m.GetObjectKind().GroupVersionKind()
	parts := strings.SplitN(gvk.Group, ".", 2)
	if len(parts) != 2 {
		return nil, errors.Errorf(fmtErrEmptyGroupName, gvk.String())
	}

	tr, ok := m.(resource.Terraformed)
	if !ok {
		return nil, errors.Errorf(fmtErrNotTerraformed, gvk.String())
	}
	tfName := tr.GetTerraformResourceType()
	for _, pc := range s.providerConfigurations {
		if pc.RootGroup != parts[1] {
			continue
		}
		if c, ok := pc.Resources[tfName]; ok {
			return c, nil
		}
	}
	return nil, errors.Errorf(fmtErrResourceConfigNotFound, gvk.String())
}
