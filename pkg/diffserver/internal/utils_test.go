// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

// Copied from
// https://github.com/crossplane/crossplane/blob/b56f55c021a195ceb301e2ff6a937db014887e43/internal/xfn/utils_test.go

package internal

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/unstructured/composite"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestAsStruct(t *testing.T) {
	type want struct {
		s   *structpb.Struct
		err error
	}

	cases := map[string]struct {
		reason string
		o      runtime.Object
		want   want
	}{
		"Unstructured": {
			reason: "It should be possible to convert a Kubernetes unstructured.Unstructured to a struct",
			o: &kunstructured.Unstructured{Object: map[string]any{
				"apiVersion": "example.org/v1",
				"kind":       "Test",
			}},
			want: want{
				s: MustStruct(map[string]any{
					"apiVersion": "example.org/v1",
					"kind":       "Test",
				}),
			},
		},
		"Composite": {
			reason: "It should be possible to convert a Crossplane composite.Unstructured to a struct",
			o: composite.New(composite.WithGroupVersionKind(schema.GroupVersionKind{
				Group:   "example.org",
				Version: "v1",
				Kind:    "Test",
			})),
			want: want{
				s: MustStruct(map[string]any{
					"apiVersion": "example.org/v1",
					"kind":       "Test",
				}),
			},
		},
		"ConfigMap": {
			reason: "It should be possible to convert a real, not unstructured, resource to a struct",
			o: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cool-map"},
			},
			want: want{
				s: MustStruct(map[string]any{
					"metadata": map[string]any{
						"name": "cool-map",
					},
				}),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, err := AsStruct(tc.o)
			if diff := cmp.Diff(tc.want.s, s, protocmp.Transform()); diff != "" {
				t.Errorf("\n%s\nTag(...): -want struct, +got struct:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nTag(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestFromStruct(t *testing.T) {
	type args struct {
		o runtime.Object
		s *structpb.Struct
	}
	type want struct {
		o   runtime.Object
		err error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Unstructured": {
			reason: "It should be possible to convert a struct to a Kubernetes unstructured.Unstructured",
			args: args{
				o: &kunstructured.Unstructured{},
				s: MustStruct(map[string]any{
					"apiVersion": "example.org/v1",
					"kind":       "Test",
				}),
			},
			want: want{
				o: &kunstructured.Unstructured{Object: map[string]any{
					"apiVersion": "example.org/v1",
					"kind":       "Test",
				}},
			},
		},
		"Composite": {
			reason: "It should be possible to convert a struct to a Crossplane composite.Unstructured",
			args: args{
				o: composite.New(),
				s: MustStruct(map[string]any{
					"apiVersion": "example.org/v1",
					"kind":       "Test",
				}),
			},
			want: want{
				o: composite.New(composite.WithGroupVersionKind(schema.GroupVersionKind{
					Group:   "example.org",
					Version: "v1",
					Kind:    "Test",
				})),
			},
		},
		"ConfigMap": {
			reason: "It should be possible to convert a struct to a real, not unstructured, resource",
			args: args{
				o: &corev1.ConfigMap{},
				s: MustStruct(map[string]any{
					"metadata": map[string]any{
						"name": "cool-map",
					},
				}),
			},
			want: want{
				o: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "cool-map"},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := FromStruct(tc.args.o, tc.args.s)
			if diff := cmp.Diff(tc.want.o, tc.args.o, protocmp.Transform()); diff != "" {
				t.Errorf("\n%s\nTag(...): -want object, +got object:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nTag(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func MustStruct(v map[string]any) *structpb.Struct {
	s, err := structpb.NewStruct(v)
	if err != nil {
		panic(err)
	}

	return s
}
