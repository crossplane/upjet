// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"

	upjetresource "github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
	tjerrors "github.com/crossplane/upjet/v2/pkg/terraform/errors"
)

func TestAsyncTerraformPluginFrameworkObserve(t *testing.T) {
	type args struct {
		testConfig testConfiguration
		prepare    func(e *terraformPluginFrameworkAsyncExternalClient, obj *fake.Terraformed) error
	}
	type want struct {
		obs              managed.ExternalObservation
		err              error
		lastCondition    *xpv1.Condition
		lastOperationErr error
		externalName     string
	}

	cases := map[string]struct {
		args
		want
	}{
		"UpToDateWithUnresolvedAsyncFailure": {
			args: args{
				testConfig: testConfiguration{
					r:   newMockBaseTPFResource(),
					cfg: newBaseUpjetConfig(),
					obj: newBaseObject(),
					params: map[string]any{
						"id":   "example-id",
						"name": "example",
					},
					currentStateMap: map[string]any{
						"id":   "example-id",
						"name": "example",
					},
					plannedStateMap: map[string]any{
						"id":   "example-id",
						"name": "example",
					},
				},
				prepare: func(e *terraformPluginFrameworkAsyncExternalClient, obj *fake.Terraformed) error {
					lastErr := tjerrors.NewAsyncCreateFailed(errBoom)
					e.opTracker.LastOperation.MarkStart("create")
					e.opTracker.LastOperation.SetError(lastErr)
					e.opTracker.LastOperation.MarkEnd()
					obj.SetConditions(upjetresource.LastAsyncOperationCondition(lastErr))
					return nil
				},
			},
			want: want{
				obs: managed.ExternalObservation{
					ResourceExists:          true,
					ResourceUpToDate:        true,
					ResourceLateInitialized: true,
					ConnectionDetails:       nil,
					Diff:                    "",
				},
				lastCondition: &xpv1.Condition{
					Type:    upjetresource.TypeLastAsyncOperation,
					Status:  corev1.ConditionFalse,
					Reason:  upjetresource.ReasonAsyncCreateFailure,
					Message: tjerrors.NewAsyncCreateFailed(errBoom).Error(),
				},
				lastOperationErr: tjerrors.NewAsyncCreateFailed(errBoom),
			},
		},
		"RecoverExternalNameAfterFailedCreateNotUpToDate": {
			args: args{
				testConfig: testConfiguration{
					r:   newMockBaseTPFResource(),
					cfg: newBaseUpjetConfig(),
					obj: newBaseObject(),
					params: map[string]any{
						"name": "example",
					},
					currentStateMap: map[string]any{
						"name": "example",
					},
					plannedStateMap: map[string]any{
						"name": "example",
					},
				},
				prepare: func(e *terraformPluginFrameworkAsyncExternalClient, obj *fake.Terraformed) error {
					state, err := protov6DynamicValueFromMap(map[string]any{
						"id":   "example-id",
						"name": "example",
					}, e.resourceValueTerraformType)
					if err != nil {
						return err
					}
					lastErr := tjerrors.NewAsyncCreateFailed(errBoom)
					e.opTracker.SetFrameworkTFState(state)
					e.opTracker.LastOperation.MarkStart("create")
					e.opTracker.LastOperation.SetError(lastErr)
					e.opTracker.LastOperation.MarkEnd()
					obj.SetConditions(upjetresource.LastAsyncOperationCondition(lastErr))
					return nil
				},
			},
			want: want{
				obs: managed.ExternalObservation{
					ResourceExists:          true,
					ResourceUpToDate:        false,
					ResourceLateInitialized: true,
				},
				lastCondition: &xpv1.Condition{
					Type:    upjetresource.TypeLastAsyncOperation,
					Status:  corev1.ConditionFalse,
					Reason:  upjetresource.ReasonAsyncCreateFailure,
					Message: tjerrors.NewAsyncCreateFailed(errBoom).Error(),
				},
				lastOperationErr: tjerrors.NewAsyncCreateFailed(errBoom),
				externalName:     "example-id",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			baseExternal := prepareTPFExternalWithTestConfig(tc.args.testConfig)
			asyncExternal := &terraformPluginFrameworkAsyncExternalClient{
				terraformPluginFrameworkExternalClient: baseExternal,
			}
			obj := tc.args.testConfig.obj
			if tc.args.prepare != nil {
				if err := tc.args.prepare(asyncExternal, &obj); err != nil {
					t.Fatalf("prepare(...): %v", err)
				}
			}
			observation, err := asyncExternal.Observe(context.TODO(), &obj)
			if diff := cmp.Diff(tc.want.obs, observation); diff != "" {
				t.Errorf("\nObserve(...): -want observation, +got observation:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\nObserve(...): -want error, +got error:\n%s", diff)
			}
			if tc.want.lastCondition != nil {
				got := obj.GetCondition(tc.want.lastCondition.Type)
				if diff := cmp.Diff(*tc.want.lastCondition, got, cmpopts.IgnoreFields(xpv1.Condition{}, "LastTransitionTime")); diff != "" {
					t.Errorf("\nObserve(...): -want condition, +got condition:\n%s", diff)
				}
			}
			if tc.want.lastOperationErr != nil {
				if diff := cmp.Diff(tc.want.lastOperationErr, asyncExternal.opTracker.LastOperation.Error(), test.EquateErrors()); diff != "" {
					t.Errorf("\nObserve(...): -want last operation error, +got last operation error:\n%s", diff)
				}
			}
			if tc.want.externalName != "" {
				if diff := cmp.Diff(tc.want.externalName, meta.GetExternalName(&obj)); diff != "" {
					t.Errorf("\nObserve(...): -want external-name, +got external-name:\n%s", diff)
				}
			}
		})
	}
}

