// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	authv1 "k8s.io/api/authorization/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The logic below was borrowed from crossplane/crossplane's
// internal/initializer/crds_migrator.go. Because the relevant files are in
// the internal package in crossplane/crossplane, when they are moved to a
// public package, we can remove the duplication here.

// CRDMigrator makes sure the CRDs are using the latest storage version.
// It discovers CRDs dynamically at runtime by listing all CRDs in the cluster
// and filtering them by the configured root group and optional short group.
type CRDMigrator struct {
	rootGroup    string
	shortGroup   string
	retryBackoff wait.Backoff
}

// CRDMigratorOption is a functional option for configuring CRDMigrator.
type CRDMigratorOption func(*CRDMigrator)

// WithRetryBackoff sets the retry backoff configuration.
func WithRetryBackoff(backoff wait.Backoff) CRDMigratorOption {
	return func(c *CRDMigrator) {
		c.retryBackoff = backoff
	}
}

// WithShortGroup restricts migration to CRDs whose API group is exactly
// shortGroup+"."+rootGroup (e.g. short group "ec2" with root group
// "aws.upbound.io" matches only "ec2.aws.upbound.io"). When no short group is
// set, all CRDs whose group equals or ends with the root group are considered.
func WithShortGroup(shortGroup string) CRDMigratorOption {
	return func(c *CRDMigrator) {
		c.shortGroup = shortGroup
	}
}

// NewCRDMigrator returns a new *CRDMigrator that dynamically discovers CRDs
// belonging to rootGroup (e.g. "aws.upbound.io") with default retry
// configuration. Use WithShortGroup to narrow the scope to a single service
// group (e.g. "ec2" → only "ec2.aws.upbound.io" CRDs).
func NewCRDMigrator(rootGroup string, opts ...CRDMigratorOption) *CRDMigrator {
	c := &CRDMigrator{
		rootGroup: rootGroup,
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

// matchesGroup returns true if the given CRD API group falls within the
// migrator's configured root/short group scope.
func (c *CRDMigrator) matchesGroup(group string) bool {
	if c.shortGroup == "" {
		return group == c.rootGroup || strings.HasSuffix(group, "."+c.rootGroup)
	}
	return group == c.shortGroup+"."+c.rootGroup
}

// Run migrates CRDs to use the latest storage version by dynamically
// discovering all CRDs that belong to the configured group scope, then for
// each CRD: listing all resources of the old storage version, patching them to
// trigger conversion to the new storage version, and updating the CRD status
// to reflect only the new storage version.
func (c *CRDMigrator) Run(ctx context.Context, logr logging.Logger, kube client.Client) error {
	var crdList extv1.CustomResourceDefinitionList
	if err := kube.List(ctx, &crdList); err != nil {
		return errors.Wrap(err, "failed to list CRDs")
	}

	var errs []error
	for i := range crdList.Items {
		crd := crdList.Items[i]
		if !c.matchesGroup(crd.Spec.Group) {
			continue
		}
		if err := c.migrateCRD(ctx, logr, kube, crd); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *CRDMigrator) migrateCRD(ctx context.Context, logr logging.Logger, kube client.Client, crd extv1.CustomResourceDefinition) error { //nolint:gocyclo // easier to follow as a unit
	crdName := crd.Name

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

	// Check if migration is needed by comparing stored versions with the current storage version
	var needMigration bool
	for _, storedVersion := range crd.Status.StoredVersions {
		if storedVersion != storageVersion {
			needMigration = true
			break
		}
	}

	if !needMigration {
		logr.Debug("Skipping CRD migration, storedVersions already matches the current storage version", "crd", crdName)
		return nil
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
		listErr := retry.OnError(c.retryBackoff, isRetryable, func() error {
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
			patchErr := retry.OnError(c.retryBackoff, isRetryable, func() error {
				return kube.Patch(ctx, &res, client.RawPatch(types.MergePatchType, []byte(`{}`)))
			})
			if patchErr != nil {
				if kerrors.IsNotFound(patchErr) {
					continue
				}
				return errors.Wrapf(patchErr, "cannot patch %s.%s %q", crd.Spec.Names.Kind, crd.Spec.Group, res.GetName())
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
		logr.Info("This client does not have permission to patch the CRD status", "crd", crdName)
		return nil
	}

	// Update CRD status to reflect that only the new storage version is stored
	if err := UpdateCRDStorageVersion(ctx, kube, c.retryBackoff, crdName, storageVersion); err != nil {
		return errors.Wrapf(err, "cannot update storage version for CRD %s", crdName)
	}
	logr.Debug("Storage version migration completed", "crd", crdName)
	return nil
}

// isRetryable returns true for transient API server errors that are safe to retry.
// It excludes permanent errors (NotFound, Forbidden, Unauthorized) and context
// cancellations so we don't retry on exceeded deadlines or terminal failures.
func isRetryable(err error) bool {
	return kerrors.IsInternalError(err) ||
		kerrors.IsServerTimeout(err) ||
		kerrors.IsTimeout(err) ||
		kerrors.IsTooManyRequests(err) ||
		kerrors.IsServiceUnavailable(err) ||
		kerrors.IsConflict(err)
}

// UpdateCRDStorageVersion updates the CRD status to reflect only the specified storage version.
// It retries the update with exponential backoff and verifies the update was successful.
func UpdateCRDStorageVersion(ctx context.Context, kube client.Client, retryBackoff wait.Backoff, crdName, storageVersion string) error {
	var crd extv1.CustomResourceDefinition
	// Update CRD status to reflect that only the new storage version is stored
	// Use retry for status updates as they can fail due to conflicts
	statusUpdateErr := retry.OnError(retryBackoff, isRetryable, func() error {
		// Re-fetch the CRD to get the latest version before patching
		if err := kube.Get(ctx, client.ObjectKey{Name: crdName}, &crd); err != nil {
			return errors.Wrapf(err, "cannot get CRD %s", crdName)
		}
		origCrd := crd.DeepCopy()
		crd.Status.StoredVersions = []string{storageVersion}
		return kube.Status().Patch(ctx, &crd, client.MergeFrom(origCrd))
	})
	if statusUpdateErr != nil {
		return errors.Wrapf(statusUpdateErr, "couldn't update %s crd", crdName)
	}
	return nil
}

// CheckCRDStatusUpdatePermission checks if the current client has permission to update/patch
// the status subresource of the specified CRD using SelfSubjectAccessReview.
func CheckCRDStatusUpdatePermission(ctx context.Context, kube client.Client, crdName string) (bool, error) {
	// Check for 'patch' verb on the status subresource
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
		if kerrors.IsForbidden(err) || kerrors.IsUnauthorized(err) || kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, errors.Wrap(err, "failed to create SelfSubjectAccessReview for verb patch")
	}

	return ssar.Status.Allowed, nil
}
