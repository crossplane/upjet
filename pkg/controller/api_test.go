// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/crossplane/upjet/pkg/resource"
	"github.com/crossplane/upjet/pkg/resource/fake"
	tjerrors "github.com/crossplane/upjet/pkg/terraform/errors"
)

func TestAPICallbacksCreate(t *testing.T) {
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
		"CreateOperationFailed": {
			reason: "It should update the condition with error if async apply failed",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(tjerrors.NewApplyFailed(nil)), got); diff != "" {
								t.Errorf("\nCreate(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.Terraformed{}),
				},
				err: tjerrors.NewApplyFailed(nil),
			},
		},
		"CreateOperationSucceeded": {
			reason: "It should update the condition with success if the apply operation does not report error",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(nil), got); diff != "" {
								t.Errorf("\nCreate(...): -want error, +got error:\n%s", diff)
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
				err: errors.Wrapf(errBoom, errGetFmt, "", ", Kind=//name", "create"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewAPICallbacks(tc.args.mgr, tc.args.mg)
			err := e.Create(types.NamespacedName{Name: "name"})(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nCreate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAPICallbacksUpdate(t *testing.T) {
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
		"UpdateOperationFailed": {
			reason: "It should update the condition with error if async apply failed",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: test.NewMockGetFn(nil),
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(tjerrors.NewApplyFailed(nil)), got); diff != "" {
								t.Errorf("\nUpdate(...): -want error, +got error:\n%s", diff)
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
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(nil), got); diff != "" {
								t.Errorf("\nUpdate(...): -want error, +got error:\n%s", diff)
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
				err: errors.Wrapf(errBoom, errGetFmt, "", ", Kind=//name", "update"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewAPICallbacks(tc.args.mgr, tc.args.mg)
			err := e.Update(types.NamespacedName{Name: "name"})(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nUpdate(...): -want error, +got error:\n%s", tc.reason, diff)
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
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
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
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
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
				err: errors.Wrapf(errBoom, errGetFmt, "", ", Kind=//name", "destroy"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewAPICallbacks(tc.args.mgr, tc.args.mg)
			err := e.Destroy(types.NamespacedName{Name: "name"})(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nDestroy(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}
