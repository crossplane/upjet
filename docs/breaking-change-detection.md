# Breaking Change Detection and Auto-Conversion System

## Table of Contents

- [Overview](#overview)
- [How It Works](#how-it-works)
  - [Build-Time: Schema Analysis](#build-time-schema-analysis)
  - [Runtime: Conversion Registration](#runtime-conversion-registration)
  - [Runtime: Conversion Execution](#runtime-conversion-execution)
- [Quick Start Guide](#quick-start-guide)
- [Supported Conversions](#supported-conversions)
- [Configuration Options](#configuration-options)
- [Controller Version Architecture](#controller-version-architecture)
- [Troubleshooting](#troubleshooting)
- [Limitations and Trade-offs](#limitations-and-trade-offs)
- [Examples](#examples)

---

## Overview

The breaking change detection system automatically generates and registers CRD conversion functions when Terraform provider schemas evolve. It handles three types of breaking changes:

1. **Field additions/deletions** - Fields added or removed between API versions
2. **Type changes** - Fields changing type (e.g., string → number, string → boolean)
3. **Singleton list conversions** - Lists converted to embedded objects (integration with existing system)

**Key Benefits:**
- Eliminates manual conversion function writing
- Prevents data loss during API version upgrades/downgrades
- Automatically detects breaking changes during code generation
- Scales across 100+ resources per provider

**Architecture:**
```
Build Time:  CRDs → schemadiff tool → JSON manifest
Runtime:     JSON → Auto-registration → Conversion functions → Webhooks
```

---

## How It Works

### Build-Time: Schema Analysis

#### 1. CRD Generation
Standard upjet code generation creates Go types and CRDs:
```bash
# In provider's generate/generate.go
//go:generate go run -tags generate sigs.k8s.io/controller-tools/cmd/controller-gen ... output:artifacts:config=../package/crds
```

#### 2. Schema Diff Analysis
After CRDs are generated, the `schemadiff` tool analyzes them:
```bash
# Added to generate/generate.go
//go:generate go run github.com/crossplane/upjet/v2/cmd/schemadiff -i ../package/crds -o ../config/crd-schema-changes.json
```

**What schemadiff does:**
- Reads all CRD YAML files in `package/crds/`
- Compares each API version pair (e.g., v1beta1 vs v1beta2)
- Detects three change types:
  - `field_added`: Field exists in new version but not old
  - `field_deleted`: Field exists in old version but not new
  - `type_changed`: Field type changed (string/number/boolean)
- Outputs JSON mapping `{group}/{kind}` to list of changes

**Example Output** (`config/crd-schema-changes.json`):
```json
{
  "ec2.aws.upbound.io/VPC": {
    "versions": {
      "v1beta2": {
        "oldVersion": "v1beta1",
        "newVersion": "v1beta2",
        "changes": [
          {
            "path": "spec.forProvider.maxSize",
            "changeType": "type_changed",
            "oldValue": "string",
            "newValue": "number"
          },
          {
            "path": "spec.forProvider.newField",
            "changeType": "field_added"
          }
        ]
      }
    }
  }
}
```

#### 3. JSON Embedding
The JSON file is embedded into the provider binary:
```go
// In config/provider.go
//go:embed crd-schema-changes.json
var crdSchemaChanges []byte
```

### Runtime: Conversion Registration

During provider startup (`GetProvider()`), the system processes the embedded JSON:

#### Phase 1: Exclude Type Changes from Identity Conversion
```go
if err := ujconfig.ExcludeTypeChangesFromIdentity(pc, crdSchemaChanges); err != nil {
    return nil, err
}
```

**What it does:**
- Scans JSON for all `type_changed` entries
- Adds affected paths to `r.AutoConversionRegistrationOptions.IdentityConversionExcludePaths`
- **Why**: Identity conversion copies fields as-is. Type-changed fields would fail (can't copy number to string slot)

**Example:**
```go
// If JSON contains: spec.forProvider.maxSize changed from string to number
// Result:
r.AutoConversionRegistrationOptions.IdentityConversionExcludePaths = []string{
    "maxSize",  // Trimmed prefix, just the field name
}
```

#### Phase 2: Provider-Specific Transformations
```go
bumpVersionsWithEmbeddedLists(pc)  // Provider-specific logic
```

**Critical**: This step must:
1. Set `r.ControllerReconcileVersion = r.Version` (controller version = API version)
2. Configure singleton list conversions
3. Use the exclusion paths populated in Phase 1

#### Phase 3: Register Automatic Conversions
```go
if !generationProvider {  // Skip during code generation
    if err := ujconfig.RegisterAutoConversions(pc, crdSchemaChanges); err != nil {
        return nil, err
    }
}
```

**What it does:**
- Iterates through all resources in the provider
- For each resource with changes in JSON:
  - **Field additions/deletions**: Registers `NewlyIntroducedFieldConversion` (ToAnnotation / FromAnnotation modes)
  - **Type changes**: Registers `FieldTypeConversion` with appropriate mode:
    - `string → number`: Consults Terraform schema to determine `StringToInt` or `StringToFloat`
    - `number → string`: Defaults to `IntToString` (limitation: float precision loss)
    - `string ↔ boolean`: Registers `StringToBool` / `BoolToString`
  - Registers bidirectional converters (both old→new and new→old)

### Runtime: Conversion Execution

#### CRD Conversion Webhook Flow

When a user applies a resource (e.g., v1beta2), Kubernetes may need to convert it to another version:

```
User applies Application v1beta2
         ↓
Webhook called to convert v1beta2 → v1beta1 (if requested)
         ↓
Registered conversions execute in order:
  1. PrioritizedManagedConversion (identity conversion)
     - Copies all fields except those in IdentityConversionExcludePaths
  2. PavedConversions (chained, without unpaving between):
     a. Singleton list conversions
     b. Type conversions (string↔number, string↔bool)
     c. Field preservation (store/restore via annotations)
  3. ManagedConversions (if any custom ones)
```

**Conversion Types:**

**1. Identity Conversion (Always First)**
- Copies identical fields from source to target
- Skips paths in `IdentityConversionExcludePaths` (type-changed fields)
- Handles the bulk of field copying

**2. Type Conversions**
- Converts individual fields with type changes
- Example: `spec.forProvider.maxSize` string "100" → number 100
- Uses Go's `strconv` package (ParseInt, ParseFloat, ParseBool, Format*)

**3. Field Preservation (Annotations)**
- For fields that exist in one version but not the other
- **old→new (field added in new)**: Restores value from annotation
- **new→old (field doesn't exist in old)**: Stores value in annotation

**Annotation Format:**
```yaml
metadata:
  annotations:
    internal.upjet.crossplane.io/field-conversions: |
      {
        "spec.forProvider.newField": "value",
        "spec.initProvider.anotherField": ["list", "values"],
        "status.atProvider.statusField": 42
      }
```

---

## Quick Start Guide

### Step 1: Update Controller Versions (Recommended)

Ensure controllers reconcile on the latest API version to simplify architecture:

```go
// In provider-specific version bumping function
func bumpVersionsWithEmbeddedLists(pc *ujconfig.Provider) {
    for name, r := range pc.Resources {
        if needsVersionBump(name) {
            r.Version = "v1beta2"

            // IMPORTANT: Set controller version to match API version
            r.ControllerReconcileVersion = r.Version  // ← This eliminates version mismatch

            // Configure conversions with exclusion paths
            excludePaths := append(
                r.CRDListConversionPaths(),
                r.AutoConversionRegistrationOptions.IdentityConversionExcludePaths...,
            )
            r.Conversions = []conversion.Conversion{
                conversion.NewIdentityConversionExpandPaths(
                    conversion.AllVersions,
                    conversion.AllVersions,
                    conversion.DefaultPathPrefixes(),
                    excludePaths...,
                ),
                conversion.NewSingletonListConversion(...),
            }
        }
        // Terraform conversions for all resources
        r.TerraformConversions = []ujconfig.TerraformConversion{
            ujconfig.NewTFSingletonConversion(),
        }
    }
}
```

### Step 2: Add Schema Diff to Code Generation

In `generate/generate.go`, add after CRD generation:

```go
//go:generate go run -tags generate sigs.k8s.io/controller-tools/cmd/controller-gen object:headerFile=../hack/boilerplate.go.txt paths=../apis/... crd:allowDangerousTypes=true,crdVersions=v1 output:artifacts:config=../package/crds

// Generate CRD schema diff JSON file
//go:generate go run github.com/crossplane/upjet/v2/cmd/schemadiff -i ../package/crds -o ../config/crd-schema-changes.json
```

### Step 3: Embed JSON in Provider

In `config/provider.go`, add:

```go
package config

import _ "embed"

//go:embed crd-schema-changes.json
var crdSchemaChanges []byte
```

### Step 4: Integrate in GetProvider()

```go
func GetProvider(ctx context.Context, generationProvider bool) (*ujconfig.Provider, error) {
    // ... existing provider setup (resources, references, etc.)

    pc.ConfigureResources()

    // Phase 1: Exclude type-changed fields from identity conversion
    // MUST run before version bumping
    if err := ujconfig.ExcludeTypeChangesFromIdentity(pc, crdSchemaChanges); err != nil {
        return nil, errors.Wrap(err, "cannot exclude type changes from identity")
    }

    // Phase 2: Provider-specific version bumping
    // Uses exclusion paths populated in Phase 1
    bumpVersionsWithEmbeddedLists(pc)

    // Phase 3: Register automatic conversions
    // MUST run after version bumping, skip during code generation
    if !generationProvider {
        if err := ujconfig.RegisterAutoConversions(pc, crdSchemaChanges); err != nil {
            return nil, errors.Wrap(err, "cannot register auto conversions")
        }
    }

    return pc, nil
}
```

### Step 5: Generate and Test

```bash
# Run code generation
make generate

# Verify JSON was generated
cat config/crd-schema-changes.json

# Test provider startup
go run cmd/provider/main.go
```

---

## Supported Conversions

### 1. Field Additions

**Scenario**: Field `spec.forProvider.newField` exists in v1beta2 but not in v1beta1.

**Automatic Behavior:**
```
v1beta2 → v1beta1:
  - Field value stored in annotation
  - Field removed from v1beta1 representation

v1beta1 → v1beta2:
  - Field value restored from annotation (if present)
  - Otherwise field is nil/empty
```

**Example:**
```yaml
# User creates Application v1beta2
apiVersion: applications.azuread.upbound.io/v1beta2
kind: Application
spec:
  forProvider:
    displayName: "my-app"
    newField: "new-value"  # Added in v1beta2

# Stored internally as v1beta1:
apiVersion: applications.azuread.upbound.io/v1beta1
kind: Application
metadata:
  annotations:
    internal.upjet.crossplane.io/field-conversions: |
      {"spec.forProvider.newField": "new-value"}
spec:
  forProvider:
    displayName: "my-app"
    # newField not in v1beta1 schema
```

### 2. Field Deletions

**Scenario**: Field `spec.forProvider.oldField` exists in v1beta1 but not in v1beta2.

**Automatic Behavior**: Same as field additions (symmetric).

### 3. Type Changes: String ↔ Number

**Scenario**: Field `spec.forProvider.maxSize` changes from string to number.

#### String → Number (v1beta1 → v1beta2)

**Automatic Behavior:**
- System consults Terraform schema to determine int64 vs float64
- Registers `StringToInt` or `StringToFloat` converter
- Parses string using `strconv.ParseInt` or `strconv.ParseFloat`

**Example:**
```yaml
# v1beta1
spec:
  forProvider:
    maxSize: "100"  # string

# Converts to v1beta2
spec:
  forProvider:
    maxSize: 100    # number (int64)
```

#### Number → String (v1beta2 → v1beta1)

**Automatic Behavior:**
- Defaults to `IntToString` converter
- **Limitation**: If field is actually float64, precision loss occurs

**Example (safe - integer):**
```yaml
# v1beta2
spec:
  forProvider:
    maxSize: 100    # int64

# Converts to v1beta1
spec:
  forProvider:
    maxSize: "100"  # string
```

**Example (unsafe - float):**
```yaml
# v1beta2
spec:
  forProvider:
    cpuUtilization: 3.14159  # float64

# Converts to v1beta1 (WRONG - precision lost)
spec:
  forProvider:
    cpuUtilization: "3"      # string (truncated!)
```

**Solution for floats**: See [Manual Configuration](#manual-configuration-for-float-fields).

### 4. Type Changes: String ↔ Boolean

**Scenario**: Field `spec.forProvider.enabled` changes from string to boolean.

**Automatic Behavior:**
- Registers `StringToBool` / `BoolToString` converters
- String → Bool: Parses "true"/"false" using `strconv.ParseBool`
- Bool → String: Formats to "true"/"false"

**Example:**
```yaml
# v1beta1
spec:
  forProvider:
    enabled: "true"   # string

# Converts to v1beta2
spec:
  forProvider:
    enabled: true     # boolean
```

---

## Configuration Options

### Resource-Level Options

```go
type Resource struct {
    // ... other fields

    // Options for automatic conversion registration
    AutoConversionRegistrationOptions AutoConversionRegistrationOptions

    // Status field paths for version mismatch handling (DEPRECATED)
    TfStatusConversionPaths []string
}

type AutoConversionRegistrationOptions struct {
    // Disable automatic conversion registration entirely for this resource
    SkipAutoRegistration bool

    // Exclude specific field paths from auto-registration
    // Use for custom conversions or float fields
    AutoRegisterExcludePaths []string

    // Internal: populated automatically by ExcludeTypeChangesFromIdentity
    // Contains Terraform schema paths of type-changed fields
    IdentityConversionExcludePaths []string
}
```

### Manual Configuration for Float Fields

When a `number→string` conversion involves a float field, exclude it and register manually:

```go
// In resource-specific config (e.g., config/ec2/config.go)
func Configure(p *config.Provider) {
    p.AddResourceConfigurator("aws_instance", func(r *config.Resource) {
        // Exclude float field from auto-registration
        r.AutoConversionRegistrationOptions.AutoRegisterExcludePaths = []string{
            "spec.forProvider.cpuUtilization",
        }

        // Manually register float conversions
        r.Conversions = append(r.Conversions,
            conversion.NewFieldTypeConversion(
                "v1beta2", "v1beta1",
                "spec.forProvider.cpuUtilization",
                conversion.FloatToString,
            ),
            conversion.NewFieldTypeConversion(
                "v1beta1", "v1beta2",
                "spec.forProvider.cpuUtilization",
                conversion.StringToFloat,
            ),
        )
    })
}
```

### Skipping Auto-Registration

For resources with complex custom conversions:

```go
func Configure(p *config.Provider) {
    p.AddResourceConfigurator("aws_complex_resource", func(r *config.Resource) {
        // Disable auto-registration entirely
        r.AutoConversionRegistrationOptions.SkipAutoRegistration = true

        // Write fully custom conversions
        r.Conversions = []conversion.Conversion{
            conversion.NewCustomConverter("v1beta1", "v1beta2", func(src, dst resource.Managed) error {
                // Custom conversion logic
                return nil
            }),
        }
    })
}
```

---

## Controller Version Architecture

### Current Architecture (Simplified)

**Since this PR**, providers configure controllers to reconcile on the latest API version:

```
Latest CRD API Version: v1beta2
Controller Version:  v1beta2 (reconciles on this)
Result: No version mismatch, no runtime annotation handling needed
```

**Configuration:**
```go
r.Version = "v1beta2"                      // Latest API version
r.ControllerReconcileVersion = r.Version   // ← Controller uses latest version
```

### Legacy Architecture (Deprecated)

**Previously**, some providers had controllers reconciling on older versions:

```
Latest CRD API Version: v1beta2
Controller Version:  v1beta1 (reconciles on this - MISMATCH)
Result: Needed runtime annotation handling
```

This created complexity:
- Fields in v1beta2 but not v1beta1 had to be restored via annotations during Terraform operations
- Required manual configuration of `TfStatusConversionPaths`
- Runtime overhead merging annotation values

### Deprecation Timeline

| Item | Status | Timeline |
|------|--------|----------|
| `ControllerReconcileVersion` field | Deprecated | Will be removed in next major upjet release |
| `TfStatusConversionPaths` field | Deprecated | Will be removed with ControllerReconcileVersion |
| Runtime annotation handling functions | Deprecated | Will be removed with ControllerReconcileVersion |
| CRD conversion webhook logic | **Permanent** | Core feature, not deprecated |

---

## Limitations and Trade-offs

### 1. Number→String Defaults to Integer

**Limitation**: Cannot automatically detect if old number field was int or float.

**Impact**: Float values lose precision if not manually configured.

**Workaround**: Use `AutoRegisterExcludePaths` and register `FloatToString` converter.

**Why this trade-off**: Majority of number fields are integers. Defaulting to int is pragmatic.

### 2. Function Call Ordering Not Enforced

**Limitation**: Three functions must be called in specific order:
```go
ExcludeTypeChangesFromIdentity()  // First
bumpVersionsWithEmbeddedLists()   // Second
RegisterAutoConversions()          // Third
```

**Impact**: Wrong order causes runtime conversion failures.

**Mitigation**:
- Document order clearly (this guide)
- Integration tests verify correct ordering
- Future enhancement: Single `FinalizeWithBreakingChanges()` function

### 3. Status Field Conversions Require Manual Config (Deprecated)

**Limitation**: `TfStatusConversionPaths` must be manually configured for status fields when controller version ≠ API version.

**Current Status**: Feature is deprecated and inactive when following recommended pattern (controller version = API version).

**Impact**: Only affects legacy configurations with version mismatches.

**Mitigation**: Update controllers to latest API version (Step 1 of Quick Start).

### 4. Annotation Size Limits

**Limitation**: Kubernetes annotations limited to 256KB total per resource.

**Unlikely Scenario**: Would require many large fields to be added/removed simultaneously.

**Impact**: If exceeded, resource UPDATE operations fail.

**Mitigation**: System doesn't enforce limit currently (future enhancement).

---

## Examples

### Example 1: Simple Field Addition

**Scenario**: Add `description` field to Application resource in v1beta2.

**CRD Diff:**
```json
{
  "applications.azuread.upbound.io/Application": {
    "versions": {
      "v1beta2": {
        "oldVersion": "v1beta1",
        "newVersion": "v1beta2",
        "changes": [
          {
            "path": "spec.forProvider.description",
            "changeType": "field_added"
          }
        ]
      }
    }
  }
}
```

**Automatic Registration:**
```go
// Registered automatically by RegisterAutoConversions():
r.Conversions = append(r.Conversions,
    // v1beta1 → v1beta2: Restore description from annotation
    conversion.NewNewlyIntroducedFieldConversion(
        "v1beta1", "v1beta2",
        "spec.forProvider.description",
        conversion.FromAnnotation,
    ),
    // v1beta2 → v1beta1: Store description in annotation
    conversion.NewNewlyIntroducedFieldConversion(
        "v1beta2", "v1beta1",
        "spec.forProvider.description",
        conversion.ToAnnotation,
    ),
)
```

**Result**: Users can create Application v1beta2 with `description`, and it's preserved even when viewed/stored as v1beta1.

### Example 2: Type Change (String → Number)

**Scenario**: Change `maxRetries` from string to number in v1beta2.

**CRD Diff:**
```json
{
  "path": "spec.forProvider.maxRetries",
  "changeType": "type_changed",
  "oldValue": "string",
  "newValue": "number"
}
```

**Automatic Registration:**
```go
// System checks Terraform schema, finds TypeInt, registers:
r.Conversions = append(r.Conversions,
    // v1beta1 → v1beta2: "100" → 100
    conversion.NewFieldTypeConversion(
        "v1beta1", "v1beta2",
        "spec.forProvider.maxRetries",
        conversion.StringToInt,
    ),
    // v1beta2 → v1beta1: 100 → "100"
    conversion.NewFieldTypeConversion(
        "v1beta2", "v1beta1",
        "spec.forProvider.maxRetries",
        conversion.IntToString,
    ),
)

// Also excluded from identity conversion:
r.AutoConversionRegistrationOptions.IdentityConversionExcludePaths = []string{
    "maxRetries",
}
```

### Example 3: Multiple Changes with Float Field

**Scenario**: Multiple changes including a float field that needs special handling.

**CRD Diff:**
```json
{
  "changes": [
    {
      "path": "spec.forProvider.newField",
      "changeType": "field_added"
    },
    {
      "path": "spec.forProvider.cpuThreshold",
      "changeType": "type_changed",
      "oldValue": "number",
      "newValue": "string"
    }
  ]
}
```

**Configuration:**
```go
func Configure(p *config.Provider) {
    p.AddResourceConfigurator("aws_autoscaling_policy", func(r *config.Resource) {
        // Exclude float field from auto-registration
        r.AutoConversionRegistrationOptions.AutoRegisterExcludePaths = []string{
            "spec.forProvider.cpuThreshold",
        }

        // Manually register float conversions
        r.Conversions = append(r.Conversions,
            // number (float) → string: 98.5 → "98.5"
            conversion.NewFieldTypeConversion(
                "v1beta1", "v1beta2",
                "spec.forProvider.cpuThreshold",
                conversion.FloatToString,
            ),
            // string → number (float): "98.5" → 98.5
            conversion.NewFieldTypeConversion(
                "v1beta2", "v1beta1",
                "spec.forProvider.cpuThreshold",
                conversion.StringToFloat,
            ),
        )
    })
}
```

**Result**:
- `newField` auto-registered (field addition)
- `cpuThreshold` manually registered (float conversion)
- No precision loss for `cpuThreshold`
