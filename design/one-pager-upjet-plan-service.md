# A Plan Service for Upjet Providers

* Owner: Christopher Haar (@haarchri)
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

A companion one-pager in crossplane/cli proposes the client side: a `crossplane
project simulate` command that resolves a project's provider dependencies, runs
each provider image as a local plan server, and drives this protocol. This
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

A `PlanService` with two RPCs:

```protobuf
service PlanService {
  // Plan computes a diff between the desired resource and the live resource.
  rpc Plan(PlanRequest) returns (PlanResponse);

  // GetInfo returns metadata about this plan server, including which API
  // groups it supports, so clients can route resources to the right server.
  rpc GetInfo(GetInfoRequest) returns (GetInfoResponse);
}

message PlanRequest {
  // The desired managed resource, as full JSON (apiVersion, kind, metadata,
  // spec).
  google.protobuf.Struct desired_resource = 1;

  // The live resource, with status.atProvider populated. Empty for a
  // resource that does not exist yet.
  google.protobuf.Struct live_resource = 2;
}

message PlanResponse {
  string action = 1;                  // no-op | create | update | replace | delete
  repeated FieldChange changes = 2;
  bool requires_replace = 3;
  repeated string replace_fields = 4; // CRD paths, e.g. spec.forProvider.region
  repeated Diagnostic diagnostics = 5;
  string error = 6;
  google.protobuf.Timestamp computed_at = 7;
}

message FieldChange {
  string field = 1;          // CRD path, e.g. spec.forProvider.deletionWindowInDays
  string current_value = 2;  // Empty on create or when unknown.
  string desired_value = 3;  // Empty when known only after apply.
  bool requires_replace = 4;
  bool sensitive = 5;        // Values are redacted when true.
}
```

The request carries everything the server needs. Desired state is the MR the
user wants to apply. Prior state is reconstructed from the live resource's
`status.atProvider`, the provider's own record of the external resource from
its last observation. Nothing else is consulted: no cluster reads, no cloud
reads. That one decision buys most of the properties I care about:

* **Credential-free.** The server needs no cloud credentials, no
  `ProviderConfig`, no kubeconfig. Anyone who can pull the provider image can
  compute a plan.
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

`GetInfo` exists so clients route by fact instead of by convention. A client
with a fleet of plan servers asks each which API groups it serves
(`s3.aws.upbound.io`, `kms.aws.upbound.io`, ...) and routes each resource to
the right one. The obvious alternative. inspecting each provider package's
CRDs to derive its groups and it assumes every
client has package-inspection machinery and a package to inspect. Asking the running
server is one RPC, doubles as the readiness check after startup, and
`GetInfoResponse` can grow fields later, capability flags
without breaking existing clients.

### The Engine

`pkg/plan` provides the implementation, built from pieces upjet already has:

* A **`Server`** implements the gRPC service. It is constructed from the
  provider's scheme, its root API group, and its generated resource
  configuration map, the same `map[string]*config.Resource` the provider's
  controllers are built from. It resolves an incoming resource's GVK to its
  Terraform resource type (handling both cluster-scoped and namespaced v2 API
  groups), looks up the resource's configuration, and hands off to the
  executor. Plan failures are reported in `PlanResponse.error` rather than as
  gRPC errors, so one bad resource in a batch does not look like a broken
  server.
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
    `terraform plan -refresh=false` to a plan file, and reads it back with
    `terraform show -json`. The JSON plan's `resource_changes` carry the
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
  sensitive values, and marks computed values as known-after-apply. It also
  filters changes the user did not author: fields injected by the provider's
  configuration injector and Crossplane's own system tags (`crossplane-kind`,
  `crossplane-name`, `crossplane-providerconfig`). Without that filter every
  plan for a fresh resource reports noise the user cannot act on.

The executor needs a `terraform.Setup` to get provider metadata (and, for
Framework resources, a configured provider). Because the server never reaches
the cloud, this setup is minimal: the provider's singleton metadata plus
whatever configuration the schema requires (a region, say, taken from the
resource itself). It deliberately does not run the provider's normal
credential resolution.

Sensitive *inputs* deserve a note. A desired spec can reference sensitive
parameters stored in Secrets. The executor treats Secret resolution as
optional: with no Kubernetes client, the normal case for a local plan server,
referenced sensitive parameters are simply absent from the diff inputs, and
the corresponding changes surface as sensitive/unknown rather than failing
the plan.

### Serving It

Providers gain an `internal plan-server` subcommand, hidden, the way
`crossplane internal render` is, because the stable interface is the
protocol, not the command line:

```console
$ docker run <provider-image> internal plan-server --port 50051
```

The subcommand constructs the `Server` from the provider's existing wiring,
scheme, resource configuration map, provider metadata and serves plaintext
gRPC on the given port until signalled. It starts in single-digit seconds
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
before constructing the server. That way a binary's `GetInfo` advertises
exactly the groups it can plan, which is what makes client-side routing
across a family of plan servers work.

Transport security is deliberately out of scope for v1alpha1: the intended
deployment is a client-managed container listening on localhost, the same
trust model as `crossplane render`'s function runtimes. Anyone deploying a
plan server as a shared service needs to put TLS and authorization in front
of it; `RunServer` can grow TLS options when that use case is real.

### What Clients Do With It

The flow for the Crossplane CLI (detailed in the companion crossplane/cli
one-pager):

1. Resolve the project's provider dependencies to images.
2. `docker run` each image's `plan-server`, wait for `GetInfo`, and build a
   routing table from supported API groups.
3. For each resource to preview: fetch its live MR (if any) from the target
   cluster, send desired + live to the routed server, collect the response.
4. Render a `terraform plan`-style diff and summary.

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
