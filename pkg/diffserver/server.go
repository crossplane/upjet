// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

// Package diffserver implements the provider's diff gRPC services.
package diffserver

import (
	"context"
	"net"
	"os"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	tf "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/diffserver/internal"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

const (
	errListen = "cannot listen on %s address %q"
	errServe  = "cannot serve the diff gRPC services"

	errNoDesiredResource = "the desired resource is not set in the plan request"
	errMarshalStruct     = "cannot marshal the resource as JSON"
	errDecode            = "cannot decode the resource into a registered API type"
	errDesiredResource   = "cannot read the desired resource"
	errLiveResource      = "cannot read the live resource"
	errInMemoryClient    = "cannot initialize the in-memory Kubernetes API client"

	errDiffPluginSDKv2           = "cannot compute diff for a Terraform plugin SDKv2 resource"
	errConnectPluginSDKv2        = "cannot connect for Terraform plugin SDKv2 resource"
	errObservePluginSDKv2        = "cannot observe Terraform plugin SDKv2 resource"
	errReconstructTerraformState = "cannot reconstruct Terraform resource state"
	errGetInstanceDiff           = "cannot read Terraform plugin SDKv2 resource instance diff"

	fmtErrNotManaged             = "the API type %q registered for the resource is not a managed resource"
	fmtErrNotObject              = "the API type %q registered for the resource is not a metav1 Object"
	fmtErrNotTerraformed         = "the API type %q is not a Terraformed resource"
	fmtErrConvertProtoBuf        = "cannot convert %s unstructured object from protobuf"
	fmtErrEmptyGroupName         = "empty API group name for GVK %q"
	fmtErrResourceConfigNotFound = "cannot find the resource configuration for the API type %q"

	violationDiffComputationNotSupported = "DIFF_COMPUTATION_NOT_SUPPORTED"
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

// Plan computes a diff between the desired and the live resources supplied in
// the request.
func (s *PlanService) Plan(ctx context.Context, req *diffv1alpha1.PlanRequest) (*diffv1alpha1.PlanResponse, error) {
	if req.GetDesiredResource() == nil {
		return nil, status.Error(codes.InvalidArgument, errNoDesiredResource)
	}
	desired, desiredGVK, err := s.managed(req.GetDesiredResource())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, errors.Wrap(err, errDesiredResource).Error())
	}

	// The live resource is unset for a resource that does not exist yet,
	// which plans as a create.
	live, liveGVK, err := s.managed(req.GetLiveResource())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, errors.Wrap(err, errLiveResource).Error())
	}

	s.log.Debug("Received a plan request",
		"desired-gvk", desiredGVK.String(), "desired-name", desired.GetName(),
		"live-gvk", liveGVK.String())

	kc, err := s.inMemoryClient(req)
	if err != nil {
		return nil, status.Error(codes.Internal, errors.Wrap(err, errInMemoryClient).Error())
	}

	if err := s.diffTerraformPluginSDK(ctx, desired, live, kc); err != nil {
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

func (s *PlanService) diffTerraformPluginSDK(ctx context.Context, desired, actual xpresource.Managed, kc kclient.Client) error {
	cfg, err := s.getResourceConfiguration(actual)
	if err != nil {
		return err
	}

	opTracker := controller.NewOperationStore(s.log)
	c := controller.NewTerraformPluginSDKConnector(
		kc, s.setupFn, cfg, opTracker,
		controller.WithTerraformPluginSDKLogger(s.log),
		controller.WithObservationMode(controller.UseLocalState),
	)
	ec, err := c.Connect(ctx, desired)
	if err != nil {
		return errors.Wrap(err, errConnectPluginSDKv2)
	}

	tr, ok := actual.(resource.Terraformed)
	if !ok {
		return errors.Errorf(fmtErrNotTerraformed, actual.GetObjectKind().GroupVersionKind().String())
	}
	opTracker.Tracker(tr).ResetReconstructedTfState()
	if _, _, _, err := c.ReconstructTerraformState(ctx, tr, s.log); err != nil {
		return errors.Wrap(err, errReconstructTerraformState)
	}

	_, err = ec.Observe(ctx, desired)
	if err != nil {
		return errors.Wrap(err, errObservePluginSDKv2)
	}

	diff, err := controller.TerraformPluginSDKInstanceDiff(ec)
	if err != nil {
		return errors.Wrap(err, errGetInstanceDiff)
	}
	filterInstanceDiff(diff)
	return nil
}

// filterInstanceDiff removes the attribute diffs that do not represent a
// meaningful change from d, in place, so that what remains answers the
// question "did anything meaningful change?". Once filtered, d.Empty()
// reports whether the desired resource differs from the live one.
func filterInstanceDiff(d *tf.InstanceDiff) {
	if d == nil {
		return
	}
	for k, a := range d.Attributes {
		if !isMeaningfulChange(a) {
			delete(d.Attributes, k)
		}
	}
}

// isMeaningfulChange reports whether the given attribute diff represents a
// change worth reporting in a plan.
func isMeaningfulChange(a *tf.ResourceAttrDiff) bool {
	switch {
	case a == nil:
		// An attribute without a diff carries no information.
		return false
	case a.RequiresNew:
		// Replacing the external resource is always meaningful, even when the
		// value that triggers the replacement is computed.
		return true
	case a.NewComputed:
		// The new value is unknown until apply. The Terraform provider
		// recomputes such attributes on every plan, AWS' tags_all being the
		// canonical example, so they do not reflect a change the user made.
		// Their New is an empty placeholder, which would also make comparing
		// it against Old meaningless.
		return false
	default:
		// Everything else, including an attribute being removed, is a change
		// exactly when its old and new values differ. Removing an attribute
		// that is already empty is therefore correctly reported as no change.
		return a.Old != a.New
	}
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

type Server struct {
	providerConfigurations []*config.Provider
	log                    logging.Logger
	setupFn                terraform.SetupFn
}

type ServerOption func(*Server)

func NewServer(opt ...ServerOption) *Server {
	s := &Server{
		log: logging.NewNopLogger(),
	}

	for _, o := range opt {
		o(s)
	}
	return s
}

func WithLogger(l logging.Logger) ServerOption {
	return func(s *Server) {
		s.log = l
	}
}

func WithProviderConfigurations(configs ...*config.Provider) ServerOption {
	return func(s *Server) {
		s.providerConfigurations = configs
	}
}

func WithTerraformSetupFn(setupFn terraform.SetupFn) ServerOption {
	return func(s *Server) {
		s.setupFn = setupFn
	}
}

// Serve starts a gRPC server serving the diff services on the given network
// and address, and blocks until ctx is done or the server fails. A "unix"
// network address is removed before it's bound so that a socket left behind by
// a previous run does not prevent the server from starting.
func (s *Server) Serve(ctx context.Context, network, address string, scheme *runtime.Scheme) error {
	if network == "unix" {
		if err := os.Remove(address); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "cannot remove the existing socket at %q", address)
		}
	}

	var lc net.ListenConfig
	l, err := lc.Listen(ctx, network, address)
	if err != nil {
		return errors.Wrapf(err, errListen, network, address)
	}

	grpcServer := grpc.NewServer()
	diffv1alpha1.RegisterPlanServiceServer(grpcServer,
		&PlanService{
			scheme:                 scheme,
			decoder:                serializer.NewCodecFactory(scheme).UniversalDeserializer(),
			log:                    s.log,
			setupFn:                s.setupFn,
			providerConfigurations: s.providerConfigurations,
		},
	)
	// Reflection lets clients such as grpcurl discover the served services.
	reflection.Register(grpcServer)

	// GracefulStop closes the listener and waits for the in-flight RPCs to
	// complete, which also unblocks the Serve call below.
	go func() {
		<-ctx.Done()
		s.log.Info("Stopping the diff gRPC server")
		grpcServer.GracefulStop()
	}()

	s.log.Info("Starting the diff gRPC server", "network", network, "address", address)
	return errors.Wrap(grpcServer.Serve(l), errServe)
}
