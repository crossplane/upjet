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
| `Group` | `string` | API group identifier for the subpackage main program being generated (e.g. `ec2`, `rds`). The template is executed once per group, so each rendered `zz_main.go` is specialized for a single group. Use this to construct package imports, controller setup calls, and any group-scoped identifiers in the generated main program. |

## Generated Output

For every group, the rendered file is written to:

```
<provider-cmd-dir>/<Group>/zz_main.go
```

A monolithic main program is also generated alongside the per-group main
programs to preserve backwards compatibility for consumers that build the
provider as a single binary.

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
