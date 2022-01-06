/*
 Copyright 2021 The Crossplane Authors.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package controller

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/manager"

	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"

	"github.com/crossplane-contrib/terrajet/pkg/resource"
	"github.com/crossplane-contrib/terrajet/pkg/resource/fake"
	tjerrors "github.com/crossplane-contrib/terrajet/pkg/terraform/errors"
)

func TestAPICallbacks_Apply(t *testing.T) {
	type args struct {
		mgr ctrl.Manager
		mg  xpresource.ManagedKind
		err error
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"ApplyOperationFailed": {
			reason: "It should update the condition with error if async apply failed",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
						MockStatusUpdate: func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(tjerrors.NewApplyFailed(nil)), got); diff != "" {
								t.Errorf("\nApply(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.Terraformed{}),
				},
				err: tjerrors.NewApplyFailed(nil),
			},
		},
		"ApplyOperationSucceeded": {
			reason: "It should update the condition with success if the apply operation does not report error",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
						MockStatusUpdate: func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(nil), got); diff != "" {
								t.Errorf("\nApply(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.Terraformed{}),
				},
			},
		},
		"CannotGet": {
			reason: "It should return error if it cannot get the resource to update",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
							return errBoom
						},
					},
					Scheme: xpfake.SchemeWith(&fake.Terraformed{}),
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errGet),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewAPICallbacks(tc.args.mgr, tc.args.mg)
			err := e.Apply("name")(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nApply(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAPICallbacks_Destroy(t *testing.T) {
	type args struct {
		mgr ctrl.Manager
		mg  xpresource.ManagedKind
		err error
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"DestroyOperationFailed": {
			reason: "It should update the condition with error if async destroy failed",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
						MockStatusUpdate: func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(tjerrors.NewDestroyFailed(nil)), got); diff != "" {
								t.Errorf("\nApply(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.Terraformed{}),
				},
				err: tjerrors.NewDestroyFailed(nil),
			},
		},
		"DestroyOperationSucceeded": {
			reason: "It should update the condition with success if the destroy operation does not report error",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
						MockStatusUpdate: func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(nil), got); diff != "" {
								t.Errorf("\nApply(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.Terraformed{}),
				},
			},
		},
		"CannotGet": {
			reason: "It should return error if it cannot get the resource to update",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
							return errBoom
						},
					},
					Scheme: xpfake.SchemeWith(&fake.Terraformed{}),
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errGet),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewAPICallbacks(tc.args.mgr, tc.args.mg)
			err := e.Destroy("name")(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nDestroy(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}
