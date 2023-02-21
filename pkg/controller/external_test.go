/*
Copyright 2021 Upbound Inc.
*/

package controller

import (
	"context"
	"testing"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	xpmeta "github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/resource/fake"
	"github.com/upbound/upjet/pkg/resource/json"
	"github.com/upbound/upjet/pkg/terraform"
)

var (
	errBoom      = errors.New("boom")
	exampleState = &json.StateV4{
		Resources: []json.ResourceStateV4{
			{
				Instances: []json.InstanceObjectStateV4{
					{
						AttributesRaw: []byte(`{"id":"some-id","obs":"obsval","param":"paramval"}`),
					},
				},
			},
		},
	}
)

type WorkspaceFns struct {
	ApplyAsyncFn   func(callback terraform.CallbackFn) error
	ApplyFn        func(ctx context.Context) (terraform.ApplyResult, error)
	DestroyAsyncFn func(callback terraform.CallbackFn) error
	DestroyFn      func(ctx context.Context) error
	RefreshFn      func(ctx context.Context) (terraform.RefreshResult, error)
	ImportFn       func(ctx context.Context, tr resource.Terraformed) (terraform.RefreshResult, error)
	PlanFn         func(ctx context.Context) (terraform.PlanResult, error)
}

func (c WorkspaceFns) ApplyAsync(callback terraform.CallbackFn) error {
	return c.ApplyAsyncFn(callback)
}

func (c WorkspaceFns) Apply(ctx context.Context) (terraform.ApplyResult, error) {
	return c.ApplyFn(ctx)
}

func (c WorkspaceFns) DestroyAsync(callback terraform.CallbackFn) error {
	return c.DestroyAsyncFn(callback)
}

func (c WorkspaceFns) Destroy(ctx context.Context) error {
	return c.DestroyFn(ctx)
}

func (c WorkspaceFns) Refresh(ctx context.Context) (terraform.RefreshResult, error) {
	return c.RefreshFn(ctx)
}

func (c WorkspaceFns) Import(ctx context.Context, tr resource.Terraformed) (terraform.RefreshResult, error) {
	return c.ImportFn(ctx, tr)
}

func (c WorkspaceFns) Plan(ctx context.Context) (terraform.PlanResult, error) {
	return c.PlanFn(ctx)
}

type StoreFns struct {
	WorkspaceFn func(ctx context.Context, c resource.SecretClient, tr resource.Terraformed, ts terraform.Setup, cfg *config.Resource) (*terraform.Workspace, error)
}

func (s StoreFns) Workspace(ctx context.Context, c resource.SecretClient, tr resource.Terraformed, ts terraform.Setup, cfg *config.Resource) (*terraform.Workspace, error) {
	return s.WorkspaceFn(ctx, c, tr, ts, cfg)
}

type CallbackFns struct {
	ApplyFn   func(string) terraform.CallbackFn
	DestroyFn func(string) terraform.CallbackFn
}

func (c CallbackFns) Apply(name string) terraform.CallbackFn {
	return c.ApplyFn(name)
}

func (c CallbackFns) Destroy(name string) terraform.CallbackFn {
	return c.DestroyFn(name)
}

func TestConnect(t *testing.T) {
	type args struct {
		setupFn terraform.SetupFn
		store   Store
		obj     xpresource.Managed
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"WrongType": {
			args: args{
				obj: &xpfake.Managed{},
			},
			want: want{
				err: errors.New(errUnexpectedObject),
			},
		},
		"SetupFailed": {
			reason: "Terraform setup should succeed",
			args: args{
				obj: &fake.Terraformed{},
				setupFn: func(_ context.Context, _ client.Client, _ xpresource.Managed) (terraform.Setup, error) {
					return terraform.Setup{}, errBoom
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errGetTerraformSetup),
			},
		},
		"WorkspaceFailed": {
			reason: "We must get workspace successfully",
			args: args{
				obj: &fake.Terraformed{},
				setupFn: func(_ context.Context, _ client.Client, _ xpresource.Managed) (terraform.Setup, error) {
					return terraform.Setup{}, nil
				},
				store: StoreFns{
					WorkspaceFn: func(_ context.Context, _ resource.SecretClient, _ resource.Terraformed, _ terraform.Setup, _ *config.Resource) (*terraform.Workspace, error) {
						return nil, errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errGetWorkspace),
			},
		},
		"Success": {
			args: args{
				obj: &fake.Terraformed{},
				setupFn: func(_ context.Context, _ client.Client, _ xpresource.Managed) (terraform.Setup, error) {
					return terraform.Setup{}, nil
				},
				store: StoreFns{
					WorkspaceFn: func(_ context.Context, _ resource.SecretClient, _ resource.Terraformed, _ terraform.Setup, _ *config.Resource) (*terraform.Workspace, error) {
						return nil, nil
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewConnector(nil, tc.args.store, tc.args.setupFn, &config.Resource{})
			_, err := c.Connect(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestObserve(t *testing.T) {
	type args struct {
		w   Workspace
		obj xpresource.Managed
	}
	type want struct {
		obs managed.ExternalObservation
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"WrongType": {
			args: args{
				obj: &xpfake.Managed{},
			},
			want: want{
				err: errors.New(errUnexpectedObject),
			},
		},
		"RefreshFailed": {
			reason: "It should return error if we cannot refresh",
			args: args{
				obj: &fake.Terraformed{},
				w: WorkspaceFns{
					RefreshFn: func(_ context.Context) (terraform.RefreshResult, error) {
						return terraform.RefreshResult{}, errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errRefresh),
			},
		},
		"RefreshNotFound": {
			reason: "It should not report error in case resource is not found",
			args: args{
				obj: &fake.Terraformed{},
				w: WorkspaceFns{
					RefreshFn: func(_ context.Context) (terraform.RefreshResult, error) {
						return terraform.RefreshResult{Exists: false}, nil
					},
				},
			},
		},
		"RefreshInProgress": {
			reason: "It should report exists and up-to-date if an operation is ongoing",
			args: args{
				obj: &fake.Terraformed{},
				w: WorkspaceFns{
					RefreshFn: func(_ context.Context) (terraform.RefreshResult, error) {
						return terraform.RefreshResult{
							IsApplying: true,
						}, nil
					},
				},
			},
			want: want{
				obs: managed.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: true,
				},
			},
		},
		"TransitionToReady": {
			reason: "We should mark the resource as ready if the refresh succeeds and there is no ongoing operation",
			args: args{
				obj: &fake.Terraformed{},
				w: WorkspaceFns{
					RefreshFn: func(_ context.Context) (terraform.RefreshResult, error) {
						return terraform.RefreshResult{
							Exists: true,
							State:  exampleState,
						}, nil
					},
				},
			},
			want: want{
				obs: managed.ExternalObservation{
					ResourceExists:          true,
					ResourceUpToDate:        true,
					ConnectionDetails:       nil,
					ResourceLateInitialized: false,
				},
			},
		},
		"PlanFailed": {
			reason: "Failure of plan should be reported",
			args: args{
				obj: &fake.Terraformed{
					Managed: xpfake.Managed{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								xpmeta.AnnotationKeyExternalName: "some-id",
							},
						},
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{xpv1.Available()},
						},
					},
				},
				w: WorkspaceFns{
					RefreshFn: func(_ context.Context) (terraform.RefreshResult, error) {
						return terraform.RefreshResult{
							Exists: true,
							State:  exampleState,
						}, nil
					},
					PlanFn: func(_ context.Context) (terraform.PlanResult, error) {
						return terraform.PlanResult{}, errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errPlan),
			},
		},
		"Success": {
			args: args{
				obj: &fake.Terraformed{
					Managed: xpfake.Managed{
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{xpv1.Available()},
						},
					},
				},
				w: WorkspaceFns{
					RefreshFn: func(_ context.Context) (terraform.RefreshResult, error) {
						return terraform.RefreshResult{
							Exists: true,
							State:  exampleState,
						}, nil
					},
					PlanFn: func(_ context.Context) (terraform.PlanResult, error) {
						return terraform.PlanResult{UpToDate: true}, nil
					},
				},
			},
			want: want{
				obs: managed.ExternalObservation{
					ResourceExists:          true,
					ResourceUpToDate:        true,
					ResourceLateInitialized: true,
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := &external{workspace: tc.w, config: config.DefaultResource("upjet_resource", nil, nil)}
			_, err := e.Observe(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nObserve(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestCreate(t *testing.T) {
	type args struct {
		w   Workspace
		c   CallbackProvider
		cfg *config.Resource
		obj xpresource.Managed
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"WrongType": {
			args: args{
				cfg: &config.Resource{},
				obj: &xpfake.Managed{},
			},
			want: want{
				err: errors.New(errUnexpectedObject),
			},
		},
		"AsyncFailed": {
			reason: "It should return error if it cannot trigger the async apply",
			args: args{
				cfg: &config.Resource{
					UseAsync: true,
				},
				c: CallbackFns{
					ApplyFn: func(s string) terraform.CallbackFn {
						return nil
					},
				},
				obj: &fake.Terraformed{},
				w: WorkspaceFns{
					ApplyAsyncFn: func(_ terraform.CallbackFn) error {
						return errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errStartAsyncApply),
			},
		},
		"SyncApplyFailed": {
			reason: "It should return error if it cannot apply in sync mode",
			args: args{
				cfg: &config.Resource{},
				obj: &fake.Terraformed{},
				w: WorkspaceFns{
					ApplyFn: func(_ context.Context) (terraform.ApplyResult, error) {
						return terraform.ApplyResult{}, errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errApply),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := &external{workspace: tc.w, callback: tc.c, config: tc.cfg}
			_, err := e.Create(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nCreate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	type args struct {
		w   Workspace
		cfg *config.Resource
		c   CallbackProvider
		obj xpresource.Managed
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"WrongType": {
			args: args{
				cfg: &config.Resource{},
				obj: &xpfake.Managed{},
			},
			want: want{
				err: errors.New(errUnexpectedObject),
			},
		},
		"AsyncFailed": {
			reason: "It should return error if it cannot trigger the async apply",
			args: args{
				cfg: &config.Resource{
					UseAsync: true,
				},
				c: CallbackFns{
					ApplyFn: func(s string) terraform.CallbackFn {
						return nil
					},
				},
				obj: &fake.Terraformed{},
				w: WorkspaceFns{
					ApplyAsyncFn: func(_ terraform.CallbackFn) error {
						return errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errStartAsyncApply),
			},
		},
		"SyncApplyFailed": {
			reason: "It should return error if it cannot apply in sync mode",
			args: args{
				cfg: &config.Resource{},
				obj: &fake.Terraformed{},
				w: WorkspaceFns{
					ApplyFn: func(_ context.Context) (terraform.ApplyResult, error) {
						return terraform.ApplyResult{}, errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errApply),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := &external{workspace: tc.w, callback: tc.c, config: tc.cfg}
			_, err := e.Update(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nCreate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	type args struct {
		w   Workspace
		cfg *config.Resource
		c   CallbackProvider
		obj xpresource.Managed
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"AsyncFailed": {
			reason: "It should return error if it cannot trigger the async destroy",
			args: args{
				cfg: &config.Resource{
					UseAsync: true,
				},
				c: CallbackFns{
					DestroyFn: func(_ string) terraform.CallbackFn {
						return nil
					},
				},
				obj: &fake.Terraformed{},
				w: WorkspaceFns{
					DestroyAsyncFn: func(_ terraform.CallbackFn) error {
						return errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errStartAsyncDestroy),
			},
		},
		"SyncDestroyFailed": {
			reason: "It should return error if it cannot destroy in sync mode",
			args: args{
				obj: &fake.Terraformed{},
				cfg: &config.Resource{},
				w: WorkspaceFns{
					DestroyFn: func(_ context.Context) error {
						return errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errDestroy),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := &external{workspace: tc.w, callback: tc.c, config: tc.cfg}
			err := e.Delete(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nCreate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}
