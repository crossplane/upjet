<!--
SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: Apache-2.0
-->

# A Plan Service for Upjet Providers

* Owner: Christopher Haar (@haarchri), Alper Ulucinar (@ulucinar), Sergen Yalcin (@sergenyalcin)
* Reviewers: Upjet Maintainers, Upjet Community
* Status: Draft

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

I want to expose it: let a client hand an upjet provider a desired resource
and its live state, and get back the provider's own diff (action, field
changes, replacement info, diagnostics) without the provider touching the
cloud or a cluster.

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
that run provider images as local plan servers and drive this protocol. This
document proposes the provider side in upjet.

## Goals

- Expose upjet's diff machinery as a gRPC plan service any client can drive.
- Support every execution mode upjet has: the in-process Terraform Plugin
  SDKv2 and Plugin Framework clients, and the Terraform CLI workspace
  architecture that predates them, so a plan covers every resource a
  provider serves.
- Make the service stateless and credential-free. All state arrives in the
  request; the server never calls a cloud API and never reads a cluster.
- Report diffs in Crossplane terms, CRD field paths rather than Terraform
  attribute paths, so clients can render them against the user's YAML.

It is not a goal to expose plans as an in-cluster API (a `Plan` CRD), to
refresh state from the cloud at plan time, or to implement diffing for
native (non-upjet) providers, though the protocol is deliberately shaped so
they can implement it themselves. See Alternatives.

## Proposal

Add a versioned plan protocol under `proto/plan/v1alpha1` and a
provider-agnostic implementation under `pkg/plan`, mirroring how composition
functions pair a protocol with a runtime. A provider serves the protocol from
a `plan-server` subcommand that clients like the Crossplane CLI run locally.

### The Protocol

A `PlanService` with one RPC:

```protobuf
service PlanService {
  // Plan computes a diff between the desired resource and the live resource.
  rpc Plan(PlanRequest) returns (PlanResponse);
}

message PlanRequest {
  // The desired managed resource, as full JSON (apiVersion, kind, metadata,
  // spec). Always set; the protocol does not express deletion (see below).
  google.protobuf.Struct desired_resource = 1;

  // The live resource, with status.atProvider populated. Must carry the
  // same apiVersion as the desired resource. Unset for a resource that
  // does not exist yet, which plans as a create.
  google.protobuf.Struct live_resource = 2;

  // The ProviderConfig the desired resource references, if the caller can
  // supply it. Optional. The server reads non-credential fields only (a
  // project ID, a subscription ID, custom endpoints); credential
  // references are never resolved.
  google.protobuf.Struct provider_config = 3;
}

message PlanResponse {
  string action = 1;                  // no-op | create | update | replace
  repeated FieldChange changes = 2;
  bool requires_replace = 3;
  repeated string replace_fields = 4; // CRD paths, e.g. spec.forProvider.region
  repeated Diagnostic diagnostics = 5;
  string error = 6;
  google.protobuf.Timestamp computed_at = 7;
}

message FieldChange {
  // CRD path in the schema of the request's apiVersion,
  // e.g. spec.forProvider.deletionWindowInDays.
  string field = 1;

  FieldValue current = 2; // Unset when the field does not exist yet.
  FieldValue desired = 3; // Unset when the change removes the field.
  bool requires_replace = 4;
}

// The value on one side of a change. Unset, concrete (including explicit
// null), unknown, sensitive, and unresolved are five distinct states.
message FieldValue {
  oneof kind {
    // The concrete value. google.protobuf.Value carries strings, numbers,
    // booleans, lists, objects, and explicit nulls, so null and "" differ.
    google.protobuf.Value value = 1;

    // The value is known only after apply.
    bool unknown = 2;

    // The value is sensitive and was redacted by the server.
    bool sensitive = 3;

    // The value comes from a reference (a Secret, or another resource via
    // a ref field) the server cannot resolve, so this plan could not
    // evaluate it. The reconciler will resolve it and may find a real
    // change here.
    bool unresolved = 4;
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

A provider can serve a kind at more than one API version, and field paths
can differ between versions: v1beta1 may hold an object in a singleton list
where v1beta2 embeds it, so the same change is
`spec.forProvider.encryptionInfo[0].clientBroker` in one and
`spec.forProvider.encryptionInfo.clientBroker` in the other. The contract is
simple: field paths follow the apiVersion the caller sent, so the diff
always matches the YAML the user wrote. Desired and live resource must have
the same apiVersion. The version handling behind that is the server's job,
not the client's, and the binary already has everything for it: upjet
generates each resource's Terraform mappings against one version (the
conversion hub, usually the newest), and generates conversion functions
between the hub and every older served version, the same functions the
provider's conversion webhook uses in-cluster. The plan server converts an
incoming resource to the hub, plans there, and translates the field paths
back to the version the caller sent (see The Engine).

The request carries everything the server needs. Desired state is the MR the
user wants to apply. Prior state is reconstructed from the live resource's
`status.atProvider`, the provider's own record of the external resource from
its last observation. Nothing else is consulted: no cluster reads, no cloud
reads. That one decision buys most of the properties I care about:

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
exists to catch, and why `requires_replace` and `replace_fields` are
first-class in the response rather than derivable details.

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

There is no `delete` action, and the request cannot ask for one. A diff of
desired against live state can only produce no-op, create, update, or
replace. A delete happens when the caller already knows the desired state is
absence, and then there is nothing left for the provider to compute: the
fields that go away are the live state the caller already has, and whether
the external resource is destroyed or orphaned is written on the MR itself,
in its deletion policy and management policies. So deletes are the client's
job. The companion CLI one-pager already handles them that way: a live
composed resource with no rendered counterpart is reported as a delete,
without calling a plan server. This also keeps the contract simple:
`desired_resource` is always required, and a request without one fails as
`INVALID_ARGUMENT`.

Provider API Groups discovery is deliberately kept out of the protocol. A client needs to
know which provider serves a resource before it knows which plan server to
start or call, and that information is already part of the provider package.

### Provider API Groups Discovery

Not every provider package is expected to support the plan service. Providers
that include the plan-server feature communicate this explicitly through the
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

The client checks this capability before attempting to start a plan server. A
package without `DiffServer` is treated as not supporting provider-side
planning, allowing clients to fall back or report that limitation without
probing the provider runtime. Because the package metadata is the first
document in the same `package.yaml` used for API group discovery, this check
does not require another artifact or registry operation.

The capability answers whether a package supports the plan protocol; the CRDs
answer which resources that package serves. Keeping these separate avoids
encoding resource routing into the capability itself and lets the existing
`spec.capabilities` mechanism remain the package-level feature notice.

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
already been pulled to run the plan server, the same package can instead be read
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

The discovery flow is therefore independent of the plan-server lifecycle:

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
start or reuse the matching plan server -> Plan
```

This treats the CRDs shipped in the package as the routing source of truth. The
running server is still authoritative about whether it can plan a request, so
a package whose CRDs and runtime wiring have drifted can route successfully and
still reject the plan.

### The Engine

`pkg/plan` provides the implementation, built from pieces upjet already has:

* A **`Server`** implements the gRPC service. It is constructed from the
  provider's scheme, its root API group, and its generated resource
  configuration map, the same `map[string]*config.Resource` the provider's
  controllers are built from. It resolves an incoming resource's GVK to its
  Terraform resource type (handling both cluster-scoped and namespaced v2 API
  groups), looks up the resource's configuration, and hands off to the
  executor. When the request arrives at an older served version, the server
  first converts it to the resource's hub version. The machinery for that is
  already in the binary: upjet generates `ConvertTo`/`ConvertFrom` on every
  older version's types, both thin wrappers around upjet's
  `conversion.RoundTrip`, which applies the conversions declared in the
  resource's configuration. In-cluster the conversion webhook calls these
  same functions; the plan server calls them directly, after registering the
  provider's conversions at startup the way the provider's `main` does. A
  version the binary does not serve at all fails that plan in
  `PlanResponse.error`; in a provider-upgrade preview that is a finding by
  itself, the new provider no longer serves the version the resource uses. Plan failures are reported in `PlanResponse.error` rather than as
  gRPC errors, so one resource that cannot be planned does not look like a
  broken server to a client fanning out calls.
* An **`Executor`** computes the diff, choosing the path from the resource's
  configuration, the same flags (`ShouldUseTerraformPluginSDKClient`,
  `ShouldUseTerraformPluginFrameworkClient`) that select the controller
  implementation today, so the plan path always matches the reconcile path:
  * **SDKv2 resources:** extract the desired parameters via
    `GetMergedParameters`, apply the resource's Terraform conversions,
    reconstruct a Terraform `InstanceState` from the live resource's
    observation (external name included, via the resource's configured
    `ExternalName` functions), then compute an `InstanceDiff` through the
    resource schema's `Diff`, which runs `CustomizeDiff`, so
    provider-specific diff logic (like AWS tag handling) behaves exactly as
    it would in a real reconcile.
  * **Framework resources:** build prior, config, and proposed
    `tftypes.Value`s from the same inputs and call `PlanResourceChange` on an
    in-process protov6 provider server, the identical call Terraform itself
    makes to plan.
  * **Terraform CLI resources:** the workspace architecture already has the
    pieces. Upjet synthesizes `main.tf.json` from the desired parameters and
    `terraform.tfstate` from the live observation, and runs `terraform plan`
    on every `Observe` today. The executor reuses that synthesis, runs
    `terraform plan -refresh=false -out=tfplan`, and reads the plan file
    back with `terraform show -json tfplan`. The JSON plan's `resource_changes` carry the
    same information the in-process paths extract, actions, before and
    after values, `replace_paths`, and feed the same conversion and the
    same response. It costs a process start and file I/O per plan instead of
    an in-process call, but it is identical on the wire: a client cannot tell
    which path served it. The in-process paths can land first and this one
    follow, with no protocol or client change.
* A **diff converter** turns the Terraform result into the response: it
  derives the action, walks the resource schema to translate Terraform
  attribute paths into CRD paths (snake_case to lowerCamel, singleton blocks,
  list indices, map keys, for both SDKv2 and Framework schemas), redacts
  sensitive values, and marks computed values as known-after-apply. Those
  paths come out in the hub version, so for a request that arrived at an
  older version the converter maps each path back. Nobody has to invert the
  conversion code for that: the conversions are declared as data in the
  resource's configuration, each one names the field paths it touches, and
  putting a named path back is mechanical. A kafka `Cluster` whose v1beta1
  wraps in singleton lists what the v1beta2 hub embeds declares the list
  paths `encryptionInfo` and `encryptionInfo[*].encryptionInTransit`, and a
  v1beta1 request walks through like this:

  ```
  caller sends (v1beta1): spec.forProvider.encryptionInfo[0].encryptionInTransit[0].clientBroker
  converted to hub:       spec.forProvider.encryptionInfo.encryptionInTransit.clientBroker
  terraform diffs:        encryption_info.0.encryption_in_transit.0.client_broker
  hub CRD path:           spec.forProvider.encryptionInfo.encryptionInTransit.clientBroker
  response (v1beta1):     spec.forProvider.encryptionInfo[0].encryptionInTransit[0].clientBroker
  ```

  The last step re-inserts the `[0]` at the two declared paths, so the field
  in the response is the exact line in the caller's YAML. One conversion
  kind declares no paths, the hand-written custom converter; a resource that
  needs one between the request's version and the hub fails the plan with an
  error naming the hub version to plan at, rather than answering with paths
  the caller's YAML does not have. It also
  filters changes the user did not author: fields injected by the provider's
  configuration injector and Crossplane's own system tags (`crossplane-kind`,
  `crossplane-name`, `crossplane-providerconfig`). Without that filter every
  plan for a fresh resource reports noise the user cannot act on.

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
  per-request data, not server configuration. That is what the optional
  `provider_config` request field is for: the caller fetches the
  ProviderConfig the resource references, with its own RBAC, and includes
  it. The server reads only non-credential fields from it and never resolves
  the credential references inside.
* **Defaults.** What is safe to default is a per-provider decision, made in
  the same `runPlanServer` wiring that constructs the server (see Serving
  It).

Initialization failures surface at two points, and the split matters. The
embedded Terraform provider initializes once at startup; if that fails, the
subcommand exits nonzero before serving. Per-resource setup
failures, a GCP resource planned without its project ID say, fail only that
plan, in `PlanResponse.error`, with a message naming the missing input so
the caller knows to include the ProviderConfig.

Sensitive *inputs* deserve a note. A desired spec can reference sensitive
parameters stored in Secrets, and the server has no Kubernetes client to
resolve them. It would be easy to let those fields silently drop out of the
diff, and wrong: a plan that reports no-op because it could not see a
password reads as "nothing changes", while the reconciler, which can resolve
the Secret, may find a real change there. So unresolved inputs are
first-class in the response. The server knows from the spec which parameters
are secret-referenced, and reports every one it could not evaluate as a
`FieldChange` whose desired value is `unresolved`, a state of its own in
`FieldValue`, distinct from known-after-apply. Clients present it as a
caveat next to the plan and count it apart from no-ops: "2 fields could not
be evaluated without their Secrets" instead of pretended certainty. If a
real need shows up, the request can later gain an explicit way for callers
that can read the Secrets to supply the resolved values; that is additive.

How close is a plan to the reconcile that follows? A reconcile does more
than the Terraform diff: it resolves references to other resources, loads
sensitive parameters from Secrets, and the API server fills in schema
defaults at admission. Each of these has a home:

* Everything that needs no cluster runs the reconcile's own code: parameter
  merging, Terraform conversions, external-name functions, the schema diff
  with `CustomizeDiff`. These cannot diverge.
* The client has the cluster, so it closes these gaps upfront. Resolved
  references are already persisted in a live resource's spec, so the client
  merges them into the desired resource before sending, the same way it
  already merges the live external-name and status. Schema defaults come
  from a server-side dry-run of the desired resource, the same trick the
  companion CLI proposal uses for its cluster diff.
* Whatever nobody resolved, a reference on a resource that does not exist
  yet, or a Secret value, is reported as `unresolved`, never silently
  skipped.

So a plan never quietly diverges from a reconcile. Every field is either
computed by the same code the reconcile runs, resolved upfront by the
client, or marked as one the plan could not see.

### Serving It

Providers gain an `internal plan-server` subcommand, hidden, the way
`crossplane internal render` is, because the stable interface is the
protocol, not the command line:

```console
$ docker run <provider-image> internal plan-server --port 50051
```

The subcommand constructs the `Server` from the provider's existing wiring,
scheme, resource configuration map, provider metadata and serves plaintext
gRPC on the given port until signalled. The companion CLI one-pager leaves
the transport choice to this protocol, so fixing it here: gRPC over a
localhost TCP port, the transport function runtimes already use, rather than
a protobuf exchange over the container's stdin and stdout. A port lets one
long-lived server answer many concurrent plans, and lets the CLI reuse the
container-and-port machinery it already has for functions. It starts in single-digit seconds
because it skips everything a reconciling provider needs: no manager, no
informers, no leader election, no credential resolution.

Adoption rides the machinery providers already generate their entrypoints
with. The generic parts live in upjet's `pkg/plan`, server, executor, diff
conversion, a `RunServer` helper. What is per-provider is a `runPlanServer`
function of a few dozen lines in `hack/main.go.tmpl`, the template upjet's
pipeline renders into every binary's `main`: build the scheme from the
provider's generated APIs, initialize the embedded Terraform provider(s),
construct the executor and server from the resource configuration map, and
serve. A provider adopts by bumping its upjet dependency, extending that one
template, and running `make generate`, the monolith binary and every family
(service-scoped) binary pick the subcommand up from the same regeneration.

One detail matters for family providers. The provider configuration returns
the full resource set regardless of which scoped binary is running, so each
family binary filters the resource configuration map to its own service
before constructing the server, so the runtime only accepts resources shipped
by that package. Client-side routing is derived independently from the package
CRDs.

Transport security is deliberately out of scope for v1alpha1: the intended
deployment is a client-managed container listening on localhost, the same
trust model as `crossplane render`'s function runtimes. Anyone deploying a
plan server as a shared service needs to put TLS and authorization in front
of it; `RunServer` can grow TLS options when that use case is real.

### What Clients Do With It

The flow for the Crossplane CLI (detailed in the companion crossplane/cli
one-pager):

1. Determine the provider images: the project's dependencies for `project
   simulate`, the providers installed on the target cluster for `resource
   simulate`, or an explicit override, which is how an upgrade is previewed
   with a provider version nothing runs yet.
2. Inspect each provider package's CRDs and build a group/kind-to-provider
   routing table, reusing cached results for image digests already seen.
3. Start plan servers as needed. Discovery does not require them to be running,
   so a client may start them eagerly or on first use.
4. For each resource to preview: fetch its live MR (if any) and the
   ProviderConfig it references from the target cluster, send them with the
   desired resource to the routed server, collect the response.
5. Render a `terraform plan`-style diff and summary.

Everything the protocol needs from a cluster (the live MR with its
`status.atProvider` and external-name annotation) is fetched by the client
with the client's own RBAC. The plan server itself holds no privileges at
all, which keeps the security story short: it cannot leak what it cannot reach,
and it only ever sees state the caller was already entitled to read.

The protocol does not care that the client is a CLI. The same servers and the
same RPCs serve CI checks on pull requests, a fleet-wide provider-upgrade
preview (run the *new* provider version's plan server against every existing
MR's spec and flag anything that is not a no-op), or an in-cluster service
that a UI queries.

### Predictability

A plan is a pure function of its request: two structs in, one diff out, no
I/O. Cost is one schema diff per call, the same computation a reconcile
performs before deciding to update, minus the observe. Memory is the loaded
provider schemas, which the provider binary carries anyway. There is no
workspace, no state file, and no per-request cleanup; a plan server can
compute plans for a whole project's resources in seconds, in parallel, and be
torn down.

## Alternatives Considered

### Discovering Resources From the Running Plan Server

The original design included a `GetInfo` RPC that reported the API groups a
running plan server supports. This keeps discovery simple for the client and
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

I think the gRPC service is the better first primitive. The CRD path requires
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

Have the plan server observe the external resource before diffing, like
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
no plan server. But it cannot be the real answer: it has no schema, so it
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
