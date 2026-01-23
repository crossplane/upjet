// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	authv1 "k8s.io/api/authorization/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The all logic was borrowed from crossplane-runtime. Because the relevant
// files are in the internal package in crossplane-runtime. When we move them
// outside the internal package, we can remove the duplication here.

// CRDsMigrator makes sure the CRDs are using the latest storage version.
type CRDsMigrator struct {
	gvkList      []schema.GroupVersionKind
	retryBackoff wait.Backoff
}

// CRDsMigratorOption is a functional option for configuring CRDsMigrator.
type CRDsMigratorOption func(*CRDsMigrator)

// WithRetryBackoff sets the retry backoff configuration.
func WithRetryBackoff(backoff wait.Backoff) CRDsMigratorOption {
	return func(c *CRDsMigrator) {
		c.retryBackoff = backoff
	}
}

// NewCRDsMigrator returns a new *CRDsMigrator with default retry configuration.
func NewCRDsMigrator(gvkList []schema.GroupVersionKind, opts ...CRDsMigratorOption) *CRDsMigrator {
	c := &CRDsMigrator{
		gvkList: gvkList,
		retryBackoff: wait.Backoff{
			Duration: 1 * time.Second,
			Factor:   2.0,
			Jitter:   0.1,
			Steps:    10,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Run migrates CRDs to use the latest storage version by listing all resources
// of the old storage version, patching them to trigger conversion to the new
// storage version, and updating the CRD status to reflect only the new storage version.
func (c *CRDsMigrator) Run(ctx context.Context, logr logging.Logger, discoveryClient discovery.DiscoveryInterface, kube client.Client) error { //nolint:gocyclo // easier to follow as a unit
	// Perform API discovery once before the loop to avoid expensive repeated discovery calls
	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return errors.Wrap(err, "failed to get API group resources")
	}
	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)

	for _, gvk := range c.gvkList {
		crdName, err := GetCRDNameFromGVK(mapper, gvk)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed to get CRD name from GVK %s", gvk.Kind))
		}

		var crd extv1.CustomResourceDefinition
		if err := kube.Get(ctx, client.ObjectKey{Name: crdName}, &crd); err != nil {
			if kerrors.IsNotFound(err) {
				// nothing to do for this CRD
				continue
			}

			return errors.Wrapf(err, "cannot get %s crd", crdName)
		}

		// Find the current storage version (the version marked as storage in the spec)
		var storageVersion string
		for _, v := range crd.Spec.Versions {
			if v.Storage {
				storageVersion = v.Name
				break
			}
		}
		if storageVersion == "" {
			return errors.Errorf("no storage version found for CRD %s", crdName)
		}
		storedVersions := crd.Status.StoredVersions

		// Check if migration is needed by comparing stored versions with the current storage version
		var needMigration bool
		for _, storedVersion := range storedVersions {
			if storedVersion != storageVersion {
				needMigration = true
				break
			}
		}

		if !needMigration {
			logr.Debug("Skipping CRD migration for CRD because it has already been migrated", crdName)
			continue
		}

		logr.Debug("Storage version migration is starting", "crd", crdName)
		// Prepare to list all resources of this CRD using the current storage version
		resources := unstructured.UnstructuredList{}
		resources.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   crd.Spec.Group,
			Version: storageVersion,
			Kind:    crd.Spec.Names.ListKind,
		})

		// List all resources in batches and patch each one to trigger storage version migration.
		// The empty patch causes the API server to read the resource in its stored version
		// and write it back in the current storage version.
		var continueToken string
		for {
			// Retry resource listing with exponential backoff
			listErr := retry.OnError(c.retryBackoff, func(err error) bool { return true }, func() error {
				return kube.List(ctx, &resources,
					client.Limit(500),
					client.Continue(continueToken),
				)
			})
			if listErr != nil {
				return errors.Wrapf(listErr, "cannot list %s", resources.GroupVersionKind().String())
			}

			for i := range resources.Items {
				// apply empty patch for storage version upgrade with retry
				res := resources.Items[i]
				patchErr := retry.OnError(c.retryBackoff, func(err error) bool { return true }, func() error {
					return kube.Patch(ctx, &res, client.RawPatch(types.MergePatchType, []byte(`{}`)))
				})
				if patchErr != nil {
					return errors.Wrapf(patchErr, "cannot patch %s %q", crd.Spec.Names.Kind, res.GetName())
				}
			}

			continueToken = resources.GetContinue()
			if continueToken == "" {
				break
			}
		}

		// Check if the client has permission to update/patch CRD status before attempting the update
		hasPermission, err := CheckCRDStatusUpdatePermission(ctx, kube, crdName)
		if err != nil {
			return errors.Wrapf(err, "permission check failed for CRD %s", crdName)
		}

		if !hasPermission {
			logr.Info(fmt.Sprintf("This client does not have permission to execute %s operation for patch", crdName))
			continue
		}

		// Update CRD status to reflect that only the new storage version is stored
		if err := UpdateCRDStorageVersion(ctx, kube, c.retryBackoff, crdName, storageVersion); err != nil {
			return err
		}
		logr.Debug("Storage version migration completed", "crd", crdName)
	}
	return nil
}

// GetCRDNameFromGVK returns the CRD name (e.g., "resources.group.example.com") for a given GroupVersionKind
// by using the provided REST mapper to find the REST mapping.
func GetCRDNameFromGVK(mapper meta.RESTMapper, gvk schema.GroupVersionKind) (string, error) {
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return "", err
	}

	return mapping.Resource.Resource + "." + mapping.Resource.Group, nil
}

// UpdateCRDStorageVersion updates the CRD status to reflect only the specified storage version.
// It retries the update with exponential backoff and verifies the update was successful.
func UpdateCRDStorageVersion(ctx context.Context, kube client.Client, retryBackoff wait.Backoff, crdName, storageVersion string) error {
	var crd extv1.CustomResourceDefinition
	// Update CRD status to reflect that only the new storage version is stored
	// Use retry for status updates as they can fail due to conflicts
	statusUpdateErr := retry.OnError(retryBackoff, func(err error) bool { return true }, func() error {
		// Re-fetch the CRD to get the latest version before patching
		if err := kube.Get(ctx, client.ObjectKey{Name: crdName}, &crd); err != nil {
			return err
		}
		origCrd := crd.DeepCopy()
		crd.Status.StoredVersions = []string{storageVersion}
		return kube.Status().Patch(ctx, &crd, client.MergeFrom(origCrd))
	})
	if statusUpdateErr != nil {
		return errors.Wrapf(statusUpdateErr, "couldn't update %s crd", crd.Name)
	}

	// One more check just to be sure we actually updated the crd
	if err := kube.Get(ctx, client.ObjectKey{Name: crd.Name}, &crd); err != nil {
		return errors.Wrapf(err, "cannot get %s crd to check", crd.Name)
	}

	if len(crd.Status.StoredVersions) != 1 || crd.Status.StoredVersions[0] != storageVersion {
		return errors.Errorf("was expecting CRD %q to only have %s, got instead: %v", crd.Name, storageVersion, crd.Status.StoredVersions)
	}
	return nil
}

// CheckCRDStatusUpdatePermission checks if the current client has permission to update/patch
// the status subresource of the specified CRD using SelfSubjectAccessReview.
func CheckCRDStatusUpdatePermission(ctx context.Context, kube client.Client, crdName string) (bool, error) {
	// Check for both 'patch' verb on the status subresource
	ssar := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Group:       "apiextensions.k8s.io",
				Resource:    "customresourcedefinitions",
				Subresource: "status",
				Name:        crdName,
				Verb:        "patch",
			},
		},
	}

	if err := kube.Create(ctx, ssar); err != nil {
		return false, errors.Wrap(err, "failed to create SelfSubjectAccessReview for verb patch")
	}

	if !ssar.Status.Allowed {
		return false, nil
	}

	return true, nil
}

// PrepareCRDsMigrator scans the provider's resources for any that have previous versions
// and creates a CRDsMigrator to handle storage version migration for those resources.
// It sets the StorageVersionMigrator field on the Provider with the configured migrator.
func PrepareCRDsMigrator(pc *Provider) {
	var gvkList []schema.GroupVersionKind
	for _, r := range pc.Resources {
		if len(r.PreviousVersions) != 0 {
			gvkList = append(gvkList, schema.GroupVersionKind{
				Group:   strings.ToLower(r.ShortGroup + "." + pc.RootGroup),
				Version: r.CRDStorageVersion(),
				Kind:    r.Kind,
			})
		}
	}
	pc.StorageVersionMigrator = NewCRDsMigrator(gvkList)
}
