<!--
SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: Apache-2.0
-->

# A Plan Service for Upjet Providers

* Owner: Christopher Haar (@haarchri), Alper Ulucinar (@ulucinar), Sergen Yalcin (@sergenyalcin)
* Reviewers: Upjet Maintainers, Upjet Community
* Status: Draft

> **Everything introduced here is unstable.** The protobuf messages and the
> gRPC service, the package metadata we extend, the upjet APIs behind the
> server, and the provider subcommand are all alpha. We expect to break them
> while the shape settles, and we will not keep compatibility for them yet.
> Only the tooling described in this document and its companion should depend
> on them.

## Background

Crossplane users want the equivalent of `terraform plan`: a preview of what a
change would do to their external resources before they apply it. A change
might be an edited managed resource (MR), a Composition update that re-renders
MRs, or a provider upgrade. In every case the question is the same: *if this
spec is applied, what changes, and is any of it destructive?*

Today there is no way to ask an upjet provider that question. The diff
machinery exists, it runs on every reconcile, but it is only reachable by
actually reconciling, which means actually applying. Tooling that wants a
preview is left approximating: comparing `spec.forProvider` against
`status.atProvider` field by field. That approximation cannot see fields that
force replacement, values the provider computes, defaults the provider
injects, or `CustomizeDiff` logic. It tells you *something* changed; it cannot
tell you the change is one Terraform could only apply by destroying and
recreating your database, and therefore one the provider will refuse to
apply at all.

The accurate answer already lives inside every upjet provider. Upjet embeds
the upstream Terraform provider and calls its diff machinery in-process during
every reconcile: SDKv2 resources compute an `InstanceDiff` through the
resource schema's `Diff`, and Terraform Plugin Framework resources call
`PlanResourceChange`. Resources on the older Terraform CLI architecture go
further still: their `Observe` runs an actual `terraform plan` in a
workspace on every reconcile, then reduces the output to a single up-to-date
boolean. The field-level answer users want is computed today, and thrown
away. The same code that decides what `Update` will do can answer what
`Update` *would* do. It is just not exposed.

We want to expose it: let a client hand an upjet provider a desired resource
and its live state, and get back the provider's own diff (the action, the
field changes, and which of them force a replacement) without the provider
touching the cloud or a cluster.

### Prior Art: How the Crossplane CLI Drives Functions

`crossplane composition render` solved a similar problem for compositions.
Rather than requiring a control plane to see what a composition produces, the
CLI runs function packages locally as containers and talks to them over a
versioned gRPC protocol (`RunFunctionRequest` / `RunFunctionResponse`). The
package *is* the runtime; the CLI orchestrates.

Provider packages can work the same way for planning. A provider image
already contains everything needed to compute a diff: the embedded Terraform
provider, the resource schemas, and upjet's conversion machinery between CRD
shape and Terraform shape. What is missing is an entrypoint that serves diffs
over gRPC instead of reconciling, and a protocol to ask for them.

An accepted companion one-pager in crossplane/cli covers the client side:
`crossplane resource simulate` and `crossplane project simulate` commands
that run provider images as local diff servers and drive this protocol. This
document proposes the provider side in upjet.

## Goals

- Expose upjet's diff machinery as a gRPC plan service any client can drive.
- Cover upjet's execution modes incrementally. The in-process Terraform Plugin
  SDKv2 and Plugin Framework clients come first. Support for the Terraform CLI
  workspace architecture can be added later.
- Make the service stateless and credential-free. All state arrives in the
  request; the server never calls a cloud API and never reads a cluster.
- Report diffs in Crossplane terms, CRD field paths rather than Terraform
  attribute paths, so clients can render them against the user's YAML.
- Fail in a way the client can act on. When the server cannot diff something
  it has to say so clearly enough that the client can fall back to a raw diff
  and tell the user what happened, instead of the client seeing a crash or an
  error it cannot classify.

The supported client mode in `v1alpha1` is **offline**. The client does not
fetch resources from a cluster or refresh state from the cloud while preparing
a plan. Everything needed for the plan is provided from its offline inputs.
Live or interactive clients that can fetch additional state from a cluster or
cloud are planned for later. Some of the current limitations can be addressed
later with live or interactive clients.

It is not a goal to expose plans as an in-cluster API (a `Plan` CRD), to
refresh state from the cloud at plan time, or to implement diffing for
native (non-upjet) providers, though the protocol is deliberately shaped so
they can implement it themselves. See Alternatives.

## Proposal

Add a versioned diff protocol under `proto/diff/v1alpha1` and a
provider-agnostic implementation under `pkg/diffserver`, mirroring how
composition functions pair a protocol with a runtime. A provider serves the
protocol from an `internal diff-server` subcommand that clients like the
Crossplane CLI run locally.

The responsibility boundary is: the client prepares the inputs, routes
the request to the appropriate provider, and decides how to filter or present
the result. The diff server applies provider-specific planning logic, returns
the changes with the information needed by clients, and reports failures
through the typed error contract. If provider-side planning is not supported,
the client can fall back to a raw diff. In `v1alpha1`, the client operates
offline and does not fetch additional state from a cluster or cloud while
preparing the request.

A word on the names, because two of them sit next to each other. The thing we
run is the *diff server*: that is the package (`pkg/diffserver`), the
subcommand (`diff-server`) and the package capability (`DiffServer`). The
service it serves is `PlanService`, and what it returns is a plan. So the diff
server answers plan requests, and both words show up in this document on
purpose.

### The Protocol

A `PlanService` with one RPC:

```protobuf
service PlanService {
  // Plan computes a diff between the desired resource and the actual resource.
  rpc Plan(PlanRequest) returns (PlanResponse);
}

message PlanRequest {
  // The desired managed resource, as full JSON (apiVersion, kind, metadata,
  // spec). Always set; the protocol does not express deletion (see below).
  google.protobuf.Struct desired_resource = 1;

  // The actual resource, with status.atProvider populated. Must carry the
  // same apiVersion as the desired resource. Unset for a resource that
  // does not exist yet, which plans as a create.
  google.protobuf.Struct actual_resource = 2;

  // The Kubernetes object store that will be used to initialize an
  // in-memory Kubernetes API client.
  repeated google.protobuf.Struct kubernetes_object_store = 3;
}

// The kind of change the plan implies for the resource. The protocol does not
// express deletion, so there is no delete action.
enum Action {
  // The server did not determine an action. A client must treat this as an
  // unusable plan rather than as "nothing to do", because it is also what an
  // older client sees for an action added after it was built.
  ACTION_UNSPECIFIED = 0;

  // The desired resource matches the actual one: nothing meaningful changed.
  ACTION_NO_OP = 1;

  // The external resource does not exist yet and would be created.
  ACTION_CREATE = 2;

  // The external resource exists and would be updated in place.
  ACTION_UPDATE = 3;

  // The external resource exists but the change cannot be applied in place,
  // so it would be destroyed and recreated. See PlanResponse.replace_fields
  // for the fields that force the replacement.
  ACTION_REPLACE = 4;
}

message PlanResponse {
  Action action = 1;
  repeated FieldChange changes = 2;
  // repeated Diagnostic diagnostics = 5;
  string error = 6;
  google.protobuf.Timestamp computed_at = 7;
}

// Where a planned value came from. This is about provenance, not about
// whether the value changed: a field the desired resource declares has
// ORIGIN_DESIRED_STATE whether or not its value differs from the actual one.
enum Origin {
  // The server did not say where the planned value came from. A client must
  // treat this as unknown provenance rather than assuming either case,
  // because it is also what an older client sees for an origin added after
  // it was built.
  ORIGIN_UNSPECIFIED = 0;

  // The desired resource declares this field, so the planned value follows
  // from what the user wrote.
  ORIGIN_DESIRED_STATE = 1;

  // The provider planned this value for a field the desired resource does not
  // declare, for example from a schema default or a custom diff. For a
  // parameter, late initialization writes the value back into the managed
  // resource's spec.forProvider once the change is applied, so the user's own
  // object changes too.
  ORIGIN_PROVIDER = 2;
}

message FieldChange {
  // CRD path in the schema of the request's apiVersion,
  // e.g. spec.forProvider.deletionWindowInDays.
  string field = 1;

  // The value the external resource has now. Unset when the field does not
  // exist yet.
  FieldValue actual = 2;

  // The value the external resource will have once the change is applied.
  // This is not simply the value from PlanRequest.desired_resource: a
  // provider can plan a value the desired resource never declared, such as a
  // schema default. Unset when the change removes the field.
  FieldValue planned = 3;

  bool requires_replace = 4;

  // Where the planned value came from.
  Origin origin = 5;
}

// Why a FieldValue carries no concrete value. A value that is present is
// carried in FieldValue.value instead, so there is no "present" member here.
enum Absence {
  // The server did not say why the value is absent. A client must treat this
  // as an unusable value rather than as any particular reason, because it is
  // also what an older client sees for a reason added after it was built.
  ABSENCE_UNSPECIFIED = 0;

  // The value is known only after apply.
  ABSENCE_UNKNOWN = 1;

  // The value is sensitive and was redacted by the server.
  ABSENCE_SENSITIVE = 2;

  // The value comes from a reference (a Secret, or another resource via a
  // ref field) the server cannot resolve, so this plan could not evaluate
  // it. The reconciler will resolve it and may find a real change here.
  ABSENCE_UNRESOLVED = 3;
}

// The value on one side of a change. Unset, concrete (including explicit
// null), and each Absence reason are distinct states: an unset kind means
// there is no value on this side at all, whereas an absence means there is
// one but the server cannot put it in the plan.
message FieldValue {
  oneof kind {
    // The concrete value. google.protobuf.Value carries strings, numbers,
    // booleans, lists, objects, and explicit nulls, so null and "" differ.
    google.protobuf.Value value = 1;

    // Set when the server has a value for this side but cannot report it.
    Absence absence = 2;
  }
}
```

Field values are typed, not stringified, because every state Terraform's
diff distinguishes must stay distinguishable on the wire. A concrete value
travels as `google.protobuf.Value`, which keeps explicit `null` distinct
from `""` and `false` distinct from unset. A value known only after apply is
its own state, not an empty string. A `FieldValue` left unset means the
field does not exist on that side at all, which is how adding a field
differs from setting one to null. Collapsing these into strings would make
transitions like null to empty string, or empty string to known-after-apply,
unrenderable for clients, and fixing that after the protocol settles would
be a breaking change. Sensitivity lives inside the value for the same
reason: Terraform marks each side independently, and a field whose current
value is public but whose desired value is sensitive is a real transition a
single flag on the change could not express.

One caveat about what a client will actually see there today. The SDKv2 path
computes its diff in Terraform's flatmap representation, where every attribute
value is already a string by the time we get it: a number reads as `"30"` and
a boolean as `"true"`. We report those as they come rather than re-typing them
against the resource schema, so on that path the concrete values inside
`google.protobuf.Value` are strings even when the field is not. Clients must
not infer a field's type from the JSON shape of a plan. The wire format is
still the right one, the states above stay distinguishable and the Framework
path carries real types, but the promise the protocol makes is that every
state is distinguishable, not that the SDKv2 path has already recovered the
types Terraform threw away. Re-typing against the schema is possible later and
would not change the protocol.

A provider can serve a kind at more than one API version, and field paths
can differ between versions: v1beta1 may hold an object in a singleton list
where v1beta2 embeds it, so the same change is
`spec.forProvider.encryptionInfo[0].clientBroker` in one and
`spec.forProvider.encryptionInfo.clientBroker` in the other. Field paths in
the response follow the apiVersion the caller sent, so the diff matches the
YAML the user wrote.

**For now the desired and actual resources must use the same API version.**
A request where they differ is rejected with `INVALID_ARGUMENT`. Upjet already
generates conversions between served CRD versions through each resource's
conversion hub, but the `v1alpha1` offline plan flow does not run those
conversions. Supporting different versions therefore remains a limitation for
now. API conversion can be considered as part of future live or interactive
client support.

The request carries everything the server needs. Desired state is the MR the
user wants to apply. Prior state is reconstructed from the actual resource's
`status.atProvider`, the provider's own record of the external resource from
its last observation. Nothing else is consulted: no cluster reads, no cloud
reads. That one decision buys most of the properties we care about:

* **Credential-free.** The server needs no cloud credentials and no
  kubeconfig, and never resolves the credentials a `ProviderConfig`
  references. Anyone who can pull the provider image can compute a plan.
* **Read-only by construction.** There is no code path that could mutate an
  external system, because there is no code path that reaches one. The safety
  property is structural, not enforced by care.
* **Runs anywhere.** A laptop, CI, a cluster sidecar, anywhere the image
  runs and the caller can supply the two structs.

The cost is freshness: the diff is computed against the state the provider
observed at its last reconcile, not the cloud's state right now. That is the
same trade `terraform plan -refresh=false` makes, and the right default here.
Live MRs are freshly observed on every poll interval anyway, and the client
knows how stale `status.atProvider` is. A refresh mode could be added to the
protocol later without breaking it (see Alternatives).

`Plan` takes one resource per call. The companion CLI one-pager leaves the
request granularity to this document, so deciding it here: a project preview
is many independent plans, and the CLI already fans out concurrent gRPC
calls the way `crossplane composition render` does for functions. A batch
RPC would only move that loop server-side while muddying per-resource
failure semantics. Instead, a resource that cannot be planned reports the
failure in its own response (see The Engine), and every other resource plans
on.

One value in the `action` enum needs Crossplane words. `replace` is
Terraform's answer, destroy and recreate, but a Crossplane provider never
destroys an external resource to update it. When a reconcile computes a diff
that requires replacement, the provider refuses to apply it: the MR turns
unsynced and stays that way until a human intervenes, by recreating the
resource deliberately or reverting the change. A `replace` in a plan is
therefore not a prediction of automatic recreation. It is a warning that the
change cannot be applied as written, which is exactly the surprise a preview
exists to catch, and why `requires_replace` sits on every `FieldChange`
rather than being something a client has to derive. Carrying it per field and
not just per plan is deliberate: `ACTION_REPLACE` tells a user the change is
destructive, and the flagged fields tell them which line in their YAML to take
back.

How a client shows a replace is deliberately not the protocol's business: a
CI bot, a CLI, and a UI will render the same response differently, and a
wire contract that prescribes presentation ages badly. What is contract is
the meaning, and it is the one thing a renderer must not lose: replace warns
that the change cannot be applied as written, so a client must not book it
the Terraform way, as one destroy plus one add that will not happen. The
companion CLI one-pager shows one concrete rendering, a `[-/+]` marker,
replaces counted on their own in the plan summary, and a closing warning
that the MR would wedge unsynced; other clients can copy that, but only the
meaning binds them.

### Diff Filtering

The server filters entries that are not meaningful changes. This includes
attributes whose old and new values are the same, values that are only known
after apply, and Terraform bookkeeping entries such as collection sizes. A
change that requires replacement is still reported even when its value is
unknown.

Further filtering is a client-side concern. Provider defaults and other
provider-computed values can be real parts of the planned state, but different
clients may choose to present them differently. The server therefore reports
the changes it can compute and classifies each one with `Origin`:
`ORIGIN_DESIRED_STATE` when the desired resource declares the field, and
`ORIGIN_PROVIDER` when the value comes from the provider. This gives clients
enough information to apply their own filtering without the server making
presentation decisions.

The server does not maintain a list of fields to suppress. Such filtering can
vary between providers and clients, and dropping those fields on the server
would make that information unavailable to every client.

There is no `delete` action, and the request cannot ask for one. A diff of
desired against live state can only produce no-op, create, update, or
replace. A delete happens when the caller already knows the desired state is
absence, and then there is nothing left for the provider to compute: the
fields that go away are the live state the caller already has, and whether
the external resource is destroyed or orphaned is written on the MR itself,
in its deletion policy and management policies. So deletes are the client's
job. The companion CLI one-pager already handles them that way: a live
composed resource with no rendered counterpart is reported as a delete,
without calling a diff server. This also keeps the contract simple:
`desired_resource` is always required, and a request without one fails as
`INVALID_ARGUMENT`.

### Error Handling

Clients need to distinguish between invalid requests, unsupported diff
computation, and unexpected server errors. These failures are returned as
typed gRPC statuses, so clients do not need to parse error messages. In
particular, an unsupported diff has a defined reaction: fall back to a raw diff
and report that the provider did not compute it.

The status codes are:

* `INVALID_ARGUMENT` - the request is malformed or self-inconsistent. No
  desired resource, a manifest that does not decode into a type the provider
  knows, a desired and actual resource whose API versions differ. The client
  made a mistake and retrying the same request will not help.
* `NOT_FOUND` - this binary does not serve this resource. Either the API group
  belongs to another package (see Serving It) or no configuration is
  registered for the type. The client's routing sent it to the wrong server.
* `FAILED_PRECONDITION` - the resource is real and this is the right server,
  but its diff cannot be computed here. This is the fallback signal. The
  status carries a `PreconditionFailure` detail whose type is
  `DIFF_COMPUTATION_NOT_SUPPORTED` and whose subject is the GVK, so a client
  matches on that rather than on the message text. The message is still
  written for a human, because it ends up in front of one.
* `INTERNAL` - something went wrong that we did not anticipate. The client
  should treat it like the fallback case but it is a bug on our side, not an
  expected outcome.

Plan failures use gRPC statuses rather than `PlanResponse.error`. This keeps
failures scoped to the individual request and gives clients a typed result to
handle. The `error` field remains unused for now.

The server also recovers from panics in provider diff code. A panic is logged
and returned as `INTERNAL` for that request without taking down the diff
server or affecting other plan requests.

Provider API Groups discovery is deliberately kept out of the protocol. A client needs to
know which provider serves a resource before it knows which diff server to
start or call, and that information is already part of the provider package.

### Provider API Groups Discovery

Not every provider package is expected to support the plan service. Providers
that include the diff-server feature communicate this explicitly through the
existing package capability mechanism:

```yaml
apiVersion: meta.pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-aws-s3
spec:
  capabilities:
  - SafeStart
  - DiffServer
```

The client checks this capability before attempting to start a diff server. A
package without `DiffServer` is treated as not supporting provider-side
planning, allowing clients to fall back or report that limitation without
probing the provider runtime. Because the package metadata is the first
document in the same `package.yaml` used for API group discovery, this check
does not require another artifact or registry operation.

The capability answers whether a package supports the plan protocol; the CRDs
answer which resources that package serves. Keeping these separate avoids
encoding resource routing into the capability itself and lets the existing
`spec.capabilities` mechanism remain the package-level feature notice.

#### Resource Support

`DiffServer` indicates that a provider package supports the plan protocol. It
does not mean that every resource in the package can be planned.

Some Terraform resources depend on a fully initialized provider client or
provider meta during diff computation. Custom diff logic can also make API
calls for information that is not available locally. The offline diff server
does not use real credentials or allow outbound API requests, so these
resources cannot be planned by the provider in this mode. Providers should use
placeholder credentials where needed to prevent SDK credential discovery and
ensure outbound requests fail rather than using credentials from the host.

When a resource cannot be planned for one of these reasons, the server returns
`FAILED_PRECONDITION` with the `DIFF_COMPUTATION_NOT_SUPPORTED` detail. The
client can then fall back to a raw diff and report that the diff was not
computed by the provider. This keeps resource-level support separate from the
package-level `DiffServer` capability.

A client builds a group/kind-to-provider routing table from the CRDs shipped in
each provider's `package.yaml`. An xpkg stores `package.yaml` in its OCI base
layer, annotated `io.crossplane.xpkg: base`, so discovery does not require
pulling or starting the full provider image. The client resolves the image,
fetches only that layer, walks the CRD documents, and extracts `spec.group` and
`spec.names.kind`. The result is cached by immutable image digest, so a given
provider version only needs to be inspected once.

Both package extraction and parsing are existing machinery, not a new image
format, OCI client, or YAML parser. `crossplane-runtime` already provides
`xpkg.ExtractPackageYAML`, which locates and extracts the package layer using
the xpkg annotation and its existing fallbacks, as well as the package parser in
`pkg/xpkg/parser` for decoding `package.yaml`. The plan client can reuse both;
the discovery-specific work is extracting `spec.group` and `spec.names.kind`
from the parsed CRDs and building the routing table. If the full image has
already been pulled to run the diff server, the same package can instead be read
locally from `/package.yaml`.

The cost is small compared with pulling and starting providers. In measurements
against provider-family-aws, extracting group/kind information took about 180 ms
for a 208-CRD service package and 3.9 s for the 2,045-CRD monolith. So the
`package.yaml` processing isn't the performance bottleneck.  The main factor
affecting performance will be on the network side when the images are captured.
However, this is a step that must be carried out in any case.
More importantly, `provider-family-aws-ec2:v2.0.0` carries `package.yaml` in a
0.91 MB compressed layer while the full image is 242 MB. Across 30 AWS service
packages, the package layers total roughly 10--27 MB; pulling the full images
for discovery is about 7.2 GB because the ~236--237 MB provider binary layers
do not deduplicate across service packages.

The discovery flow is therefore independent of the diff-server lifecycle:

```text
provider image reference
        |
        v
resolve manifest and config
        |
        v
fetch xpkg base layer (or read local /package.yaml)
        |
        v
extract CRD group/kind
        |
        v
cache by image digest
        |
        v
group/kind -> provider routing table
        |
        v
start or reuse the matching diff server -> Plan
```

This treats the CRDs shipped in the package as the routing source of truth. The
running server is still authoritative about whether it can plan a request, so
a package whose CRDs and runtime wiring have drifted can route successfully and
still reject the plan.

### The Engine

`pkg/diffserver` provides the implementation, built from pieces upjet already
has:

* A **`Server`** implements the gRPC service. It is constructed from the
  provider's scheme, the API groups the package serves, and the provider
  configurations the provider's controllers are built from. It decodes an
  incoming resource into a registered API type, resolves its GVK to a
  Terraform resource type (handling both cluster-scoped and namespaced v2 API
  groups), looks up the resource's configuration, and hands off to the
  executor. Along the way it is the thing that says no: a request for an API
  group this package does not serve, a manifest that does not decode, a
  desired and an actual resource at different versions, a type with no
  registered configuration. Each of those gets the status code from Error Handling
  rather than a plan, and a version the binary does not serve at all is
  `INVALID_ARGUMENT` for the same reason the others are. In a
  provider-upgrade preview that last one is a finding by itself: the new
  provider no longer serves the version the resource uses.
* An **`Executor`** computes the diff using the execution mode selected by
  the resource configuration, following the same SDKv2 and Framework flags
  (`ShouldUseTerraformPluginSDKClient`,
  `ShouldUseTerraformPluginFrameworkClient`) used by the controller. The first
  supported execution modes are SDKv2 and Framework. A resource on an
  unsupported execution path returns `FAILED_PRECONDITION` with
  `DIFF_COMPUTATION_NOT_SUPPORTED`, so the client can use the same fallback
  behavior as for other resources that cannot be planned by the provider:
  * **SDKv2 resources:** extract the desired parameters via
    `GetMergedParameters`, apply the resource's Terraform conversions,
    reconstruct a Terraform `InstanceState` from the actual resource's
    observation (external name included, via the resource's configured
    `ExternalName` functions), then compute an `InstanceDiff` through the
    resource schema's `Diff`. This also runs `CustomizeDiff`, preserving
    provider-specific diff behavior.
  * **Framework resources:** build prior, config, and proposed
    `tftypes.Value`s from the same inputs and call `PlanResourceChange` on an
    in-process protov6 provider server, following the Framework plan path.
  * **Terraform CLI resources:** support for the Terraform CLI execution mode
    is planned for later. The existing workspace path already synthesizes
    Terraform configuration and state and runs `terraform plan` during
    reconciliation, so this can be considered as the basis for a future plan
    implementation. This execution mode is not part of the first supported
    scope and does not require a different wire protocol.
* A **diff converter** turns the Terraform result into the response: it
  derives the action, walks the resource schema to translate Terraform
  attribute paths into CRD paths (snake_case to lowerCamel, singleton blocks
  the CRD models as embedded objects, list indices, map keys), redacts
  sensitive values, and marks computed values as known-after-apply. Since both
  sides of a request are at one version today, the paths it produces are
  already the caller's; the mapping back from a hub version is part of the
  conversion work described above, not something the converter does now.

The executor needs a `terraform.Setup` to get provider metadata (and, for
Framework resources, a configured provider). It deliberately skips the
provider's normal credential resolution, but credential-free is not
configuration-free: some providers need a few provider-level values before
their schemas behave correctly. Those values come from three places, tried
in order:

* **The resource itself.** An AWS resource carries its region in
  `spec.forProvider.region`, so the setup reads it from the resource being
  planned. This is the common case and needs nothing from the caller.
* **The referenced ProviderConfig, sent by the caller.** Some values exist
  only in provider configuration: a GCP project ID, an Azure subscription
  and tenant, a custom endpoint for S3-compatible storage. Two resources can
  reference two ProviderConfigs with two different projects, so this is
  per-request data, not server configuration. That is what
  `kubernetes_object_store` is for. Rather than a field for the
  ProviderConfig alone, the request carries a small bag of Kubernetes objects
  that the server loads into an in-memory client, and the provider's own
  ProviderConfig resolution then runs against that client unchanged, including
  the cluster-scoped and namespaced flavours and their different reference
  rules. The caller fetches the ProviderConfig the resource references with
  its own RBAC and puts it in the bag. The server reads only non-credential
  fields from it and never resolves the credential references inside. Reusing
  the provider's resolution code rather than reading a dedicated field is what
  keeps the setup honest: whatever configuration affects a diff during a real
  reconcile affects it here the same way.
* **Defaults.** What is safe to default is a per-provider decision, made in
  the same per-provider wiring that constructs the server (see Serving It).

Initialization failures surface at two points, and the split matters. The
embedded Terraform provider initializes once at startup; if that fails, the
subcommand exits nonzero before serving, because a server that cannot
initialize should not accept a single request. Per-resource setup failures, a
GCP resource planned without its project ID say, fail only that plan, with a
message naming the missing input so the caller knows to include the
ProviderConfig.

Sensitive *inputs* deserve a note. A desired spec can reference sensitive
parameters stored in Secrets, a database password say. The easy thing would
be to let those fields drop silently out of the diff, and it would be wrong: a
plan that reports no-op because it could not see a password reads as "nothing
changes", while the reconciler, which can resolve the Secret, may find a real
change there.

The object store is what avoids that. Secrets go into the same bag as the
ProviderConfig, the caller having read them with its own RBAC, and the
provider's normal sensitive-parameter loading runs against the in-memory
client exactly as it does in a reconcile. So a referenced Secret the caller
included is resolved, and the diff sees the real value. What comes back in the
response is still redacted: an attribute Terraform marks sensitive is reported
with both sides absent and the reason `ABSENCE_SENSITIVE`, so the plan says
this field may have changed without printing either value. That is the case we
want, the change is visible and the secret is not.

`ABSENCE_UNRESOLVED` exists in the protocol for the other case, an input the
server genuinely could not evaluate, but nothing produces it yet. It is the
natural home for a Secret the caller did not or could not include, and for a
reference to another resource that does not exist yet, and we would rather
have the state reserved in an alpha protocol than have to add it later once
clients depend on the current shape. A client should treat it the way the
earlier draft described, as a caveat next to the plan counted apart from
no-ops, "2 fields could not be evaluated without their Secrets", rather than
as certainty.

How close is a plan to the reconcile that follows? A reconcile does more
than the Terraform diff: it resolves references to other resources, loads
sensitive parameters from Secrets, and the API server fills in schema
defaults at admission. Each of these has a home:

* Everything that needs no cluster reuses the reconcile's existing code paths:
  parameter merging, Terraform conversions, external-name functions, and the
  schema diff with `CustomizeDiff`. This keeps the plan path aligned with the
  corresponding reconcile logic.
* A future live or interactive client can close additional cluster-dependent
  gaps before the request is sent. These cluster-backed steps are not part of
  the `v1alpha1` offline client.
* Whatever nobody resolved, a reference on a resource that does not exist
  yet, or a Secret the caller did not include, should be reported as
  `ABSENCE_UNRESOLVED` rather than silently skipped. As noted above nothing
  produces that state yet, so today such an input simply does not appear in
  the diff. This is the one place the offline version can still be quieter
  than it should be, and closing it is a matter of the server noticing from
  the spec which parameters are secret-referenced and reporting the ones it
  could not evaluate.

The goal is to make any remaining gap between plan and reconcile explicit.
Fields are computed from the same underlying logic where possible, provided in
the request when additional input is available, or represented as unresolved
when the plan cannot evaluate them, with the gap above still to be addressed.

### Serving It

Providers gain an `internal diff-server` subcommand, hidden, the way
`crossplane internal render` is, because the stable interface is the
protocol, not the command line:

```console
$ docker run <provider-image> internal diff-server --address :9099
```

The subcommand constructs the server from the provider's existing wiring,
scheme, resource configurations, provider metadata and serves plaintext gRPC
until signalled. It takes a `--network` and an `--address`: TCP by default,
and a unix socket when the client would rather not open a port. The companion
CLI one-pager leaves the transport choice to this protocol, so fixing it here:
gRPC on a local endpoint, the transport function runtimes already use, rather
than a protobuf exchange over the container's stdin and stdout. It lets one
long-lived server answer many concurrent plans, and lets the CLI reuse the
container-and-port machinery it already has for functions. It starts in
single-digit seconds because it skips everything a reconciling provider needs:
no manager, no informers, no leader election, no credential resolution.

Adoption rides the machinery providers already generate their entrypoints
with. The generic parts live in upjet's `pkg/diffserver`: the gRPC server, the
plan service, the diff conversion, and a `Serve` that blocks until the context
is done. What is per-provider is a few dozen lines in the main template that
upjet's pipeline renders into every binary's `main`: build the scheme from the
provider's generated APIs, initialize the embedded Terraform provider(s),
construct the server from the provider configurations, hand it a Terraform
setup function that resolves no credentials, and serve. A provider adopts by
bumping its upjet dependency, extending that one template, and running `make
generate`, the monolith binary and every family (service-scoped) binary pick
the subcommand up from the same regeneration.

One detail matters for family providers. The provider configuration returns
the full resource set regardless of which scoped binary is running, so a
family binary would otherwise happily answer a plan request for a resource it
does not reconcile. Each binary therefore tells its server which API group the
package actually serves, and the server declines a request for any other one
instead of answering it from a configuration whose controllers do not run
there. The monolith serves them all. Client-side routing is derived
independently from the package CRDs, so a misrouted request fails rather than
quietly returning a plan from the wrong binary.

Transport security is deliberately out of scope for v1alpha1: the intended
deployment is a client-managed container listening on localhost, the same
trust model as `crossplane render`'s function runtimes. Anyone deploying a
diff server as a shared service needs to put TLS and authorization in front
of it; the server can grow TLS options when that use case is real.

### Client Flow

The planned flow for a live Crossplane CLI client (detailed in the companion
crossplane/cli one-pager) is below. This flow depends on cluster access and is
not part of the `v1alpha1` offline client:

1. Determine the provider images: the project's dependencies for `project
   simulate`, the providers installed on the target cluster for `resource
   simulate`, or an explicit override, which is how an upgrade is previewed
   with a provider version nothing runs yet.
2. Inspect each provider package's CRDs and build a group/kind-to-provider
   routing table, reusing cached results for image digests already seen.
3. Start diff servers as needed. Discovery does not require them to be running,
   so a client may start them eagerly or on first use.
4. For each resource to preview: fetch its live MR (if any), the
   ProviderConfig it references and any Secrets its spec references from the
   target cluster, put them in the object store, send them with the desired
   resource to the routed server, collect the response.
5. Handle the failures. A `FAILED_PRECONDITION` carrying
   `DIFF_COMPUTATION_NOT_SUPPORTED` means this resource has to be raw-diffed
   instead, and so does a provider package without the `DiffServer`
   capability at all, so the same fallback covers both. `NOT_FOUND` means the
   routing table sent it to the wrong server and is worth surfacing as a bug
   rather than as a finding about the user's resource.
6. Render a `terraform plan`-style diff and summary, marking the resources
   that were raw-diffed so that the user knows which parts of the preview the
   provider did not compute.

For a future live client, everything needed from a cluster (the live MR with
its `status.atProvider` and external-name annotation, for example) is fetched
by the client with the client's own RBAC. The diff server itself still holds no
cluster privileges and only sees state included by the caller.

The protocol does not care that the client is a CLI. The same servers and the
same RPCs serve CI checks on pull requests, a fleet-wide provider-upgrade
preview (run the *new* provider version's diff server against every existing
MR's spec and flag anything that is not a no-op), or an in-cluster service
that a UI queries.

### Predictability

A plan is a pure function of its request: two structs in, one diff out, no
I/O. Cost is one schema diff per call, the same computation a reconcile
performs before deciding to update, minus the observe. Memory is the loaded
provider schemas, which the provider binary carries anyway. There is no
workspace, no state file, and no per-request cleanup; a diff server can
compute plans for a whole project's resources in seconds, in parallel, and be
torn down.

## Alternatives Considered

### Discovering Resources From the Running Plan Server

The original design included a `GetInfo` RPC that reported the API groups a
running diff server supports. This keeps discovery simple for the client and
lets the server describe its own capabilities. The trade-off is that discovery
depends on provider startup: the client needs to pull and start each candidate
provider before it can build the routing table. This is more noticeable for
family providers, where each service package carries its own large provider
binary layer. Reading the package CRDs instead allows the same routing decision
to be made from the much smaller package metadata, before starting the
provider.

### Publishing Routing Metadata at Build Time

Providers could write their supported group/kind set into the package meta
object or OCI annotations at build time. Reading that metadata would be
cheaper than walking the CRDs, but it would only exist in packages built after
the change and in provider repositories that adopt it. Clients would still
need CRD inspection for existing, third-party, and community packages, leaving
two discovery paths to maintain for a small latency improvement.

### Kubernetes Discovery

An in-cluster client could derive routing from established CRDs and their
`ProviderRevision` ownership. That does not cover the workflows this design
needs to support without a cluster, in particular local and CI previews and
previewing a provider version that is not installed yet, so cluster state
cannot be the routing source.

### Discovering Resources From the Runtime Scheme

Another option is to expose the resources registered in the provider’s runtime
Scheme through a discovery RPC. Unlike maintaining a separate static
capability list, this derives the response from runtime state the provider
already has and reflects the GVKs actually registered by that binary. It could
also provide a natural foundation for exposing additional runtime capabilities
in the future.

This has a similar lifecycle trade-off to GetInfo: the provider must be
pulled and started before its scheme can be queried, so it does not help the
client decide which provider to start in the first place. For the routing
information needed here, the same group/kind information is already available
from the CRDs in package.yaml without initializing the runtime. A
scheme-backed discovery RPC could still be useful later if clients need
runtime information that cannot be represented by the package metadata.

### A `Plan` CRD Reconciled In-Cluster

Expose planning as a custom resource: a client creates a `Plan` embedding the
desired MR, a controller in the running provider computes the diff and writes
it to `status`. This was my starting point, and it has real attractions: it
reuses the provider's live credentials, so it can refresh from the cloud, and
RBAC gates who can plan.

We think the gRPC service is the better first primitive. The CRD path requires
a running control plane with the provider installed at the right version,
which rules out the most common workflows: previewing a change from a laptop
or CI before anything is deployed, and previewing a provider *upgrade*
(the cluster runs the old version; the plan must come from the new one). It
also makes every plan an etcd write, puts diffs of potentially sensitive
fields at rest in the cluster, and needs RBAC and lifecycle answers before
anything works. The gRPC service needs none of that, and a `Plan` CRD can be
layered on later as a thin in-cluster client of the same engine: the
executor does not care who calls it.

### Refreshing From the Cloud at Plan Time

Have the diff server observe the external resource before diffing, like
`terraform plan`'s default refresh. This gives fresher answers but costs the
properties that make the design simple: the server would need cloud
credentials, provider configuration resolution, and network reach, and
"read-only by construction" would become "read-only by policy". Live MRs are
re-observed every poll interval, so `status.atProvider` is rarely stale in
practice. If a refresh mode proves necessary, it fits the existing protocol
as an explicit opt-in: the request gains a credentials source and the
executor gains a refresh step (`RefreshWithoutUpgrade` for SDKv2,
`ReadResource` for Framework), without changing the default.

### Reimplementing the Diff in the Client

Compute the preview client-side by comparing `spec.forProvider` against
`status.atProvider`. No provider changes needed, and the companion CLI
proposal keeps exactly this as a clearly-labelled fallback for resources with
no diff server. But it cannot be the real answer: it has no schema, so it
cannot see replacement-forcing fields, computed values, provider defaults, or
`CustomizeDiff` behavior. The point of this proposal is that the accurate
diff already exists in the provider; copying an inaccurate one into every
client is the wrong direction.

### Native Providers

This is less an alternative considered than an extension point left open.
Nothing in the protocol is Terraform-specific: a desired resource and a live
resource go in, an action and field changes come out. A native (non-upjet)
provider that wants first-class previews can serve the same proto with its
own diff logic, it knows its schema, its defaults, and what forces
replacement in ways no generic engine can. This proposal deliberately
does not try to build that for them: without a Terraform schema to lean on,
an honest diff is the provider author's problem, and a generic
approximation here would reproduce the inaccurate previews this design
exists to replace. Until a native provider implements the contract, clients
degrade to a clearly-labelled client-side comparison (see the companion
crossplane/cli one-pager); once one does, its output is indistinguishable
from the upjet path. If that adoption happens, the protocol should graduate
from upjet to a neutral home (crossplane/crossplane-runtime, alongside the
function protocol), with upjet keeping its implementations. Starting in upjet keeps a
v1alpha1 protocol next to its first implementations while the shape
settles.
