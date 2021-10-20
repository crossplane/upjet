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

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/terrajet/pkg/resource"
	"github.com/crossplane-contrib/terrajet/pkg/resource/fake"
	"github.com/crossplane-contrib/terrajet/pkg/resource/json"
	"github.com/crossplane-contrib/terrajet/pkg/terraform"
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
	DestroyAsyncFn func() error
	DestroyFn      func(ctx context.Context) error
	RefreshFn      func(ctx context.Context) (terraform.RefreshResult, error)
	PlanFn         func(ctx context.Context) (terraform.PlanResult, error)
}

func (c WorkspaceFns) ApplyAsync(callback terraform.CallbackFn) error {
	return c.ApplyAsyncFn(callback)
}

func (c WorkspaceFns) Apply(ctx context.Context) (terraform.ApplyResult, error) {
	return c.ApplyFn(ctx)
}

func (c WorkspaceFns) DestroyAsync() error {
	return c.DestroyAsyncFn()
}

func (c WorkspaceFns) Destroy(ctx context.Context) error {
	return c.DestroyFn(ctx)
}

func (c WorkspaceFns) Refresh(ctx context.Context) (terraform.RefreshResult, error) {
	return c.RefreshFn(ctx)
}

func (c WorkspaceFns) Plan(ctx context.Context) (terraform.PlanResult, error) {
	return c.PlanFn(ctx)
}

type StoreFns struct {
	WorkspaceFn func(ctx context.Context, c resource.SecretClient, tr resource.Terraformed, ts terraform.Setup) (*terraform.Workspace, error)
}

func (s StoreFns) Workspace(ctx context.Context, c resource.SecretClient, tr resource.Terraformed, ts terraform.Setup) (*terraform.Workspace, error) {
	return s.WorkspaceFn(ctx, c, tr, ts)
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
					WorkspaceFn: func(_ context.Context, _ resource.SecretClient, _ resource.Terraformed, _ terraform.Setup) (*terraform.Workspace, error) {
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
					WorkspaceFn: func(_ context.Context, _ resource.SecretClient, _ resource.Terraformed, _ terraform.Setup) (*terraform.Workspace, error) {
						return nil, nil
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewConnector(nil, tc.args.store, tc.args.setupFn)
			_, err := c.Connect(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestObserve(t *testing.T) {
	type args struct {
		w     Workspace
		kube  client.Client
		async bool
		obj   xpresource.Managed
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
		"LastOperationFailed": {
			reason: "It should report the last operation error without failing",
			args: args{
				async: true,
				obj: &fake.Terraformed{
					MetadataProvider: fake.MetadataProvider{
						IDField: "id",
					},
				},
				kube: &test.MockClient{
					MockStatusUpdate: test.NewMockStatusUpdateFn(nil),
				},
				w: WorkspaceFns{
					RefreshFn: func(_ context.Context) (terraform.RefreshResult, error) {
						return terraform.RefreshResult{
							State:              exampleState,
							LastOperationError: errBoom,
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
		"StatusUpdateFailed": {
			reason: "It should fail if status cannot be updated",
			args: args{
				async: true,
				obj:   &fake.Terraformed{},
				kube: &test.MockClient{
					MockStatusUpdate: test.NewMockStatusUpdateFn(errBoom),
				},
				w: WorkspaceFns{
					RefreshFn: func(_ context.Context) (terraform.RefreshResult, error) {
						return terraform.RefreshResult{
							State:              exampleState,
							LastOperationError: errBoom,
						}, nil
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errStatusUpdate),
			},
		},
		"GetReady": {
			reason: "We need to return early if the resource has just become ready",
			args: args{
				obj: &fake.Terraformed{
					MetadataProvider: fake.MetadataProvider{
						IDField: "id",
					},
				},
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
					ResourceExists:   true,
					ResourceUpToDate: true,
				},
			},
		},
		"PlanFailed": {
			reason: "Failure of plan should be reported",
			args: args{
				obj: &fake.Terraformed{
					MetadataProvider: fake.MetadataProvider{
						IDField: "id",
					},
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
					MetadataProvider: fake.MetadataProvider{
						IDField: "id",
					},
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
			e := &external{workspace: tc.w, kube: tc.kube, async: tc.async}
			_, err := e.Observe(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nObserve(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestCreate(t *testing.T) {
	type args struct {
		w     Workspace
		async bool
		obj   xpresource.Managed
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
		"AsyncFailed": {
			reason: "It should return error if it cannot trigger the async apply",
			args: args{
				async: true,
				obj:   &fake.Terraformed{},
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
			e := &external{workspace: tc.w, async: tc.async}
			_, err := e.Create(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nCreate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	type args struct {
		w     Workspace
		async bool
		obj   xpresource.Managed
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
		"AsyncFailed": {
			reason: "It should return error if it cannot trigger the async apply",
			args: args{
				async: true,
				obj:   &fake.Terraformed{},
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
			e := &external{workspace: tc.w, async: tc.async}
			_, err := e.Update(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nCreate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	type args struct {
		w     Workspace
		async bool
		obj   xpresource.Managed
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
				async: true,
				obj:   &fake.Terraformed{},
				w: WorkspaceFns{
					DestroyAsyncFn: func() error {
						return errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errStartAsyncDestroy),
			},
		},
		"SyncApplyFailed": {
			reason: "It should return error if it cannot destroy in sync mode",
			args: args{
				obj: &fake.Terraformed{},
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
			e := &external{workspace: tc.w, async: tc.async}
			err := e.Delete(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nCreate(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}
