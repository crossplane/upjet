<!--
SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->

# Provider Main Template Variables

This document describes the template variables consumed by the provider
subpackage main program template. When configured, this template is used to
render the `zz_main.go` entry point for each subpackage family member (one per
API group), allowing the generated provider binary to be partitioned across
API groups while still preserving a monolithic build for backwards
compatibility.

The main template is configured on `config.Provider.MainTemplate` (typically
via `config.WithMainTemplate`). If `MainTemplate` is left empty, no
per-subpackage main programs are generated. Any custom template MUST honor the
contract described below.

> [!WARNING]
> **Overriding the main template is an advanced feature.**
>
> - Use this capability with great care, and only when the default behavior
>   genuinely cannot accommodate your provider's requirements. If at all
>   possible, avoid configuring a custom main template and prefer upstream
>   changes that benefit all providers.
> - The set of template variables and their semantics are **not** covered by
>   the same compatibility guarantees as the rest of the public Go API. Upjet
>   may add, remove, rename, or change the meaning of template variables
>   **without a deprecation cycle**. Such changes will land in a new minor
>   release and be called out in the corresponding release notes.
> - Custom templates that diverge from the default may break on any minor
>   upgrade. Plan for the maintenance cost of keeping a fork of this template
>   in sync.

## Template Variables

The following keys are available inside the template via `{{ .<Name> }}`.

| Variable | Type | Description |
|---|---|---|
| `Group` | `string` | Identifier of the subpackage main program being generated. This is usually a short API group name (e.g. `ec2`, `rds`), but it also takes two special values: `monolith` — the program that wires up **all** of the provider's resources into a single binary — and `config` — the program for the base/`ProviderConfig` resources. The template is executed once per value, so each rendered `zz_main.go` is specialized for a single group. A custom main template MUST handle the `monolith` value (and, when the provider ships base resources, `config`) in addition to the real API groups. Use this to construct package imports, controller setup calls, and any group-scoped identifiers in the generated main program. |

## Generated Output

The template is executed once per `Group` value — each real API short group
plus the special `monolith` and `config` values — and each rendered file is
written to:

```
<provider-cmd-dir>/<Group>/zz_main.go
```

For example, `cmd/provider/ec2/zz_main.go` for the `ec2` API group and
`cmd/provider/monolith/zz_main.go` for the monolithic program. The monolithic
program is generated alongside the per-group programs (via the `monolith`
`Group` value). The main program is generated only once (from the
 cluster-scoped pipeline run).

## Template Engine Notes

The main template is parsed with the standard `text/template` package. Unlike
the controller template, it is not processed by an import-tracking wrapper, so
the template is responsible for emitting:

- The license header, if required.
- The `// Code generated ... DO NOT EDIT.` marker, if required.
- The full `import` block, including any group-specific imports derived from
  `{{ .Group }}`.

## Overriding the Template

To supply a custom main template, set `config.Provider.MainTemplate` via
`config.WithMainTemplate`. The custom template must accept the variables
documented above; otherwise the rendered file will not compile against the
upjet runtime contracts.

### When Template Errors Surface

Errors in a custom main template are reported at two distinct stages:

- **Provider generation time.** Static template errors — such as a template
  string that fails to parse (`text/template.Parse()` failures), malformed
  actions, or references to undefined fields evaluated during execution — are
  raised by the code generation pipeline and cause provider generation to
  fail.
- **Provider build / lint time.** Syntactically valid templates that produce
  invalid Go (for example, an empty template that emits no `package` clause,
  or output that omits imports required by the upjet runtime contracts) parse
  cleanly but fail later when the generated `zz_main.go` files are compiled
  or linted as part of the provider build.
