<!--
SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->

# Storage Version Migration

Removing a deprecated api version of a CustomResourceDefinition (CRD) does not
rewrite objects that are already stored in etcd. For example, after changing a
CRD's storage version from `v1beta1` to `v1beta2`, existing objects may still be
stored as `v1beta1` until they are updated.

Kubernetes records every version that may still exist in storage in the CRD's
`status.storedVersions` field. An old version must not be removed from
`spec.versions` while it is still listed in `status.storedVersions`. Doing so
could leave the API server unable to decode objects that are still stored in
that version.

Upjet provides a `CRDMigrator` to complete this process. The migrator discovers
CRDs in its configured API group, rewrites their existing objects using the
current storage version, and then updates `status.storedVersions` to remove the
old versions.

## How the Migrator Works

A `CRDMigrator` is configured with a root API group, such as
`gcp.upbound.io`, and optionally a short group, such as `storage`.

When it runs, the migrator:

1. Lists the CRDs installed in the cluster.
2. Selects the CRDs whose `spec.group` is within the configured scope.
3. Finds the version marked with `storage: true` in each selected CRD.
4. Compares that version with the CRD's `status.storedVersions`. If the current
   storage version is already the only stored version, no migration is needed.
5. Lists the existing objects for CRDs that require migration, in batches of
   500, and patches each object so that the API server writes it back using the
   current storage version.
6. Updates the CRD's `status.storedVersions` field to contain only the current
   storage version.

List and patch operations are retried with exponential backoff when the API
server returns a transient error.

## Integrate the Migrator into a Provider

A common way to integrate the migrator is to add a dedicated `init` subcommand to the
provider binary. This subcommand runs the migration and exits without starting the
provider controllers, so it does not affect normal reconciliation.

### 1. Add the migration entry point

The migration entry point can be implemented as follows:

```go
// cmd/provider/sv_migrator.go
package provider

import (
    "context"

    "github.com/crossplane/crossplane-runtime/v2/pkg/errors"
    "github.com/crossplane/crossplane-runtime/v2/pkg/logging"
    ujconfig "github.com/crossplane/upjet/v2/pkg/config"
    authv1 "k8s.io/api/authorization/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/manager"
)

const rootGroup = "example.upbound.io"

// RunStorageVersionMigration migrates CRDs in the requested provider scope.
//
// When shortGroup is set, only CRDs in
// "<shortGroup>.example.upbound.io" are included. An empty shortGroup includes
// all CRDs under the root group and is intended for the monolithic provider
// binary.
func RunStorageVersionMigration(ctx context.Context, logr logging.Logger, mgr manager.Manager, shortGroup string) error {
    if err := authv1.AddToScheme(mgr.GetScheme()); err != nil {
        return errors.Wrap(err, "failed to add authv1 to scheme")
    }

    kubeClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
    if err != nil {
        return errors.Wrap(err, "failed to create kube client for storage version migration")
    }

    var opts []ujconfig.CRDMigratorOption
    if shortGroup != "" {
        opts = append(opts, ujconfig.WithShortGroup(shortGroup))
    }

    migrator := ujconfig.NewCRDMigrator(rootGroup, opts...)
    return errors.Wrap(migrator.Run(ctx, logr, kubeClient), "failed to run storage version migrator")
}
```

Replace `example.upbound.io` with the root API group for your provider.

### 2. Add the `init` subcommand to the main template

In `main.go.tmpl`, the `init` subcommand can be declared and
`RunStorageVersionMigration` can be called after the manager and scheme have been initialized,
before the provider controllers are set up.

For family providers:

- The `config` package does not need this path. It does not own versioned managed
  resource CRDs.
- For service packages, pass the generated short group, such as `storage` or `compute`.
- For the `monolith` package, pass an empty short group so that every
  CRD under the provider's root API group is included.

```go
    )

    _ = app.Command("core", "Run the provider controllers.").Default()
{{ if ne .Group "config" }}    initCmd := app.Command("init", "Run storage version migration and exit. Intended for use as an init container.")
    cmd := kingpin.MustParse(app.Parse(os.Args[1:]))
{{ else }}    kingpin.MustParse(app.Parse(os.Args[1:]))
{{ end }}

// ... manager and scheme setup ...

{{ if ne .Group "config" }}    if cmd == initCmd.FullCommand() {
        {{ if eq .Group "monolith" -}}
        kingpin.FatalIfError(providerinit.RunStorageVersionMigration(ctx, logr, mgr, ""), "Cannot run storage version migrator")
        {{- else -}}
        kingpin.FatalIfError(providerinit.RunStorageVersionMigration(ctx, logr, mgr, "{{ .Group }}"), "Cannot run storage version migrator")
        {{- end }}
        return
    }
{{ end }}
```

The `{{ .Group }}` template variable resolves to the short API group of the
package being generated. See
[Main Template Variables](main-template-variables.md) for the available
template variables.

> **Important:** Do not configure a `DeploymentRuntimeConfig` that runs the
> `init` subcommand against the family `config` package. That binary does not
> expose the subcommand.

### 3. Register the API extension types

The migrator lists `CustomResourceDefinition` objects, so
`apiextensions.k8s.io/v1` must be registered in the manager's scheme before the
`init` subcommand runs:

```go
kingpin.FatalIfError(apiextensionsv1.AddToScheme(mgr.GetScheme()), "Cannot add api-extensions APIs to scheme")
```

The migration entry point registers `authorization.k8s.io/v1` itself because
that API is needed only by the migration path.

## Run the Migration as an Init Container

The recommended deployment model runs the provider binary with the `init` subcommand in an
init container. Kubernetes waits for the init container to finish successfully
before starting the main provider container. This prevents the provider from
beginning normal reconciliation before the storage migration has completed.

The migration is safe to run again after it succeeds. CRDs whose
`status.storedVersions` already contains only the current storage version are
skipped.

### Configure a DeploymentRuntimeConfig

A `DeploymentRuntimeConfig` can be used to inject the migration init container
into the provider Deployment. Use the same package image for both the init
container and the provider container; no separate migration image is required.

```yaml
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: sv-migration
spec:
  serviceAccountTemplate:
    metadata:
      # Keep the ServiceAccount name stable across provider revisions so that
      # the ClusterRoleBinding below does not need to change after an upgrade.
      name: sv-migration-sa
  deploymentTemplate:
    spec:
      selector: {}
      template:
        spec:
          initContainers:
            - name: sv-migrator
              # Use the same image and version as the Provider package.
              image: xpkg.upbound.io/upbound/provider-gcp-storage:v3.0.0
              args:
                - init
              securityContext:
                runAsNonRoot: true
                runAsUser: 2000
                runAsGroup: 2000
                allowPrivilegeEscalation: false
                privileged: false
              resources:
                limits:
                  cpu: 500m
                  memory: 512Mi
                requests:
                  cpu: 100m
                  memory: 256Mi
```

The runtime configuration can then be referenced from the `Provider` resource:

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

The image in the init container must match the package version in the
`Provider` resource. Otherwise, the migration may run with CRD definitions or
migration behavior from a different provider release.

> **Important:** Do not use this runtime configuration with the family
> `config` provider package. Its binary does not expose the `init` subcommand,
> so the init container will fail.

### Grant the required RBAC permission

Crossplane's RBAC manager grants providers `get`, `list`, and `watch`
permissions for `customresourcedefinitions`. The migrator therefore does not
need additional permission to discover CRDs.

The provider does not receive `patch` permission for the
`customresourcedefinitions/status` subresource by default. The migrator needs
this permission to replace `status.storedVersions` after all objects have been
rewritten.

Without this permission, the object migration may complete, but the migrator
cannot finish the CRD migration. The old stored version remains recorded and
must not be removed from `spec.versions`.

The following `ClusterRole` can be bound to the stable ServiceAccount configured in
the `DeploymentRuntimeConfig`:

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
    # Use the namespace where Crossplane installs provider workloads.
    namespace: crossplane-system
```

If provider workloads run outside `crossplane-system`, adjust the namespace accordingly.

## Use the Standalone CLI

Upjet also provides a `crd-migrator` command-line tool. It can be used for a
manual, one-off migration or in environments where the init-container pattern
is not available.

### Dynamic mode

Dynamic mode discovers CRDs from the cluster and performs the same migration as
the in-process migrator.

Migrate every CRD under a root API group:

```shell
crd-migrator migrate --mode dynamic --root-group gcp.upbound.io
```

Limit the migration to one short group:

```shell
crd-migrator migrate --mode dynamic \
  --root-group gcp.upbound.io \
  --short-group storage
```

### Static mode

Static mode updates `status.storedVersions` for explicitly named CRDs. It does
**not** list or rewrite their existing objects.

Use static mode only when you have independently verified that no objects
remain stored in the old version. Removing an old entry from
`status.storedVersions` before completing the object migration can make it
unsafe to remove that version from the CRD specification.

Mappings can be provided directly:

```shell
crd-migrator migrate --mode static \
  --crd-names "bucketobjects.storage.gcp.upbound.io:v1beta2,buckets.storage.gcp.upbound.io:v1beta2"
```

Alternatively, they can be provided in a YAML file:

```shell
crd-migrator migrate --mode static --crd-file mappings.yaml
```

The mapping file uses CRD names as keys and target storage versions as values:

```yaml
bucketobjects.storage.gcp.upbound.io: v1beta2
buckets.storage.gcp.upbound.io: v1beta2
```

### Use a non-default kubeconfig

If needed, a non-default kubeconfig can be specified before the `migrate` subcommand:

```shell
crd-migrator --kubeconfig ~/.kube/staging migrate \
  --mode dynamic \
  --root-group gcp.upbound.io
```

## Related Documentation

- [Managing CRD Versions](managing-crd-versions.md) — deciding when to create a
  new CRD version and configuring conversions
- [Main Template Variables](main-template-variables.md) — variables available in
  `main.go.tmpl`
