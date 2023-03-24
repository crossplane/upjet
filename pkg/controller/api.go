/*
 Copyright 2021 Upbound Inc.

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

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/upbound/upjet/pkg/resource"
	"github.com/upbound/upjet/pkg/terraform"
)

const (
	errGet = "cannot get resource"
)

const (
	annotationKeyLastAsyncOperation     = "upjet.upbound.io/last-async-operation"
	annotationKeyAsyncOperationFinished = "upjet.upbound.io/async-operation-finished"
)

// APISecretClient is a client for getting k8s secrets
type APISecretClient struct {
	kube client.Client
}

// GetSecretData gets and returns data for the referenced secret
func (a *APISecretClient) GetSecretData(ctx context.Context, ref *xpv1.SecretReference) (map[string][]byte, error) {
	secret := &v1.Secret{}
	if err := a.kube.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, secret); err != nil {
		return nil, err
	}
	return secret.Data, nil
}

// GetSecretValue gets and returns value for key of the referenced secret
func (a *APISecretClient) GetSecretValue(ctx context.Context, sel xpv1.SecretKeySelector) ([]byte, error) {
	d, err := a.GetSecretData(ctx, &sel.SecretReference)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get secret data")
	}
	return d[sel.Key], err
}

// NewAPICallbacks returns a new APICallbacks.
func NewAPICallbacks(m ctrl.Manager, of xpresource.ManagedKind) *APICallbacks {
	nt := func() resource.Terraformed {
		return xpresource.MustCreateObject(schema.GroupVersionKind(of), m.GetScheme()).(resource.Terraformed)
	}
	return &APICallbacks{
		kube:           m.GetClient(),
		newTerraformed: nt,
	}
}

// APICallbacks providers callbacks that work on API resources.
type APICallbacks struct {
	kube           client.Client
	newTerraformed func() resource.Terraformed
}

// Apply makes sure the error is saved in async operation condition.
func (ac *APICallbacks) Apply(name string) terraform.CallbackFn {
	return func(err error, ctx context.Context) error {
		nn := types.NamespacedName{Name: name}
		tr := ac.newTerraformed()
		if kErr := ac.kube.Get(ctx, nn, tr); kErr != nil {
			return errors.Wrap(kErr, errGet)
		}
		tr.SetConditions(resource.LastAsyncOperationCondition(err))
		tr.SetConditions(resource.AsyncOperationFinishedCondition())

		if err = ac.kube.Status().Update(ctx, tr); err != nil {
			return errors.Wrap(err, errStatusUpdate)
		}

		// Note(turkenh): After consuming DesiredStateChanged predicate from
		// crossplane-runtime, updates to the status will no longer trigger a
		// reconciliation which was assumed above. To continue triggering a
		// reconciliation after an async operation completes, we need to modify
		// the desired state and annotations is the best fit here. So, we are
		// setting the last transition timestamps and put to annotations so that
		// a change in the annotations will trigger a reconciliation.
		// We intentionally get the timestamp from the resource
		// conditions back since it is only changed after a transition.
		meta.AddAnnotations(tr, map[string]string{annotationKeyLastAsyncOperation: tr.GetCondition(resource.TypeLastAsyncOperation).LastTransitionTime.String()})
		meta.AddAnnotations(tr, map[string]string{annotationKeyAsyncOperationFinished: tr.GetCondition(resource.TypeAsyncOperation).LastTransitionTime.String()})
		return errors.Wrap(ac.kube.Update(ctx, tr), errUpdate)
	}
}

// Destroy makes sure the error is saved in async operation condition.
func (ac *APICallbacks) Destroy(name string) terraform.CallbackFn {
	return func(err error, ctx context.Context) error {
		nn := types.NamespacedName{Name: name}
		tr := ac.newTerraformed()
		if kErr := ac.kube.Get(ctx, nn, tr); kErr != nil {
			return errors.Wrap(kErr, errGet)
		}
		tr.SetConditions(resource.LastAsyncOperationCondition(err))
		tr.SetConditions(resource.AsyncOperationFinishedCondition())

		if err = ac.kube.Status().Update(ctx, tr); err != nil {
			return errors.Wrap(err, errStatusUpdate)
		}

		// Note(turkenh): After consuming DesiredStateChanged predicate from
		// crossplane-runtime, updates to the status will no longer trigger a
		// reconciliation which was assumed above. To continue triggering a
		// reconciliation after an async operation completes, we need to modify
		// the desired state and annotations is the best fit here. So, we are
		// setting the last transition timestamps and put to annotations so that
		// a change in the annotations will trigger a reconciliation.
		// We intentionally get the timestamp from the resource
		// conditions back since it is only changed after a transition.
		meta.AddAnnotations(tr, map[string]string{annotationKeyLastAsyncOperation: tr.GetCondition(resource.TypeLastAsyncOperation).LastTransitionTime.String()})
		meta.AddAnnotations(tr, map[string]string{annotationKeyAsyncOperationFinished: tr.GetCondition(resource.TypeAsyncOperation).LastTransitionTime.String()})
		return errors.Wrap(ac.kube.Update(ctx, tr), errUpdate)
	}
}
