// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package diffserver

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kschema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	diffv1alpha1 "github.com/crossplane/upjet/v2/proto/diff/v1alpha1"
)

func testPlanService(t *testing.T) *PlanService {
	t.Helper()
	sch := runtime.NewScheme()
	if err := corev1.AddToScheme(sch); err != nil {
		t.Fatalf("cannot build the test scheme: %v", err)
	}
	return &PlanService{scheme: sch, decoder: serializer.NewCodecFactory(sch).UniversalDeserializer()}
}

func mustStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	st, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("cannot build the test struct: %v", err)
	}
	return st
}

func TestObject(t *testing.T) {
	cases := map[string]struct {
		reason  string
		st      *structpb.Struct
		wantGVK kschema.GroupVersionKind
		wantNil bool
		wantErr bool
	}{
		"Unset": {
			reason:  "An unset resource is not an error: it is how the protocol expresses a resource that does not exist yet.",
			st:      nil,
			wantNil: true,
		},
		"UnregisteredKind": {
			reason:  "A kind the scheme does not know cannot be decoded.",
			st:      mustStruct(t, map[string]any{"apiVersion": "example.crossplane.io/v1", "kind": "Nonexistent"}),
			wantNil: true,
			wantErr: true,
		},
		"MissingKind": {
			reason:  "A manifest without a kind cannot be decoded.",
			st:      mustStruct(t, map[string]any{"metadata": map[string]any{"name": "example"}}),
			wantNil: true,
			wantErr: true,
		},
		"Decoded": {
			reason:  "A registered kind decodes, and its GVK is returned alongside it.",
			st:      mustStruct(t, map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "example"}}),
			wantGVK: kschema.GroupVersionKind{Version: "v1", Kind: "Secret"},
		},
	}

	s := testPlanService(t)
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o, gvk, err := s.object(tc.st)
			if tc.wantErr && err == nil {
				t.Fatalf("\n%s\nobject(...): want an error, got none", tc.reason)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("\n%s\nobject(...): want no error, got %v", tc.reason, err)
			}
			if tc.wantNil && o != nil {
				t.Errorf("\n%s\nobject(...): want no object, got %v", tc.reason, o)
			}
			if !tc.wantNil && o == nil {
				t.Errorf("\n%s\nobject(...): want an object, got none", tc.reason)
			}
			if diff := cmp.Diff(tc.wantGVK, gvk); diff != "" {
				t.Errorf("\n%s\nobject(...): -want gvk, +got gvk:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestManaged(t *testing.T) {
	cases := map[string]struct {
		reason  string
		st      *structpb.Struct
		wantNil bool
		wantErr bool
	}{
		"Unset": {
			reason:  "An unset resource yields no managed resource and no error.",
			st:      nil,
			wantNil: true,
		},
		"NotAManagedResource": {
			reason:  "A registered type that is not a managed resource is rejected rather than used.",
			st:      mustStruct(t, map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "example"}}),
			wantNil: true,
			wantErr: true,
		},
		"Undecodable": {
			reason:  "A decode failure is propagated.",
			st:      mustStruct(t, map[string]any{"apiVersion": "example.crossplane.io/v1", "kind": "Nonexistent"}),
			wantNil: true,
			wantErr: true,
		},
	}

	s := testPlanService(t)
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mg, _, err := s.managed(tc.st)
			if tc.wantErr && err == nil {
				t.Fatalf("\n%s\nmanaged(...): want an error, got none", tc.reason)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("\n%s\nmanaged(...): want no error, got %v", tc.reason, err)
			}
			if tc.wantNil && mg != nil {
				t.Errorf("\n%s\nmanaged(...): want no managed resource, got %v", tc.reason, mg)
			}
		})
	}
}

func TestInMemoryClient(t *testing.T) {
	s := testPlanService(t)

	cases := map[string]struct {
		reason string
		req    *diffv1alpha1.PlanRequest
		lookup kclient.ObjectKey
		wantOK bool
	}{
		"EmptyStore": {
			reason: "A request with no object store yields an empty client rather than an error.",
			req:    &diffv1alpha1.PlanRequest{},
			lookup: kclient.ObjectKey{Namespace: "upbound-system", Name: "example-creds"},
		},
		"StoredObjectIsReadable": {
			reason: "An object supplied in the request can be read back from the client.",
			req: &diffv1alpha1.PlanRequest{KubernetesObjectStore: []*structpb.Struct{
				mustStruct(t, map[string]any{
					"apiVersion": "v1", "kind": "Secret",
					"metadata": map[string]any{"name": "example-creds", "namespace": "upbound-system"},
				}),
			}},
			lookup: kclient.ObjectKey{Namespace: "upbound-system", Name: "example-creds"},
			wantOK: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kc, err := s.inMemoryClient(tc.req)
			if err != nil {
				t.Fatalf("\n%s\ninMemoryClient(...): unexpected error: %v", tc.reason, err)
			}
			sec := &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}}
			err = kc.Get(context.Background(), tc.lookup, sec)
			if tc.wantOK && err != nil {
				t.Errorf("\n%s\ninMemoryClient(...): want the object to be readable, got %v", tc.reason, err)
			}
			if !tc.wantOK && err == nil {
				t.Errorf("\n%s\ninMemoryClient(...): want the object to be absent, got it", tc.reason)
			}
		})
	}
}
