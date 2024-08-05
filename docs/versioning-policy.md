# Upjet provider versioning and upgrade policy

This policy recommends how Upjet providers should implement versioning numbers and handle upgrades to new versions.

## Versioning 

Upjet-based providers **MUST** follow the [semantic versioning](https://semver.org/) numbering scheme. 

Specifically, a version contains 3 digits known as `major`, `minor` and `patch` numbers in the format `major.minor.patch` (e.g., `v1.2.3`).

### Versioning specification

1. The `major` version number **MUST** be incremented if the `major` version number of the Terraform provider it is generated from is incremented.
2. The `minor` version number **MUST** be incremented when new functionality is released or when [unavoidable breaking changes](#unavoidable-breaking-changes) are introduced. 
3. The `patch` version number **SHOULD ONLY** be incremented for a release containing **ONLY** bug fixes and no new features.
4. A release that increments the `minor` or `patch` version **ONLY**, **MUST** be backward compatible.
5. Incrementing the `minor` version **MUST** reset the `patch` version number to zero.
6. Incrementing the `major` version **MUST** reset the `minor` and `patch` version number to zero.

### Unavoidable breaking changes

Due to a reliance on the upstream Terraform providers and prior instances where they introduce breaking changes in minor version releases, we cannot guarantee that a minor version bump will never introduce a breaking change. 

All efforts **SHOULD** be made to automate or work around a breaking change to minimize the impact on the end user. 

When it is unavoidable, the details of the breaking change and how to adapt to it **MUST** accompany the new version's release notes.

## Upgrades

### Backward compatibility

Except for [unavoidable breaking changes](#unavoidable-breaking-changes), backward compatibility expects that there **MUST** be no changes required to the Crossplane resource manifests, configurations or infrastructure when upgrading to a new version with a higher minor or patch version number but the same major version number. For example, upgrading from `v1.2.3` to `v1.2.4` or `v1.3.0`.  

Backward compatibility is **NOT** guaranteed when downgrading to a prior version.

### Major version upgrades

A change in the `major` version number **DOES NOT** come with a backward compatibility guarantee.

All breaking changes introduced **MUST** be indicated in the release notes. 

All efforts **SHOULD** be made to automate or work around the breaking change to minimize the impact on the end user. 

## Minimizing the impact of changes

The following mitigation techniques **SHOULD** be considered when introducing breaking changes is unavoidable.

### Use multiple versions in MR CRDs 

Leverage multiple versions in the generated CRDs for breaking change management. If there’s a breaking change in the API of an MR, publish the new API with a new version so that both the old and the new versions will be available in the OpenAPI schema. This allows the utilization of conversion webhooks between the different versions of a CRD and for proper API deprecation and removal.

### Conversion webhooks 

Having the old APIs stored in a generated CRD, as discussed above with multiple CRD versions, allows for leveraging API converters in the form of Kubernetes conversion webhooks. These could help end users with breaking API changes during the provider upgrade. 

The webhooks should be implemented as part of the provider to simplify deployment.

Upjet provides high-level libraries that make writing these converters fast and robust for specific API changes.

### Upgrade tool/script

An upgrade tool or script could be provided to the end user to automate the necessary changes where API migrations are impossible via conversion webhooks. An automated tool/script is preferable over manual instructions as it reduces the chances of human error. 

### API deprecation notice

APIs should not be removed without prior deprecation notice.

### Detailed release notes

These details include API changes, automatic conversion support, and instructions on migrating the existing APIs.