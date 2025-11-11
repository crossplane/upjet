<!--
SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->
# Managing CRD Versions

This guide explains how to manage Custom Resource Definition (CRD) versions
when underlying Terraform provider schemas change, including when to bump
versions and how to implement version management using Upjet's configuration API.

## Table of Contents

- [When to Bump CRD Versions](#when-to-bump-crd-versions)
- [Breaking vs. Non-Breaking Changes](#breaking-vs-non-breaking-changes)
- [Implementation with Upjet](#implementation-with-upjet)
- [Conversion Strategies](#conversion-strategies)
- [Best Practices](#best-practices)
- [Example Reference](#example-reference)

## When to Bump CRD Versions

CRD version management becomes necessary when the underlying Terraform provider
schema changes in ways that affect the generated Custom Resource Definition. The
decision to bump versions depends on the nature of the change.

### Changes That Require Version Bumps

Version bumps are needed for **breaking changes** that would prevent existing
manifests from working:

1. **Field Renames**: When a Terraform resource field is renamed
   - Example: `cluster_name` → `name`
   - Impact: Existing manifests using the old field name would fail

2. **Field Removals**: When a field is removed from the Terraform schema
   - Impact: Manifests referencing the removed field would be invalid

3. **Type Changes**: When a field's type changes incompatibly
   - Example: String → Integer, or singleton → list
   - Impact: Existing values may not be compatible with the new type

4. **API Improvements**: Converting singleton lists to embedded objects
   - Example: `field[0].value` → `field.value`
   - Impact: Changes the structure of the API

5. **New Required Fields**: When a previously optional field becomes required
   - Impact: Existing manifests without this field would become invalid
   - Note: This is unavoidable even with versioning (see below)

### Changes That Do NOT Require Version Bumps

The following changes maintain backward compatibility and do not require a new
version:

1. **Optional New Fields**: Adding new optional fields to the schema
   - The existing version stays backward compatible
   - Old manifests continue to work without modification

2. **Optional Fields with Defaults**: New fields that have default values in the
   Terraform provider schema
   - Note: Upjet does not generate default values in the CRD schema itself, even
     when defaults exist in the Terraform schema
   - Defaults are applied at runtime by the Terraform provider or cloud provider
   - Resources will function with or without the new field specified in the manifest

3. **Additional Enum Values**: Adding new allowed values to existing fields
   - Existing values remain valid

## Breaking vs. Non-Breaking Changes

### Non-Breaking Changes (Backward Compatible)

When new fields are **optional** or have **default values in the Terraform
provider schema**, no version bump is necessary. The existing API version
remains backward compatible because:

- Old manifests that don't specify the new fields continue to work
- The Terraform provider handles the absence of these fields gracefully at
  runtime (applying defaults when needed)
- Late initialization can populate sensible defaults from the provider state
- Note: While defaults exist in the Terraform schema, they are not reflected in
  the generated CRD schema itself

**Example**: Adding an optional `encryption_enabled` field to a database
resource doesn't break existing manifests that don't specify this field.

### Breaking Changes (Require Versioning)

Breaking changes require careful management through versioning:

#### Hiding Breaking API Changes

When fields are renamed or removed, generating a new API version allows old
manifests to continue working while new manifests can use the updated schema.

**Example**: If a Terraform resource renames `instance_name` to `name`:
- `v1alpha1`: Keep `instance_name` field (for existing manifests)
- `v1alpha2`: Use new `name` field (for new manifests)
- Conversion webhook: Translate between the two versions

The new version contains updated field names while the old version preserves
original field names, "shielding clients of the old API version from the
breaking change."

#### Unavoidable Breaking Changes

Some breaking changes cannot be fully mitigated by versioning:

**Required Fields**: When a new field becomes required (not optional):
- Both old and new API versions will fail to create resources without this field
- This represents an unavoidable breaking change at the provider level
- Users must update their manifests regardless of the API version they use

**Recommendation**: Communicate these changes clearly in release notes and
migration guides.

## Implementation with Upjet

Upjet provides a resource configuration API to manage multiple CRD versions:

### Configuration Fields

```go
// Resource contains configuration for a given resource.
type Resource struct {
    // Version specifies the version of the resource (e.g., "v1beta1")
    Version string

    // PreviousVersions is a list of previous API versions that should still
    // be generated and served by the provider
    PreviousVersions []string

    // ... other fields
}
```

### Example Configuration

Here's how to configure a resource with multiple versions:

```go
func Configure(p *config.Provider) {
    p.AddResourceConfigurator("azurerm_example_resource", func(r *config.Resource) {
        // Current version
        r.Version = "v1beta1"

        // Previous versions to maintain
        r.PreviousVersions = []string{"v1alpha2", "v1alpha1"}

        // Additional configuration for conversions (see below)
    })
}
```

This configuration will:
1. Generate the CRD for the latest version (`v1beta1`)
2. Mark the previous versions (`v1alpha1`, `v1alpha2`) as served API versions
3. Set `v1beta1` as both the storage version and hub version (by default, both
   are set to `r.Version`)
4. Create conversion webhooks to translate between versions

**Note**: The storage version and hub version are different concepts, though by
default both are set to `r.Version`. They can be configured independently using:
- `SetCRDStorageVersion`: Configures which version is used for persistence
- `SetCRDHubVersion`: Configures which version is used as the hub in the
  hub-and-spoke conversion pattern

## Conversion Strategies

When serving multiple CRD versions, you need conversion logic to translate
between versions.

### Hub-and-Spoke Pattern

Kubernetes uses a "hub-and-spoke" conversion model:

```
v1alpha1 ←→ v1beta1 (hub) ←→ v1alpha2
```

- **Hub**: The central version used for all conversions (by default set to
  `r.Version`, usually the latest stable version)
- **Spokes**: Other versions that convert to/from the hub
- **Storage Version**: The version used to persist objects in etcd (by default
  also set to `r.Version`, but can be configured separately)

All conversion happens through the hub version:
- `v1alpha1` → `v1beta1` → `v1alpha2`
- Never directly `v1alpha1` ↔ `v1alpha2`

**Note**: While the hub version and storage version are often the same (both
default to `r.Version`), they serve different purposes and can be configured
independently if needed.

### API-Level Converters

Configure conversion functions for API schema changes:

```go
func Configure(p *config.Provider) {
    p.AddResourceConfigurator("azurerm_example_resource", func(r *config.Resource) {
        r.Version = "v1beta1"
        r.PreviousVersions = []string{"v1alpha1"}

        // Register conversion functions
        // These handle field renames, restructuring, etc.
        r.Conversions = []config.Conversion{
            {
                // Convert from v1alpha1 to v1beta1
                FromVersion: "v1alpha1",
                ToVersion:   "v1beta1",
                ConvertFn:   convertV1Alpha1ToV1Beta1,
            },
            {
                // Convert from v1beta1 to v1alpha1
                FromVersion: "v1beta1",
                ToVersion:   "v1alpha1",
                ConvertFn:   convertV1Beta1ToV1Alpha1,
            },
        }
    })
}

func convertV1Alpha1ToV1Beta1(src, dst interface{}) error {
    // Implement conversion logic
    // Example: rename fields, restructure data
    return nil
}

func convertV1Beta1ToV1Alpha1(src, dst interface{}) error {
    // Implement reverse conversion logic
    return nil
}
```

### Terraform-Level Converters

Some changes happen at the Terraform provider level and need special handling:

```go
func Configure(p *config.Provider) {
    p.AddResourceConfigurator("azurerm_example_resource", func(r *config.Resource) {
        // Configure Terraform state converters for provider-level changes
        r.TerraformConversions = []config.TerraformConversion{
            {
                // Handle changes in Terraform provider behavior
                // between different provider versions
            },
        }
    })
}
```

## Best Practices

### Consider the Maintenance Burden

**Key Insight**: "Change management is hard"

Before implementing version management:

1. **Evaluate the Impact**: How many users will be affected by the breaking
   change?

2. **Consider Accepting Breaking Changes**: Sometimes it's better to accept a
   breaking change and move the operational burden to clients rather than
   maintaining multiple API versions indefinitely.

3. **Communicate Clearly**: Provide clear migration guides and deprecation
   timelines when breaking changes are necessary.

### When to Use Versioning

Use versioning when:
- Large number of existing manifests would break
- Change affects critical production workloads
- Migration would be complex or risky for users
- You can provide a reasonable deprecation timeline

### When to Accept Breaking Changes

Accept breaking changes when:
- The resource is new or has limited adoption
- The change significantly improves the API design
- Maintaining multiple versions would add excessive complexity
- The migration path is straightforward

### Deprecation Timeline

When maintaining multiple versions:

1. **Announce**: Clearly communicate the deprecation in release notes
2. **Document**: Provide migration guides with examples
3. **Warning Period**: Give users adequate time (e.g., 2-3 releases)
4. **Remove**: Only remove after the warning period expires

## Example Reference

For a working example of CRD version management in practice, see:

- [provider-upjet-azuread](https://github.com/crossplane-contrib/provider-upjet-azuread)

This provider demonstrates:
- Configuration of multiple API versions
- Conversion webhook implementation
- Migration guides for users

## Additional Resources

- [Kubernetes API Versioning](https://kubernetes.io/docs/reference/using-api/#api-versioning)
- [Kubernetes API Changes Documentation](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api_changes.md)
- [Crossplane API Conventions](https://github.com/crossplane/crossplane/blob/master/design/one-pager-managed-resource-api-design.md)

## Related Documentation

- [Configuring a Resource](configuring-a-resource.md) - General resource
  configuration options
- [Generating a Provider](generating-a-provider.md) - Initial provider setup
