// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"context"
	"strings"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/terraform"
)

func TestRecoverUnary(t *testing.T) {
	cases := map[string]struct {
		reason  string
		handler grpc.UnaryHandler
		wantErr bool
		wantRsp any
	}{
		"PanickingHandler": {
			reason:  "A panicking handler must be turned into an Internal status rather than taking the process down.",
			handler: func(context.Context, any) (any, error) { panic("boom") },
			wantErr: true,
		},
		"PanickingHandlerWithNilPanic": {
			reason:  "A panic with a nil value is still a panic and must be recovered.",
			handler: func(context.Context, any) (any, error) { panic(error(nil)) },
			wantErr: true,
		},
		"HealthyHandler": {
			reason:  "A handler that does not panic must be passed through untouched.",
			handler: func(context.Context, any) (any, error) { return "ok", nil },
			wantRsp: "ok",
		},
		"FailingHandler": {
			reason:  "A handler that returns an error must have it passed through rather than replaced.",
			handler: func(context.Context, any) (any, error) { return nil, errors.New("boom") },
			wantErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			i := recoverUnary(logging.NewNopLogger())
			rsp, err := i(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/upjet.diff.v1alpha1.PlanService/Plan"}, tc.handler)
			if tc.wantErr && err == nil {
				t.Fatalf("\n%s\nrecoverUnary(...): want an error, got none", tc.reason)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("\n%s\nrecoverUnary(...): want no error, got %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.wantRsp, rsp); diff != "" {
				t.Errorf("\n%s\nrecoverUnary(...): -want response, +got response:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestRecoverUnaryStatusCode(t *testing.T) {
	i := recoverUnary(logging.NewNopLogger())
	_, err := i(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/m"}, func(context.Context, any) (any, error) { panic("boom") })

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("recoverUnary(...): want a gRPC status, got %v", err)
	}
	if diff := cmp.Diff(codes.Internal, st.Code()); diff != "" {
		t.Errorf("recoverUnary(...): -want code, +got code:\n%s", diff)
	}
	if !strings.Contains(st.Message(), "boom") {
		t.Errorf("recoverUnary(...): want the panic value in the message, got %q", st.Message())
	}
}

func TestRecoverStream(t *testing.T) {
	i := recoverStream(logging.NewNopLogger())
	err := i(nil, nil, &grpc.StreamServerInfo{FullMethod: "/m"}, func(any, grpc.ServerStream) error { panic("boom") })

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("recoverStream(...): want a gRPC status, got %v", err)
	}
	if diff := cmp.Diff(codes.Internal, st.Code()); diff != "" {
		t.Errorf("recoverStream(...): -want code, +got code:\n%s", diff)
	}
}

func TestNewServer(t *testing.T) {
	pc := []*config.Provider{{RootGroup: "aws.upbound.io"}}
	setupFn := func(context.Context, kclient.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Version: "1.2.3"}, nil
	}

	t.Run("Defaults", func(t *testing.T) {
		s := NewServer()
		if s.log == nil {
			t.Error("NewServer(): want a non-nil logger so that an unconfigured server does not panic when it logs")
		}
		if s.providerConfigurations != nil {
			t.Errorf("NewServer(): want no provider configurations, got %v", s.providerConfigurations)
		}
		if s.setupFn != nil {
			t.Error("NewServer(): want no Terraform setup function")
		}
	})

	t.Run("Options", func(t *testing.T) {
		s := NewServer(
			WithLogger(logging.NewNopLogger()),
			WithProviderConfigurations(pc...),
			WithTerraformSetupFn(setupFn),
		)
		if len(s.providerConfigurations) != 1 || s.providerConfigurations[0] != pc[0] {
			t.Errorf("NewServer(): want the configured provider configurations, got %v", s.providerConfigurations)
		}
		if s.setupFn == nil {
			t.Fatal("NewServer(): want the Terraform setup function to be set")
		}
		ts, err := s.setupFn(context.Background(), nil, nil)
		if err != nil {
			t.Fatalf("NewServer(): the configured setup function returned an error: %v", err)
		}
		if diff := cmp.Diff("1.2.3", ts.Version); diff != "" {
			t.Errorf("NewServer(): -want the configured setup function, +got:\n%s", diff)
		}
	})
}
