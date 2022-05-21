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

package terraform

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/resource"
)

var (
	errBoom = errors.New("errboom")
)

type StoreFns struct {
	WorkspaceFn func(ctx context.Context, tr resource.Terraformed, ts Setup, l logging.Logger) (*Workspace, error)
	RemoveFn    func(obj xpresource.Object) error
}

func (sf *StoreFns) Workspace(ctx context.Context, tr resource.Terraformed, ts Setup, l logging.Logger) (*Workspace, error) {
	return sf.WorkspaceFn(ctx, tr, ts, l)
}

func (sf *StoreFns) Remove(obj xpresource.Object) error {
	return sf.RemoveFn(obj)
}

func TestAddFinalizer(t *testing.T) {
	type args struct {
		finalizer xpresource.Finalizer
		store     StoreCleaner
		obj       xpresource.Object
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"Success": {
			args: args{
				finalizer: xpresource.FinalizerFns{
					AddFinalizerFn: func(_ context.Context, _ xpresource.Object) error {
						return nil
					},
				},
			},
		},
		"Failure": {
			args: args{
				finalizer: xpresource.FinalizerFns{
					AddFinalizerFn: func(_ context.Context, _ xpresource.Object) error {
						return errBoom
					},
				},
			},
			want: want{
				err: errBoom,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := NewWorkspaceFinalizer(tc.args.store, tc.args.finalizer)
			err := f.AddFinalizer(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nAddFinalizer(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestRemoveFinalizer(t *testing.T) {
	type args struct {
		finalizer xpresource.Finalizer
		store     StoreCleaner
		obj       xpresource.Object
	}
	type want struct {
		err error
	}
	cases := map[string]struct {
		reason string
		args
		want
	}{
		"Success": {
			args: args{
				store: &StoreFns{
					RemoveFn: func(_ xpresource.Object) error {
						return nil
					},
				},
				finalizer: xpresource.FinalizerFns{
					RemoveFinalizerFn: func(_ context.Context, _ xpresource.Object) error {
						return nil
					},
				},
			},
		},
		"StoreRemovalFails": {
			args: args{
				store: &StoreFns{
					RemoveFn: func(_ xpresource.Object) error {
						return errBoom
					},
				},
			},
			want: want{
				err: errors.Wrap(errBoom, errRemoveWorkspace),
			},
		},
		"FinalizerFails": {
			args: args{
				store: &StoreFns{
					RemoveFn: func(_ xpresource.Object) error {
						return nil
					},
				},
				finalizer: xpresource.FinalizerFns{
					RemoveFinalizerFn: func(_ context.Context, _ xpresource.Object) error {
						return errBoom
					},
				},
			},
			want: want{
				err: errBoom,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := NewWorkspaceFinalizer(tc.args.store, tc.args.finalizer)
			err := f.RemoveFinalizer(context.TODO(), tc.args.obj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nRemoveFinalizer(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}
