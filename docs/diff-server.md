<!--
SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->

# The Diff/Plan Server

> **Alpha:** The protocol, package metadata, and provider subcommand may change
> without a deprecation period.
> The [design one-pager](../design/one-pager-upjet-plan-service.md) covers the
> rationale and API design. This guide focuses on the current integration and
> usage from a provider maintainer's or client author's perspective.

## Overview

Upjet-based providers already calculate diffs during reconciliation. SDKv2
resources use the schema's `Diff`, while Framework resources use
`PlanResourceChange`. Normally, that information is only used to decide what
reconciliation should do next.

The diff/plan server makes the same planning logic available to clients without
applying any changes. Given a desired resource and its current state, a client
can find out whether the result would be a create, update, replacement, or no
change.

Providers expose this through an internal `diff-server` subcommand and a single
gRPC method, `PlanService.Plan`, defined in `proto/diff/v1alpha1`. A request
contains the desired and actual resources, along with any Kubernetes objects
needed to resolve references (`kubernetes_object_store`). The response contains
an `Action` (`NO_OP`, `CREATE`, `UPDATE`, or `REPLACE`) and a list of
`FieldChange`s.

The server runs offline and keeps no state between requests. It uses the objects
supplied by the caller rather than connecting to a Kubernetes cluster, and it
does not resolve real cloud credentials or make cloud API calls.

## Provider integration

Enabling the diff/plan server involves a few changes to the provider:

1. **Subcommand registration.** The provider's Upjet dependency needs to include
   diff-server support, and `main.go.tmpl` needs a dispatch for
   `internal diff-server`. This dispatch belongs before any cluster
   initialization (`ctrl.GetConfig()` or `ctrl.NewManager()`), since the diff
   server uses an in-memory Kubernetes client populated from
   `kubernetes_object_store`.

2. **Offline Terraform setup.** The provider needs a `terraform.SetupFn` that
   can configure the embedded Terraform provider without looking up real
   credentials. This typically means supplying placeholder credentials that
   prevent the underlying SDK from attempting Application Default Credentials,
   instance metadata, or environment-based discovery, and using an HTTP
   transport that blocks outbound requests. Blocked requests should return
   `diffserver.NewDiffComputationNotSupportedError` so clients receive the
   appropriate gRPC status rather than a generic error.
   `provider-gcp/internal/clients/offline.go` is one example, using a static
   `access_token` and an egress-guard `http.RoundTripper`.

3. **Package capability.** `package/crossplane.yaml.tmpl` needs the `DiffServer`
   capability. Clients use this metadata to determine whether the package
   supports the protocol before starting the server. Packages without the
   capability are considered unsupported, without further probing.

4. **Code generation.** After `make generate`, the subcommand is included in
   both the monolith and service-scoped provider binaries.

The `DiffServer` capability indicates protocol support at the package level. It
does not guarantee that every resource can be planned offline; some resources
may still return a not-supported result.

Example integration in a family provider: [provider-upjet-gcp](https://github.com/crossplane-contrib/provider-upjet-gcp/pull/1044)
Example integration in a non-family provider: [provider-upjet-azuread](https://github.com/crossplane-contrib/provider-upjet-azuread/pull/383)

## Supported behavior

- **Terraform execution modes:** Both Terraform Plugin SDKv2 and Terraform
  Plugin Framework resources are supported. Workspace-based Terraform CLI
  execution is not supported yet. Those resources return `FAILED_PRECONDITION`
  with `DIFF_COMPUTATION_NOT_SUPPORTED`, which clients can use as a fallback
  signal.
- **Resource scope:** Cluster-scoped and namespaced resources are supported.
- **API versions:** Any served CRD version can be used, provided the desired and
  actual resources use the same version. Mixed-version requests return
  `INVALID_ARGUMENT`. Generated Upjet API version conversions are not run as
  part of the offline `v1alpha1` flow.
- **Secrets:** Secret-backed parameters can be resolved from
  `kubernetes_object_store`. The server checks whether the referenced Secret
  exists without passing its decoded value to Terraform. Resolved fields are
  reported with `ABSENCE_SENSITIVE`, while unresolved references are reported
  with `ABSENCE_UNRESOLVED`. Neither case exposes the Secret value in the
  response.
- **`spec.initProvider`:** The usual merge-on-create and ignore-on-update
  behavior is preserved.

## Known limitations

- **No cross-version planning.** Desired and actual resources must have the same
  API version. Cross-version API conversion is outside the current offline mode.
- **No Terraform CLI support.** Resources using workspace-based Terraform CLI
  execution cannot currently be planned by the diff/plan server.
- **No state refresh.** Planning uses `status.atProvider` from the actual
  resource, as last recorded by reconciliation. It does not fetch current cloud
  state, similar to `terraform plan -refresh=false`. The caller is responsible
  for providing a sufficiently recent actual resource.
- **Service-scoped binaries only handle their own API group.** Requests for a
  different group return `NOT_FOUND`. The monolith can handle all groups.
- **Plaintext gRPC.** Transport security is not part of `v1alpha1`. The expected
  setup is a local process managed by the client, following the same trust model
  used by `crossplane render` for function runtimes.

## Examples

### Starting the server

The diff/plan server is available through the provider binary's
`internal diff-server` subcommand:

```console
$ ./provider internal diff-server --address :9099
```

The server supports gRPC reflection, so `grpcurl` can be used to inspect
the service and send requests:

```console
$ grpcurl -plaintext localhost:9099 list
$ grpcurl -plaintext localhost:9099 describe upjet.diff.v1alpha1.PlanService
```

### Updating an existing resource

The following request plans a storage class change for an existing GCP
Storage Bucket. The desired resource specifies `NEARLINE`, while the
actual resource still has `STANDARD`.

```json
{
  "desired_resource": {
    "apiVersion": "storage.gcp.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {
      "name": "example-bucket"
    },
    "spec": {
      "forProvider": {
        "location": "US",
        "storageClass": "NEARLINE"
      }
    }
  },
  "actual_resource": {
    "apiVersion": "storage.gcp.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {
      "name": "example-bucket"
    },
    "spec": {
      "forProvider": {
        "location": "US",
        "storageClass": "STANDARD"
      }
    },
    "status": {
      "atProvider": {
        "id": "example-bucket",
        "storageClass": "STANDARD"
      }
    }
  },
  "kubernetes_object_store": []
}
```

Save the request as `request.json` and send it to the server:

```console
$ grpcurl -plaintext -d @ localhost:9099 upjet.diff.v1alpha1.PlanService/Plan < request.json
```

The response reports `ACTION_UPDATE` with a `FieldChange` for
`spec.forProvider.storageClass`. The actual value is `STANDARD`, the
planned value is `NEARLINE`, and `requires_replace` is `false`.

### Creating a resource

When `actual_resource` is omitted, the server treats the resource as
not yet existing.

For example, a request containing only a desired Bucket:

```json
{
  "desired_resource": {
    "apiVersion": "storage.gcp.upbound.io/v1beta1",
    "kind": "Bucket",
    "metadata": {
      "name": "example-bucket"
    },
    "spec": {
      "forProvider": {
        "location": "US",
        "storageClass": "STANDARD"
      }
    }
  },
  "kubernetes_object_store": []
}
```

The response reports `ACTION_CREATE`, along with the field changes
associated with creating the resource.

No cloud resource is created. The server only computes the plan using
the supplied desired state.

### Replacing a resource

Some attributes require resource replacement when changed.
The diff/plan server reports these changes with `ACTION_REPLACE` and marks
the relevant fields with `requires_replace: true`.

For example, changing an immutable attribute on an existing resource
may produce a response equivalent to:

```json
{
  "action": "ACTION_REPLACE",
  "changes": [
    {
      "field": "spec.forProvider.exampleImmutableField",
      "requires_replace": true
    }
  ]
}
```

This is an illustrative response rather than a complete protobuf JSON
response. The exact fields and values depend on the resource's
schema and the supplied state.

A replacement is reported when the underlying provider plan requires
it. The server does not apply the replacement or delete the existing
resource.

### Working with Secrets

Secret-backed parameters can be included in a plan without requiring
access to a Kubernetes cluster.

Consider a desired resource that references a Secret:

```json
{
  "spec": {
    "forProvider": {
      "passwordSecretRef": {
        "name": "example-credentials",
        "namespace": "default",
        "key": "password"
      }
    }
  }
}
```

The corresponding Secret can be supplied through
`kubernetes_object_store`:

```json
{
  "kubernetes_object_store": [
    {
      "apiVersion": "v1",
      "kind": "Secret",
      "metadata": {
        "name": "example-credentials",
        "namespace": "default"
      },
      "data": {
        "password": "c2VjcmV0LXZhbHVl"
      }
    }
  ]
}
```

The Secret uses the same representation returned by the Kubernetes API,
with base64-encoded values in `data`. The server checks whether the Secret
reference can be resolved from the supplied objects. The decoded Secret value is
not used in the diff computation.

Resolved fields are reported with `ABSENCE_SENSITIVE`, with both the actual and
planned values omitted from the response. If the referenced Secret is
unavailable, the field is reported with `ABSENCE_UNRESOLVED` instead.

This allows clients to distinguish resolved and unresolved Secret references
without exposing sensitive values.

### Handling unsupported diff computation

Not every resource can be planned offline, even when its provider
advertises the `DiffServer` capability.

For example, resources using the Terraform CLI execution mode are not
supported. Other resources may depend on operations that cannot be
performed without external access.

In these cases, the server returns the gRPC status
`FAILED_PRECONDITION`, with `DIFF_COMPUTATION_NOT_SUPPORTED` indicating
that the diff could not be computed.

Clients can use this status to distinguish unsupported computations
from other failures and fall back to another approach when available.

The `DiffServer` package capability only indicates that the provider
supports the protocol. It does not guarantee that every resource can
be planned successfully.
