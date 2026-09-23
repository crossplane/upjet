// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

// Package diffserver implements the provider's diff gRPC services.
package diffserver

import (
	"context"
	"net"
	"os"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

const (
	errListen = "cannot listen on %s address %q"
	errServe  = "cannot serve the diff gRPC services"

	errNoDesiredResource = "the desired resource is not set in the plan request"
	errMarshalStruct     = "cannot marshal the resource as JSON"
	errDecode            = "cannot decode the resource into a registered API type"
	errNotManaged        = "the API type %q registered for the resource is not a managed resource"
	errNotObject         = "the API type %q registered for the resource is not a metav1 Object"
	errDesiredResource   = "cannot read the desired resource"
	errLiveResource      = "cannot read the live resource"
	errProviderConfig    = "cannot read the ProviderConfig"
)

// PlanService implements the upjet.diff.v1alpha1.PlanService gRPC service.
type PlanService struct {
	diffv1alpha1.UnimplementedPlanServiceServer

	decoder runtime.Decoder
	log     logging.Logger
}

// NewPlanService returns a new PlanService that deserializes the resources it
// receives using the API types registered with the given scheme.
func NewPlanService(scheme *runtime.Scheme, log logging.Logger) *PlanService {
	return &PlanService{
		decoder: serializer.NewCodecFactory(scheme).UniversalDeserializer(),
		log:     log,
	}
}

// Plan computes a diff between the desired and the live resources supplied in
// the request.
func (s *PlanService) Plan(_ context.Context, req *diffv1alpha1.PlanRequest) (*diffv1alpha1.PlanResponse, error) {
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

	pc, pcGVK, err := s.object(req.GetProviderConfig())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, errors.Wrap(err, errProviderConfig).Error())
	}

	s.log.Debug("Received a plan request", "desired-gvk", desiredGVK.String(),
		"desired-name", desired.GetName(), "live-gvk", liveGVK.String(),
		"provider-config-name", pc.GetName(), "provider-config-gvk", pcGVK.String(),
		"exists", live != nil)
	return &diffv1alpha1.PlanResponse{}, nil
}

// managed deserializes the given resource into an MR type
// registered for its apiVersion and kind, also returning its GVK.
// Both the returned MR and the GVK are zero if the resource is unset.
func (s *PlanService) managed(st *structpb.Struct) (resource.Managed, schema.GroupVersionKind, error) {
	o, gvk, err := s.object(st)
	if err != nil {
		return nil, schema.GroupVersionKind{}, err
	}
	mg, ok := o.(resource.Managed)
	if !ok {
		return nil, schema.GroupVersionKind{}, errors.Errorf(errNotManaged, gvk.String())
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
		return nil, schema.GroupVersionKind{}, errors.Errorf(errNotObject, gvk.String())
	}
	return mo, *gvk, nil
}

// Serve starts a gRPC server serving the diff services on the given network
// and address, and blocks until ctx is done or the server fails. A "unix"
// network address is removed before it's bound so that a socket left behind by
// a previous run does not prevent the server from starting.
func Serve(ctx context.Context, network, address string, scheme *runtime.Scheme, log logging.Logger) error {
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

	s := grpc.NewServer()
	diffv1alpha1.RegisterPlanServiceServer(s, NewPlanService(scheme, log))
	// Reflection lets clients such as grpcurl discover the served services.
	reflection.Register(s)

	// GracefulStop closes the listener and waits for the in-flight RPCs to
	// complete, which also unblocks the Serve call below.
	go func() {
		<-ctx.Done()
		log.Info("Stopping the diff gRPC server")
		s.GracefulStop()
	}()

	log.Info("Starting the diff gRPC server", "network", network, "address", address)
	return errors.Wrap(s.Serve(l), errServe)
}
