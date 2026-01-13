<!--
SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->

# CLAUDE.md

This file provides guidance to AI Agents when working with code in this repository.

## Overview

Upjet is a code generation framework that transforms Terraform providers into Crossplane providers.  
It generates Kubernetes CRDs, reconciliation controllers, example manifests, and provider configuration from Terraform schemas.

The framework supports three Terraform execution modes:
- **Terraform CLI** (fork-based): Spawns terraform processes
- **Terraform Plugin SDK v2** (no-fork): Direct Go library calls
- **Terraform Plugin Framework** (no-fork): Protocol-based communication (protov6)

## Development Commands

### Building and Testing
```bash
# First-time setup: Initialize build submodule
make submodules

# Run linting and tests (run before opening PR)
make reviewable

# Build the project
make build

# Run unit tests
make test
# Or with specific flags:
go test -v ./pkg/...

# Run specific test
go test -v ./pkg/config -run TestExternalName

# Run linting
make lint

# Check Go modules are tidy
make modules.check

# Generate code
make generate
```

### Common Development Workflow
```bash
# 1. Make changes to Upjet code
# 2. Test in a provider using replace directive in provider's go.mod:
#    replace github.com/crossplane/upjet/v2 => ../upjet
# 3. Run make reviewable before committing
# 4. Open PR with example provider changes
```

## High-Level Architecture

The framework follows a layered pipeline:

```
Configuration Layer (pkg/config)
    ↓
Code Generation Pipeline (pkg/pipeline)
    ↓
Type Generation (pkg/types, pkg/schema)
    ↓
Runtime System (pkg/controller, pkg/terraform, pkg/resource)
```

### Key Architectural Layers

**1. Configuration Layer (pkg/config)**
- `Provider`: Root configuration mapping Terraform schemas to CRDs
- `Resource`: Per-resource configuration (external names, references, sensitivity)
- `ExternalName`: Maps Terraform IDs ↔ Crossplane external names (required for all resources)

**2. Code Generation Pipeline (pkg/pipeline)**
- `Run()`: Main orchestrator that generates all code
- `CRDGenerator`: Creates `*_types.go` files with Kubernetes CRD structs
- `ControllerGenerator`: Creates `*_controller.go` files with reconciliation logic
- `TerraformedGenerator`: Creates `*_terraformed.go` files implementing resource.Terraformed interface
- `ExampleGenerator`: Creates YAML example manifests

**3. Schema/Type Transformation (pkg/schema, pkg/types)**
- `TypeBuilder`: Converts Terraform schemas recursively to Go types
- Field classification:
  - **ForProvider**: User-configurable parameters
  - **InitProvider**: Late-initialized fields
  - **AtProvider**: Read-only observations

**4. Runtime Layer (pkg/controller, pkg/terraform)**
- `Connector`: Creates workspace and ExternalClient per resource
- `ExternalClient`: Implements Observe/Create/Update/Delete operations
- `Workspace`: Per-resource Terraform working directory managing state files
- Three execution modes switch on resource configuration:
  - CLI: Spawns terraform binary (pkg/controller/external.go)
  - SDK v2: Direct library calls (pkg/controller/external_tfpluginsdk.go)
  - Framework: Protocol calls (pkg/controller/external_tfpluginfw.go)

## Critical Patterns

### External Names (Required)
Every resource MUST have an external name configuration. This is how Crossplane identifies resources in the remote system:

```go
// Common patterns:
config.NameAsIdentifier           // Uses "name" field as identifier
config.IDAsExternalName           // Uses Terraform "id" as identifier
config.TemplatedStringAsIdentifier("field", "{{ .parameters.x }}")
```

The external name is:
- Removed from spec (not user-provided)
- Extracted from Terraform state after creation
- Used for terraform import operations

### Cross-Resource References
Enable Kubernetes-style references between resources:

```go
r.References["subnet_ids"] = config.Reference{
    TerraformName: "aws_subnet",
    Extractor: "status.atProvider.id",  // Optional, defaults to external name
}
// Generates: SubnetIDRefs and SubnetIDSelector fields in CRD
```

### Late Initialization
Server-generated fields are automatically populated into spec after creation. Configure ignored fields:

```go
r.LateInitializer.IgnoredFields = []string{"tags", "metadata"}
```

### Async Operations
For resources with long create/delete times:

```go
r.UseAsync = true  // Enables concurrent async operations
```

### Management Policies
Control which operations are allowed (Crossplane v1.11+):
- Observe: Read-only
- Create/Update/Delete: Allow modifications
- LateInitialize: Auto-fill from server
- * (All): Default

## Code Generation Flow

```go
// 1. Extract Terraform schema
// terraform providers schema --json > schema.json

// 2. Create provider configuration
provider := config.NewProvider(
    schemaJSON,
    "aws",                              // Terraform resource prefix
    "github.com/upbound/provider-aws",
    metadataYAML,
    config.WithIncludeList([]string{"aws_vpc", "aws_subnet"}),
)

// 3. Configure resources
provider.AddResourceConfigurator("aws_vpc", func(r *config.Resource) {
    r.ExternalName = config.NameAsIdentifier
    r.References["default_security_group_id"] = config.Reference{
        TerraformName: "aws_security_group",
    }
})

// 4. Run pipeline
provider.ConfigureResources()
pipeline.Run(provider, nil, "./")
```

This generates for each resource:
```
apis/<group>/<version>/
├── zz_<resource>_types.go          # CRD types
├── zz_<resource>_terraformed.go    # Spec ↔ Terraform mapping
└── zz_register.go                   # Type registration

internal/controller/<group>/<resource>/
└── zz_<resource>_controller.go     # Reconciliation logic

examples-generated/<group>/
└── <resource>.yaml                  # Example manifest
```

## Key Packages

| Package | Responsibility |
|---------|-----------------|
| pkg/config | Provider and resource configuration (what to generate) |
| pkg/pipeline | Code generation orchestration and generators |
| pkg/controller | Runtime reconciliation logic and external client |
| pkg/terraform | Workspace management and Terraform execution |
| pkg/resource | Terraformed interface and observation extraction |
| pkg/types | Go type generation from Terraform schemas |
| pkg/schema | Schema traversal and transformation |
| pkg/registry | Provider metadata and documentation scraping |
| cmd/scraper | Extract Terraform provider docs metadata |
| cmd/resolver | Post-process generated resolver files |

## Generated Type Structure

```go
// Top-level CRD
type VPC struct {
    metav1.TypeMeta
    metav1.ObjectMeta
    Spec   VPCSpec
    Status VPCStatus
}

type VPCSpec struct {
    ForProvider      VPCParameters      // User inputs
    InitProvider     VPCInitParameters  // Late-initialized
    ManagementPolicy xpv1.ManagementPolicy
    ProviderConfigRef xpv1.Reference
}

type VPCStatus struct {
    AtProvider VPCObservation  // Observed outputs (read-only)
    Conditions []xpv1.Condition
}
```

## Extension Points

1. **Schema Traversers**: Modify schemas before generation
2. **Resource Configurators**: Customize individual resources via AddResourceConfigurator
3. **Reference Injectors**: Add cross-resource references
4. **Configuration Injectors**: Inject values into Terraform config at runtime
5. **Custom Diffs**: Customize Terraform diff computation

## Runtime Operation Flow

**Observe** → Check resource exists → Extract state → Late-initialize → Extract connection details
**Create** → Convert spec to Terraform JSON → terraform apply → Extract state → Set external name
**Update** → Regenerate config → terraform plan → terraform apply if needed
**Delete** → terraform destroy

## Go-Specific Conventions

**Type System:**
- No `any` type used throughout codebase - use concrete types or type parameters
- Pointer types used for optional fields in generated structs
- Type aliases avoided in favor of explicit types

**Code Generation:**
- All generated files prefixed with `zz_` to distinguish from manually written code
- Generated code uses `typewriter` package for type-safe code generation
- Interface definitions in `pkg/resource/interfaces.go` and `pkg/controller/interfaces.go`

**Testing:**
- Standard Go testing only - no Ginkgo, Testify, or third-party test frameworks
- Table-driven tests strongly preferred (see `pkg/config/externalname_test.go` for examples)
- Test files: `*_test.go` in same package as code under test
- Mock generation: Uses `golang/mock` (see `pkg/resource/fake/mocks/`)
- Integration tests: Use replace directive in provider go.mod to test Upjet changes

**Go Module Management:**
- Module path: `github.com/crossplane/upjet/v2`
- When testing in providers: Add `replace github.com/crossplane/upjet/v2 => ../upjet` to provider's go.mod
- Run `make modules.check` to verify go.mod/go.sum are tidy before committing

**Package Organization:**
- Internal packages for generated controller code (`internal/controller/`)
- Public API packages under `pkg/`
- Avoid circular dependencies between packages
- Use dependency injection for testing (see controller setup)

**Error Handling:**
- Use `github.com/pkg/errors` for error wrapping with context
- Return errors from functions, don't panic (except for impossible states)
- Wrap errors with context: `errors.Wrap(err, "cannot configure resource")`

**Architecture Patterns:**
- External names required for all resources (Go type: `config.ExternalName`)
- Schema-driven generation (no manual type definitions)
- Interface-based design for extensibility (`resource.Terraformed`, `controller.ExternalClient`)
- Kubernetes-native patterns via crossplane-runtime
