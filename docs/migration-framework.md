<!--
SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->
## Migration Framework

The [migration package](https://github.com/crossplane/upjet/tree/main/pkg/migration)
in the [upjet](https://github.com/crossplane/upjet) repository contains a framework
that allows users to write converters and migration tools that are suitable for
their system and use. This document will focus on the technical details of the
Migration Framework and how to use it.

The concept of migration in this document is, in its most basic form, the
conversion of a crossplane resource from its current state in the source API to
its new state in the target API.

Let's explain this with an example. For example, a user has a classic
provider-based VPC Managed Resource in her cluster. The user wants to migrate
her system to upjet-based providers. However, she needs to make various
conversions for this. Because there are differences between the API of the VPC
MR in classic provider (source API) and upjet-based provider (target API), such
as group name. While the group value of the VPC MR in the source API is
ec2.aws.crossplane.io, it is ec2.aws.upbound.io in upjet-based providers. There
may also be some changes in other API fields of the resource. The fact that the
values of an existing resource, such as group and kind, cannot be changed
requires that this resource be recreated with the migration procedure without
affecting the user's case.

So, let’s see how the framework can help us to migrate the systems.

### Concepts

The Migration Framework had various concepts for users to perform an end-to-end
migration. These play an essential role in understanding the framework's
structure and how to use it.

#### What Is Migration?

There are two main topics when we talk about the concept of migration. The first
one is API Migration. This includes the migration of the example mentioned above.
API Migration, in its most general form, is the migration from the API/provider
that the user is currently using (e.g. Community Classic) to the target
API/provider (e.g. upjet-based provider). The context of the resources
to be migrated here are MRs and Compositions because there are various API
changes in the related resources.

The other type of migration is the migration of Crossplane Configuration
packages. After the release of family providers, migrating users with monolithic
providers and configuration packages to family providers has come to the agenda.
The migration framework is extended in this context, and monolithic package
references in Crossplane resources such as Provider, Lock, and Configuration are
replaced with family ones. There is no API migration here. Because there is no
API change in the related resources in source and targets. The purpose is only
to provide a smooth transition from monolithic packages to family packages.

#### Converters

Converters convert related resource kind from the source API or structure to the
target API or structure. There are many types of converters supported in the
migration framework. However, in this document, we will talk about converters
and examples for API migration.

1. **Resource Converter** converts a managed resource from the migration source
provider's schema to the migration target provider's schema. The function of
the interface is [Resource](https://github.com/crossplane/upjet/blob/cc55f3952474e51ee31cd645c4a9578248de7f3a/pkg/migration/interfaces.go#L32).
`Resource` takes a managed resource and returns zero or more managed resources to
be created.

```go
Resource(mg resource.Managed) ([]resource.Managed, error)
```

[Here](https://github.com/upbound/extensions-migration/blob/main/converters/provider-aws/kafka/cluster.go)
is an example.
As can be seen in the example, the [CopyInto](https://github.com/upbound/extensions-migration/blob/3c1d4cd0717fa915d7f23455c6622b9190e5bd6d/converters/provider-aws/kafka/cluster.go#L29)
function was called before starting the change in the resource. This function
copies all fields that can be copied from the source API to the target API. The
function may encounter an error when copying some fields. In this case, these
fields should be passed to the `skipFieldPaths` value of the function. This way,
the function will not try to copy these fields. In the Kafka Cluster resource in
the example, there have been various changes in the Spec fields, such as Group
and Kind. Related converters should be written to handle these changes. The main
points to be addressed in this regard are listed below.

- Changes in Group and Kind names.
  - Changes in the Spec field.
    - Changes in the [Field](https://github.com/upbound/extensions-migration/blob/3c1d4cd0717fa915d7f23455c6622b9190e5bd6d/converters/provider-aws/ec2/vpc.go#L38)
      name. Changes due to differences such as lower case upper
      case. The important thing here is not the field's Go name but the json path's
      name. Therefore, the changes here should be made considering the changes in the
      json name.
    ```go
    target.Spec.ForProvider.EnableDNSSupport = source.Spec.ForProvider.EnableDNSSupport
    target.Spec.ForProvider.EnableDNSHostnames = source.Spec.ForProvider.EnableDNSHostNames
    ```
    - Fields with [completely changed](https://github.com/upbound/extensions-migration/blob/3c1d4cd0717fa915d7f23455c6622b9190e5bd6d/converters/provider-aws/ec2/vpc.go#L39)
      names. You may need to review the API documentation to understand them.
    ```go
    target.Spec.ForProvider.AssignGeneratedIPv6CidrBlock = source.Spec.ForProvider.AmazonProvidedIpv6CIDRBlock
    ```
    - Changes in the [field's type](https://github.com/upbound/extensions-migration/blob/3c1d4cd0717fa915d7f23455c6622b9190e5bd6d/converters/provider-aws/ec2/vpc.go#L31).
      Such as a value that was previously Integer
      changing to [Float64](https://github.com/upbound/extensions-migration/blob/3c1d4cd0717fa915d7f23455c6622b9190e5bd6d/converters/provider-aws/kafka/cluster.go#L38).
    ```go
    target.Spec.ForProvider.Tags = make(map[string]*string, len(source.Spec.ForProvider.Tags))
    for _, t := range source.Spec.ForProvider.Tags {
         v := t.Value
         target.Spec.ForProvider.Tags[t.Key] = &v
    }
    ```
    ```go
    target.Spec.ForProvider.ConfigurationInfo[0].Revision = common.PtrFloat64FromInt64(source.Spec.ForProvider.CustomConfigurationInfo.Revision)
    ```
    - In Upjet-based providers, all structs in the API are defined as Slice. This is
      not the case in Classic Providers. For this reason, this situation should be
      taken into consideration when making the relevant [struct transformations](https://github.com/upbound/extensions-migration/blob/3c1d4cd0717fa915d7f23455c6622b9190e5bd6d/converters/provider-aws/kafka/cluster.go#L40).
    ```go
    if source.Spec.ForProvider.EncryptionInfo != nil {
        target.Spec.ForProvider.EncryptionInfo = make([]targetv1beta1.EncryptionInfoParameters, 1)
        target.Spec.ForProvider.EncryptionInfo[0].EncryptionAtRestKMSKeyArn = source.Spec.ForProvider.EncryptionInfo.EncryptionAtRest.DataVolumeKMSKeyID
            if source.Spec.ForProvider.EncryptionInfo.EncryptionInTransit != nil {
               target.Spec.ForProvider.EncryptionInfo[0].EncryptionInTransit = make([]targetv1beta1.EncryptionInTransitParameters, 1)
               target.Spec.ForProvider.EncryptionInfo[0].EncryptionInTransit[0].InCluster = source.Spec.ForProvider.EncryptionInfo.EncryptionInTransit.InCluster
               target.Spec.ForProvider.EncryptionInfo[0].EncryptionInTransit[0].ClientBroker = source.Spec.ForProvider.EncryptionInfo.EncryptionInTransit.ClientBroker
            }
      }
    ```
- External name conventions may differ between upjet-based providers and classic
  providers. For this reason, external name conversions of related resources
  should also be done in converter functions.

Another important case is when an MR in the source API corresponds to more than
one MR in the target API. Since the [Resource](https://github.com/crossplane/upjet/blob/cc55f3952474e51ee31cd645c4a9578248de7f3a/pkg/migration/interfaces.go#L32)
function returns a list of MRs, this infrastructure is ready. There is also an
example converter [here](https://github.com/upbound/extensions-migration/blob/main/converters/provider-aws/ec2/routetable.go).

2. **Composed Template Converter** converts a Composition's ComposedTemplate
from the migration source provider's schema to the migration target provider's
schema. Conversion of the `Base` must be handled by a ResourceConverter.
This interface has a function [ComposedTemplate](https://github.com/crossplane/upjet/blob/cc55f3952474e51ee31cd645c4a9578248de7f3a/pkg/migration/interfaces.go#L49).
`ComposedTemplate` receives a migration source v1.ComposedTemplate that has been
converted, by a resource converter, to the v1.ComposedTemplates with the new
shapes specified in the `convertedTemplates` argument. Conversion of the
v1.ComposedTemplate.Bases is handled via ResourceConverter.Resource and
ComposedTemplate must only convert the other fields (`Patches`,
`ConnectionDetails`, `PatchSet`s, etc.) Returns any errors encountered.

```go
ComposedTemplate(sourceTemplate xpv1.ComposedTemplate, convertedTemplates ...*xpv1.ComposedTemplate) error
```

There is a generic Composed Template implementation [DefaultCompositionConverter](https://github.com/crossplane/upjet/blob/cc55f3952474e51ee31cd645c4a9578248de7f3a/pkg/migration/registry.go#L481).
`DefaultCompositionConverter` is a generic composition converter. It takes a
`conversionMap` that is fieldpath map for conversion. Key of the conversionMap
points to the source field and the Value of the conversionMap points to the
target field. Example: "spec.forProvider.assumeRolePolicyDocument": "spec.forProvider.assumeRolePolicy".
And the fns are functions that manipulate the patchsets.

3. **PatchSetConverter** converts patch sets of Compositions. Any registered
PatchSetConverters will be called before any resource or ComposedTemplate
conversion is done. The rationale is to convert the Composition-wide patch sets
before any resource-specific conversions so that migration targets can
automatically inherit converted patch sets if their schemas match them.
Registered PatchSetConverters will be called in the order they are registered.
This interface has function [PatchSets](https://github.com/crossplane/upjet/blob/cc55f3952474e51ee31cd645c4a9578248de7f3a/pkg/migration/interfaces.go#L69).
`PatchSets` converts the `spec.patchSets` of a Composition from the migration
source provider's schema to the migration target provider's schema.

```go
PatchSets(psMap map[string]*xpv1.PatchSet) error
```

There is a [common PatchSets implementation](https://github.com/upbound/extensions-migration/blob/3c1d4cd0717fa915d7f23455c6622b9190e5bd6d/converters/provider-aws/common/common.go#L14)
for provider-aws resources.

```
NOTE: Unlike MR converters, Composition converters and PatchSets converters can
contain very specific cases depending on the user scenario. Therefore, it is not
possible to write a universal migrator in this context. This is due to the fact
that all compositions are inherently different, although some helper functions
can be used in common.
```

#### Registry

Registry is a bunch of converters. Every Converter is keyed with the associated 
`schema.GroupVersionKind`s and an associated `runtime.Scheme` with which the
corresponding types are registered. All converters intended to be used during
migration must be registered in the Registry. For Kinds that are not registered
in the Registry, no conversion will be performed, even if the resource is
included and read in the Source.

Before registering converters in the registry, the source and target API schemes
need to be added to the registry so that the respective Kinds are recognized.

```go
sourceF := sourceapis.AddToScheme
targetF := targetapis.AddToScheme
if err := common.AddToScheme(registry, sourceF, targetF); err != nil {
	panic(err)
}
```

In addition, the Composition type and, if applicable, the Composite and Claim
types must also be defined in the registry before the converters are registered.

```go
if err := registry.AddCompositionTypes(); err != nil {
    panic(err)
}
registry.AddClaimType(...)
registry.AddCompositeType(...)
```

The `RegisterAPIConversionFunctions` is used for registering the API conversion
functions. These functions are, Resource Converters, Composition Converters and
PatchSet Converters.

```go
registry.RegisterAPIConversionFunctions(ec2v1beta1.VPCGroupVersionKind, ec2.VPCResource, 
	migration.DefaultCompositionConverter(nil, common.ConvertComposedTemplateTags), 
	common.DefaultPatchSetsConverter)
```

#### Source

[Source](https://github.com/crossplane/upjet/blob/cc55f3952474e51ee31cd645c4a9578248de7f3a/pkg/migration/interfaces.go#L114)
is a source for reading resource manifests. It is an interface used to read the
resources subject to migration.

There were currently two implementations of the Source interface.

[File System Source](https://github.com/crossplane/upjet/blob/main/pkg/migration/filesystem.go)
is a source implementation to read resources from the file system. It is the
source type that the user will use to read the resources that they want to
migrate for their cases, such as those in their local system, those in a GitHub
repository, etc.

[Kubernetes Source](https://github.com/crossplane/upjet/blob/main/pkg/migration/kubernetes.go)
is a source implementation to read resources from Kubernetes cluster. It is the
source type that the user will use to read the resources that they want to
migrate for their cases in their Kubernetes Cluster.  There are two types for
reading the resources from the Kubernetes cluster.

The important point here is that the Kubernetes source does not read all the
resources in the cluster. Kubernetes Source reads using two ways. The first one
is when the user specifies the category when initializing the Migration Plan.
If this is done, Kubernetes Source will read all resources belonging to the
specified categories. Example categories: managed, claim, etc. The other way is
the Kinds of the converters registered in the Registry. As it is known, every
converter registered in the registry is registered according to a specific type.
Kubernetes Source reads the resources of the registered converter Kinds. For
example, if a converter of VPC Kind is registered in the registry, Kubernetes
Source will read the resources of the VPC type in the cluster.

```
NOTE: Multiple source is allowed. While creating the Plan Generator object,
by using the following option, you can register both sources.

migration.WithMultipleSources(sources...)
```

#### Target

[Target](https://github.com/crossplane/upjet/blob/cc55f3952474e51ee31cd645c4a9578248de7f3a/pkg/migration/interfaces.go#L132)
is a target where resource manifests can be manipulated
(e.g., added, deleted, patched, etc.). It is the interface for storing the
manifests resulting from the migration steps. Currently only File System Target
is supported. In other words, the converted manifests that the user will see as
output when they start the migration process will be stored in the File System.

#### Migration Plan

Plan represents a migration plan for migrating managed resources, and associated
composites and claims from a migration source provider to a migration target provider.

PlanGenerator generates a Migration Plan reading the manifests available from 
`source`, converting managed resources and compositions using the available 
`Converter`s registered in the `registry` and writing the output manifests to
the specified `target`.

There is an example plan:

```yaml
spec:
  steps:
  - patch:
      type: merge
      files:
      - pause-managed/sample-vpc.vpcs.fakesourceapi.yaml
    name: pause-managed
    manualExecution:
      - "kubectl patch --type='merge' -f pause-managed/sample-vpc.vpcs.fakesourceapi.yaml --patch-file pause-managed/sample-vpc.vpcs.fakesourceapi.yaml"
    type: Patch

  - patch:
      type: merge
      files:
      - pause-composites/my-resource-dwjgh.xmyresources.test.com.yaml
    name: pause-composites
    manualExecution:
      - "kubectl patch --type='merge' -f pause-composites/my-resource-dwjgh.xmyresources.test.com.yaml --patch-file pause-composites/my-resource-dwjgh.xmyresources.test.com.yaml"
    type: Patch

  - apply:
      files:
      - create-new-managed/sample-vpc.vpcs.faketargetapi.yaml
    name: create-new-managed
    manualExecution:
      - "kubectl apply -f create-new-managed/sample-vpc.vpcs.faketargetapi.yaml"
    type: Apply

  - apply:
      files:
      - new-compositions/example-migrated.compositions.apiextensions.crossplane.io.yaml
    name: new-compositions
    manualExecution:
      - "kubectl apply -f new-compositions/example-migrated.compositions.apiextensions.crossplane.io.yaml"
    type: Apply

  - patch:
      type: merge
      files:
      - edit-composites/my-resource-dwjgh.xmyresources.test.com.yaml
    name: edit-composites
    manualExecution:
      - "kubectl patch --type='merge' -f edit-composites/my-resource-dwjgh.xmyresources.test.com.yaml --patch-file edit-composites/my-resource-dwjgh.xmyresources.test.com.yaml"
    type: Patch

  - patch:
      type: merge
      files:
      - edit-claims/my-resource.myresources.test.com.yaml
    name: edit-claims
    manualExecution:
      - "kubectl patch --type='merge' -f edit-claims/my-resource.myresources.test.com.yaml --patch-file edit-claims/my-resource.myresources.test.com.yaml"
    type: Patch

  - patch:
      type: merge
      files:
      - deletion-policy-orphan/sample-vpc.vpcs.fakesourceapi.yaml
    name: deletion-policy-orphan
    manualExecution:
      - "kubectl patch --type='merge' -f deletion-policy-orphan/sample-vpc.vpcs.fakesourceapi.yaml --patch-file deletion-policy-orphan/sample-vpc.vpcs.fakesourceapi.yaml"
    type: Patch

  - delete:
      options:
        finalizerPolicy: Remove
      resources:
      - group: fakesourceapi
        kind: VPC
        name: sample-vpc
        version: v1alpha1
    name: delete-old-managed
    manualExecution:
      - "kubectl delete VPC.fakesourceapi sample-vpc"
    type: Delete

  - patch:
      type: merge
      files:
      - start-managed/sample-vpc.vpcs.faketargetapi.yaml
    name: start-managed
    manualExecution:
      - "kubectl patch --type='merge' -f start-managed/sample-vpc.vpcs.faketargetapi.yaml --patch-file start-managed/sample-vpc.vpcs.faketargetapi.yaml"
    type: Patch

  - patch:
      type: merge
      files:
      - start-composites/my-resource-dwjgh.xmyresources.test.com.yaml
    name: start-composites
    manualExecution:
      - "kubectl patch --type='merge' -f start-composites/my-resource-dwjgh.xmyresources.test.com.yaml --patch-file start-composites/my-resource-dwjgh.xmyresources.test.com.yaml"
    type: Patch

version: 0.1.0
```

As can be seen here, the Plan includes steps on how to migrate the user's
resources. The output manifests existing in the relevant steps are the outputs
of the converters registered by the user. Therefore, it should be underlined
once again that converters are the ones that do the actual conversion during
migration.

While creating a Plan Generator object, user may set some options by using the
option functions. The most important two option functions are:

- WithErrorOnInvalidPatchSchema returns a PlanGeneratorOption for configuring 
whether the PlanGenerator should error and stop the migration plan generation in
case an error is encountered while checking a patch statement's conformance to
the migration source or target.

- WithSkipGVKs configures the set of GVKs to skip for conversion during a
migration.

### Example Usage - Template Migrator

In the [upbound/extensions-migration](https://github.com/upbound/extensions-migration)
repository there are two important things for the Migration Framework. One of
them is the [common converters](https://github.com/upbound/extensions-migration/tree/main/converters/provider-aws).
These converters include previously written API converters. The relevant
converters are added here for re-use in different migrators. By reviewing
converters here, in addition to this document, you can better understand how to
write a converter.

The second part is the [template migrator](https://github.com/upbound/extensions-migration/blob/main/converters/template/cmd/main.go).
Here, it will be possible to generate a Plan by using the above-mentioned
capabilities of the migration framework. It is worth remembering that since each
source has its own characteristics, the user has to make various changes on the
template.
