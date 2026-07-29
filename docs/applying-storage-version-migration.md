<!--
SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->

# Applying Storage Version Migration

Some provider releases remove the deprecated api version for one or more CRDs.
When that happens, existing objects need to be migrated before the previous
storage version can be removed safely.

This guide describes how to perform that migration using an init container
during a provider upgrade.

> **Note**: These steps are only relevant for releases whose release notes
> mention a storage version migration. Most provider upgrades do not require
> any additional migration steps.

## Do I Need This?

The provider's release notes are the best place to check whether a storage
version migration is required.

If a migration is recommended, following the steps in this guide ensures that
existing objects are re-persisted using the current storage version before a
future release removes the previous one.

Skipping the migration will not usually affect the current upgrade, but it may
prevent a later release from safely dropping the old storage version because
the affected CRDs will continue to report it in `status.storedVersions`.

## Prerequisites

Before starting, it helps to have:

- `kubectl` access to the cluster where the provider is installed.
- A provider managed by Crossplane (for example through the Helm chart or the
  package manager).
- A healthy provider installation.

## Step 1 — Grant Permission to Update CRD Status

The init container updates `status.storedVersions` after the migration
completes. Crossplane's default RBAC does not include this permission, so an
additional `ClusterRole` and `ClusterRoleBinding` are needed.

> **Note**: The namespace in the `ClusterRoleBinding` should match the namespace
> where Crossplane is installed (typically `crossplane-system` or
> `upbound-system`).

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: provider-crd-status-patcher
rules:
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions/status"]
    verbs: ["patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: provider-crd-status-patcher
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: provider-crd-status-patcher
subjects:
  - kind: ServiceAccount
    name: sv-migration-sa
    namespace: crossplane-system
```

The manifest can be applied with:

```shell
kubectl apply -f rbac.yaml
```

## Step 2 — Apply the DeploymentRuntimeConfig

The following `DeploymentRuntimeConfig` adds an init container to the provider
`Deployment`. The init container performs the migration before the main
provider container starts.

> **Important**: Make sure the `image` matches the provider version you are
> upgrading to. The init container and the provider should use the same image.

> **Note**: This `DeploymentRuntimeConfig` is intended for versioned providers.
> It should not be used with `*-family` or `config` provider packages, since
> those packages do not support the `init` subcommand.

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: sv-migration
spec:
  serviceAccountTemplate:
    metadata:
      name: sv-migration-sa
  deploymentTemplate:
    spec:
      selector: {}
      template:
        spec:
          initContainers:
            - name: sv-migrator
              image: xpkg.upbound.io/upbound/provider-gcp-storage:v3.0.0
              args:
                - init
```

The manifest can be applied with:

```shell
kubectl apply -f deploymentruntimeconfig.yaml
```

## Step 3 — Reference the DeploymentRuntimeConfig

The `Provider` resource can then be updated to reference the
`DeploymentRuntimeConfig` while pointing to the new provider package version.

```yaml
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-gcp-storage
spec:
  package: xpkg.upbound.io/upbound/provider-gcp-storage:v3.0.0
  runtimeConfigRef:
    name: sv-migration
```

Apply the updated resource:

```shell
kubectl apply -f provider.yaml
```

During the upgrade, Crossplane restarts the provider `Deployment`. The init
container runs first, completes the migration, and exits. The main provider
container then starts as usual.

## Step 4 — Verify the Migration

Once the provider has restarted, it can be helpful to confirm that the init
container completed successfully.

```shell
kubectl get pods -n crossplane-system -l pkg.crossplane.io/revision=provider-gcp-storage
kubectl logs -n crossplane-system <pod-name> -c sv-migrator
```

Migration time depends on the number of existing managed resources. Smaller
installations may finish in seconds, while larger clusters can take several
minutes.

The updated storage version can also be verified directly:

```shell
kubectl get crd buckets.storage.gcp.upbound.io   -o jsonpath='{.status.storedVersions}'
# Expected: ["v1beta2"]
```

If the init container reports an error, its logs are usually the best place to
start troubleshooting. Missing or incorrectly configured RBAC is the most
common cause.

## Step 5 — Clean Up

The migration only needs to run once for a given storage version change.

After the provider is healthy and the migration has been verified, the
`DeploymentRuntimeConfig` and the temporary RBAC resources are no longer needed
and can be removed.

Crossplane restarts the provider one final time without the init container.

---

If multiple sub-providers are being upgraded (for example `provider-gcp-storage`
and `provider-gcp-compute`), the same process applies to each provider. Each
sub-provider uses its own `DeploymentRuntimeConfig`, while the RBAC resources
can be shared.
