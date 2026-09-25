// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

// Package diffserver implements the provider's diff gRPC services.
package diffserver

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime/debug"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

const (
	errListen = "cannot listen on %s address %q"
	errServe  = "cannot serve the diff gRPC services"

	errNoDesiredResource      = "the desired resource is not set in the plan request"
	errMarshalStruct          = "cannot marshal the resource as JSON"
	errDecode                 = "cannot decode the resource into a registered API type"
	errDesiredResource        = "cannot read the desired resource"
	errActualResource         = "cannot read the actual resource"
	errInMemoryClient         = "cannot initialize the in-memory Kubernetes API client"
	errResourceConfigNotFound = "cannot find the resource configuration"

	fmtErrPanic           = "the diff gRPC server recovered from a panic: %v"
	fmtErrNotManaged      = "the API type %q registered for the resource is not a managed resource"
	fmtErrNotObject       = "the API type %q registered for the resource is not a metav1.Object"
	fmtErrConvertProtoBuf = "cannot convert %s unstructured object from protobuf"

	violationDiffComputationNotSupported = "DIFF_COMPUTATION_NOT_SUPPORTED"
)

// Server represents gRPC Server that supports unix and TCP networks for
// serving and currently exposes the gRPC PlanService.
// Please see also: proto/diff/v1alpha1
type Server struct {
	providerConfigurations []*config.Provider
	log                    logging.Logger
	setupFn                terraform.SetupFn
}

// ServerOption represents a configuration option for a Server instance.
type ServerOption func(*Server)

// NewServer initializes and returns a new gRPC Server with
// the specified options.
func NewServer(opt ...ServerOption) *Server {
	s := &Server{
		log: logging.NewNopLogger(),
	}

	for _, o := range opt {
		o(s)
	}
	return s
}

// WithLogger configures a logger to be used with the Server.
func WithLogger(l logging.Logger) ServerOption {
	return func(s *Server) {
		s.log = l
	}
}

// WithProviderConfigurations sets the specified Provider configurations to be
// used by a Server. Provider configurations are used when calling external
// connectors and clients.
func WithProviderConfigurations(configs ...*config.Provider) ServerOption {
	return func(s *Server) {
		s.providerConfigurations = configs
	}
}

// WithTerraformSetupFn configures the Terraform setup function to be used by
// a Server when calling external connector's Connect method.
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

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(recoverUnary(s.log)),
		grpc.ChainStreamInterceptor(recoverStream(s.log)),
	)
	diffv1alpha1.RegisterPlanServiceServer(grpcServer,
		&PlanService{
			scheme:                 scheme,
			decoder:                serializer.NewCodecFactory(scheme).UniversalDeserializer(),
			log:                    s.log,
			setupFn:                s.setupFn,
			providerConfigurations: s.providerConfigurations,
		},
	)
	// Reflection lets gRPC clients to discover the available services.
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

func recoverUnary(log logging.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (rsp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(log, info.FullMethod, r)
				err = status.Errorf(codes.Internal, fmtErrPanic, r)
			}
		}()
		return handler(ctx, req)
	}
}

func recoverStream(log logging.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(log, info.FullMethod, r)
				err = status.Errorf(codes.Internal, fmtErrPanic, r)
			}
		}()
		return handler(srv, ss)
	}
}

func logPanic(log logging.Logger, method string, r any) {
	log.Info("Recovered from a panic while serving an RPC",
		"method", method, "panic", fmt.Sprintf("%v", r), "stack", string(debug.Stack()))
}
