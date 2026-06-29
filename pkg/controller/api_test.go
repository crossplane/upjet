// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/crossplane/upjet/v2/pkg/controller/handler"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
	tjerrors "github.com/crossplane/upjet/v2/pkg/terraform/errors"
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
			err := e.Create(types.NamespacedName{Name: "name"}, true)(tc.args.err, context.TODO())
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
			err := e.Update(types.NamespacedName{Name: "name"}, true)(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nUpdate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAPICallbacksDestroy(t *testing.T) {
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
			err := e.Destroy(types.NamespacedName{Name: "name"}, true)(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nDestroy(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAPICallbacksCreateNamespaced(t *testing.T) {
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
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.ModernTerraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, gotKey client.ObjectKey, _ client.Object) error {
							if diff := cmp.Diff(client.ObjectKey{Name: "name", Namespace: "foo-ns"}, gotKey); diff != "" {
								t.Errorf("\nGet(...): -want object key, +got object key:\n%s", diff)
							}
							return nil
						},
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(tjerrors.NewApplyFailed(nil)), got); diff != "" {
								t.Errorf("\nCreate(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.ModernTerraformed{}),
				},
				err: tjerrors.NewApplyFailed(nil),
			},
		},
		"CreateOperationSucceeded": {
			reason: "It should update the condition with success if the apply operation does not report error",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.ModernTerraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, gotKey client.ObjectKey, _ client.Object) error {
							if diff := cmp.Diff(client.ObjectKey{Name: "name", Namespace: "foo-ns"}, gotKey); diff != "" {
								t.Errorf("\nGet(...): -want object key, +got object key:\n%s", diff)
							}
							return nil
						},
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(nil), got); diff != "" {
								t.Errorf("\nCreate(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.ModernTerraformed{}),
				},
			},
		},
		"CannotGet": {
			reason: "It should return error if it cannot get the resource to update",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.ModernTerraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
							return errBoom
						},
					},
					Scheme: xpfake.SchemeWith(&fake.ModernTerraformed{}),
				},
			},
			want: want{
				err: errors.Wrapf(errBoom, errGetFmt, "", ", Kind=/foo-ns/name", "create"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewAPICallbacks(tc.args.mgr, tc.args.mg)
			err := e.Create(types.NamespacedName{Name: "name", Namespace: "foo-ns"}, true)(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nCreate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAPICallbacksUpdateNamespaced(t *testing.T) {
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
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.ModernTerraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, gotKey client.ObjectKey, _ client.Object) error {
							if diff := cmp.Diff(client.ObjectKey{Name: "name", Namespace: "foo-ns"}, gotKey); diff != "" {
								t.Errorf("\nGet(...): -want object key, +got object key:\n%s", diff)
							}
							return nil
						},
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(tjerrors.NewApplyFailed(nil)), got); diff != "" {
								t.Errorf("\nUpdate(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.ModernTerraformed{}),
				},
				err: tjerrors.NewApplyFailed(nil),
			},
		},
		"ApplyOperationSucceeded": {
			reason: "It should update the condition with success if the apply operation does not report error",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.ModernTerraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, gotKey client.ObjectKey, _ client.Object) error {
							if diff := cmp.Diff(client.ObjectKey{Name: "name", Namespace: "foo-ns"}, gotKey); diff != "" {
								t.Errorf("\nGet(...): -want object key, +got object key:\n%s", diff)
							}
							return nil
						},
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(nil), got); diff != "" {
								t.Errorf("\nUpdate(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.ModernTerraformed{}),
				},
			},
		},
		"CannotGet": {
			reason: "It should return error if it cannot get the resource to update",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.ModernTerraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
							return errBoom
						},
					},
					Scheme: xpfake.SchemeWith(&fake.ModernTerraformed{}),
				},
			},
			want: want{
				err: errors.Wrapf(errBoom, errGetFmt, "", ", Kind=/foo-ns/name", "update"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewAPICallbacks(tc.args.mgr, tc.args.mg)
			err := e.Update(types.NamespacedName{Name: "name", Namespace: "foo-ns"}, true)(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nUpdate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAPICallbacksDestroyNamespaced(t *testing.T) {
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
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.ModernTerraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, gotKey client.ObjectKey, _ client.Object) error {
							if diff := cmp.Diff(client.ObjectKey{Name: "name", Namespace: "foo-ns"}, gotKey); diff != "" {
								t.Errorf("\nGet(...): -want object key, +got object key:\n%s", diff)
							}
							return nil
						},
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(tjerrors.NewDestroyFailed(nil)), got); diff != "" {
								t.Errorf("\nDestroy(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.ModernTerraformed{}),
				},
				err: tjerrors.NewDestroyFailed(nil),
			},
		},
		"DestroyOperationSucceeded": {
			reason: "It should update the condition with success if the destroy operation does not report error",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.ModernTerraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, gotKey client.ObjectKey, _ client.Object) error {
							if diff := cmp.Diff(client.ObjectKey{Name: "name", Namespace: "foo-ns"}, gotKey); diff != "" {
								t.Errorf("\nGet(...): -want object key, +got object key:\n%s", diff)
							}
							return nil
						},
						MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							got := obj.(resource.Terraformed).GetCondition(resource.TypeLastAsyncOperation)
							if diff := cmp.Diff(resource.LastAsyncOperationCondition(nil), got); diff != "" {
								t.Errorf("\nDestroy(...): -want error, +got error:\n%s", diff)
							}
							return nil
						},
					},
					Scheme: xpfake.SchemeWith(&fake.ModernTerraformed{}),
				},
			},
		},
		"CannotGet": {
			reason: "It should return error if it cannot get the resource to update",
			args: args{
				mg: xpresource.ManagedKind(xpfake.GVK(&fake.ModernTerraformed{})),
				mgr: &xpfake.Manager{
					Client: &test.MockClient{
						MockGet: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
							return errBoom
						},
					},
					Scheme: xpfake.SchemeWith(&fake.ModernTerraformed{}),
				},
			},
			want: want{
				err: errors.Wrapf(errBoom, errGetFmt, "", ", Kind=/foo-ns/name", "destroy"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewAPICallbacks(tc.args.mgr, tc.args.mg)
			err := e.Destroy(types.NamespacedName{Name: "name", Namespace: "foo-ns"}, true)(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nDestroy(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

// okManager returns a manager whose client successfully handles Get and
// Status().Update so that the callback reaches the requestReconcile gate.
func okManager() ctrl.Manager {
	return &xpfake.Manager{
		Client: &test.MockClient{
			MockGet: test.NewMockGetFn(nil),
			MockStatusUpdate: func(_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				return nil
			},
		},
		Scheme: xpfake.SchemeWith(&fake.Terraformed{}),
	}
}

// newUnprimedEventHandler returns an EventHandler whose underlying queue has
// not been initialized. RequestReconcile on such a handler returns false,
// which the APICallbacks surfaces as an errReconcileRequestFmt error. The
// tests use this to assert whether RequestReconcile was invoked at all,
// without depending on a real workqueue.
func newUnprimedEventHandler() *handler.EventHandler {
	return handler.NewEventHandler(handler.WithLogger(logging.NewNopLogger()))
}

func TestAPICallbacksCreateRequestReconcile(t *testing.T) {
	type args struct {
		err              error
		withEventHandler bool
		requestReconcile bool
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NoEventHandlerRequestReconcileTrue": {
			reason: "Without an event handler, requestReconcile=true must be a no-op.",
			args: args{
				requestReconcile: true,
			},
		},
		"NoEventHandlerRequestReconcileFalse": {
			reason: "Without an event handler, requestReconcile=false must be a no-op.",
			args: args{
				requestReconcile: false,
			},
		},
		"WithEventHandlerRequestReconcileFalse": {
			reason: "When the event handler is set but requestReconcile=false, no reconcile request must be queued.",
			args: args{
				withEventHandler: true,
				requestReconcile: false,
			},
		},
		"WithEventHandlerRequestReconcileFalseOnError": {
			reason: "A callback error must not trigger a reconcile request when requestReconcile=false.",
			args: args{
				err:              tjerrors.NewApplyFailed(nil),
				withEventHandler: true,
				requestReconcile: false,
			},
		},
		"WithEventHandlerRequestReconcileTrue": {
			reason: "When the event handler is set and requestReconcile=true, the callback must attempt to queue a reconcile request; with an uninitialized queue this surfaces as an errReconcileRequestFmt error.",
			args: args{
				withEventHandler: true,
				requestReconcile: true,
			},
			want: want{
				err: errors.Errorf(errReconcileRequestFmt, "", ", Kind=//name", opCreate),
			},
		},
		"WithEventHandlerRequestReconcileTrueOnError": {
			reason: "On callback error with requestReconcile=true, the callback must still attempt to queue a rate-limited reconcile request.",
			args: args{
				err:              tjerrors.NewApplyFailed(nil),
				withEventHandler: true,
				requestReconcile: true,
			},
			want: want{
				err: errors.Errorf(errReconcileRequestFmt, "", ", Kind=//name", opCreate),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var opts []APICallbacksOption
			if tc.args.withEventHandler {
				opts = append(opts, WithEventHandler(newUnprimedEventHandler()))
			}
			e := NewAPICallbacks(okManager(), xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})), opts...)
			err := e.Create(types.NamespacedName{Name: "name"}, tc.args.requestReconcile)(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nCreate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAPICallbacksUpdateRequestReconcile(t *testing.T) {
	type args struct {
		err              error
		withEventHandler bool
		requestReconcile bool
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NoEventHandlerRequestReconcileTrue": {
			reason: "Without an event handler, requestReconcile=true must be a no-op.",
			args: args{
				requestReconcile: true,
			},
		},
		"NoEventHandlerRequestReconcileFalse": {
			reason: "Without an event handler, requestReconcile=false must be a no-op.",
			args: args{
				requestReconcile: false,
			},
		},
		"WithEventHandlerRequestReconcileFalse": {
			reason: "When the event handler is set but requestReconcile=false, no reconcile request must be queued.",
			args: args{
				withEventHandler: true,
				requestReconcile: false,
			},
		},
		"WithEventHandlerRequestReconcileFalseOnError": {
			reason: "A callback error must not trigger a reconcile request when requestReconcile=false.",
			args: args{
				err:              tjerrors.NewApplyFailed(nil),
				withEventHandler: true,
				requestReconcile: false,
			},
		},
		"WithEventHandlerRequestReconcileTrue": {
			reason: "When the event handler is set and requestReconcile=true, the callback must attempt to queue a reconcile request; with an uninitialized queue this surfaces as an errReconcileRequestFmt error.",
			args: args{
				withEventHandler: true,
				requestReconcile: true,
			},
			want: want{
				err: errors.Errorf(errReconcileRequestFmt, "", ", Kind=//name", opUpdate),
			},
		},
		"WithEventHandlerRequestReconcileTrueOnError": {
			reason: "On callback error with requestReconcile=true, the callback must still attempt to queue a rate-limited reconcile request.",
			args: args{
				err:              tjerrors.NewApplyFailed(nil),
				withEventHandler: true,
				requestReconcile: true,
			},
			want: want{
				err: errors.Errorf(errReconcileRequestFmt, "", ", Kind=//name", opUpdate),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var opts []APICallbacksOption
			if tc.args.withEventHandler {
				opts = append(opts, WithEventHandler(newUnprimedEventHandler()))
			}
			e := NewAPICallbacks(okManager(), xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})), opts...)
			err := e.Update(types.NamespacedName{Name: "name"}, tc.args.requestReconcile)(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nUpdate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAPICallbacksDestroyRequestReconcile(t *testing.T) {
	type args struct {
		err              error
		withEventHandler bool
		requestReconcile bool
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"NoEventHandlerRequestReconcileTrue": {
			reason: "Without an event handler, requestReconcile=true must be a no-op.",
			args: args{
				requestReconcile: true,
			},
		},
		"NoEventHandlerRequestReconcileFalse": {
			reason: "Without an event handler, requestReconcile=false must be a no-op.",
			args: args{
				requestReconcile: false,
			},
		},
		"WithEventHandlerRequestReconcileFalse": {
			reason: "When the event handler is set but requestReconcile=false, no reconcile request must be queued.",
			args: args{
				withEventHandler: true,
				requestReconcile: false,
			},
		},
		"WithEventHandlerRequestReconcileFalseOnError": {
			reason: "A callback error must not trigger a reconcile request when requestReconcile=false.",
			args: args{
				err:              tjerrors.NewDestroyFailed(nil),
				withEventHandler: true,
				requestReconcile: false,
			},
		},
		"WithEventHandlerRequestReconcileTrue": {
			reason: "When the event handler is set and requestReconcile=true, the callback must attempt to queue a reconcile request; with an uninitialized queue this surfaces as an errReconcileRequestFmt error.",
			args: args{
				withEventHandler: true,
				requestReconcile: true,
			},
			want: want{
				err: errors.Errorf(errReconcileRequestFmt, "", ", Kind=//name", opDestroy),
			},
		},
		"WithEventHandlerRequestReconcileTrueOnError": {
			reason: "On callback error with requestReconcile=true, the callback must still attempt to queue a rate-limited reconcile request.",
			args: args{
				err:              tjerrors.NewDestroyFailed(nil),
				withEventHandler: true,
				requestReconcile: true,
			},
			want: want{
				err: errors.Errorf(errReconcileRequestFmt, "", ", Kind=//name", opDestroy),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var opts []APICallbacksOption
			if tc.args.withEventHandler {
				opts = append(opts, WithEventHandler(newUnprimedEventHandler()))
			}
			e := NewAPICallbacks(okManager(), xpresource.ManagedKind(xpfake.GVK(&fake.Terraformed{})), opts...)
			err := e.Destroy(types.NamespacedName{Name: "name"}, tc.args.requestReconcile)(tc.args.err, context.TODO())
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nDestroy(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}
