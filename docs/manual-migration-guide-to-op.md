## Manual Migration Guide to Official Providers

This document describes the steps that need to be applied to migrate from
community providers to official providers manually. We plan to implement a
client-based tool to automate this process. 

For the sake of simplicity, we only focus on migrating managed resources 
and compositions in this guide. These scenarios can be extended
with other tools like ArgoCD, Flux, Helm, Kustomize, etc.

### Migrating Managed Resources

Migrating existing managed resources to official providers can be simplified
as import scenarios. The aim is to modify the community provider's scheme to official
providers and apply those manifests to import existing cloud resources.

To prevent a conflict between two provider controllers reconciling for the same external resource,
we're scaling down the old provider. This can also be eliminated with the new [pause annotation feature].


1) Backup managed resource manifests
```bash
kubectl get managed -o yaml > backup-mrs.yaml
```
2) Update deletion policy to `Orphan` with the command below:
```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec": {"deletionPolicy":"Orphan"}}' --type=merge
```
3) Install the official provider
4) Install provider config
5) Update managed resource manifests to the new API version `upbound.io`, external-name annotations and new field names/types. You can use
[Upbound Marketplace] for comparing CRD schema changes. It is also planned to extend current documentation with external-name syntax in this [issue].
```bash
cp backup-mrs.yaml op-mrs.yaml
vi op-mrs.yaml
```
6) Scale down Crossplane deployment
```bash
kubectl scale deploy crossplane --replicas=0
```
7) Scale down native provider deployment
```bash
kubectl scale deploy ${deployment_name} --replicas=0
```
8) Apply updated managed resources and wait until they become ready
```bash
kubectl apply -f op-mrs.yaml
```
9) Delete old MRs
```bash
kubectl delete -f backup-mrs.yaml
kubectl patch -f backup-mrs.yaml -p '{"metadata":{"finalizers":[]}}' --type=merge
```
10) Delete old provider config
```bash
kubectl delete providerconfigs ${provider_config_name}
```
11) Delete old provider
```bash
kubectl delete providers ${provider_name}
```
12) Scale up Crossplane deployment
```bash
kubectl scale deploy crossplane --replicas=1
``` 

#### Migrating VPC Managed Resource

In below, we display the required changes to migrate a native provider-aws VPC resource to an official 
provider-aws VPC. As you can see, we have updated the API version and some field names/types in spec 
and status subresources. To find out which fields to update, we need to compare the CRDs in the current
provider version and the target official provider version. 

```diff
-   apiVersion: ec2.aws.crossplane.io/v1beta1
+   apiVersion: ec2.aws.upbound.io/v1beta1  
    kind: VPC
    metadata:
      annotations:
        crossplane.io/external-create-pending: "2022-09-23T12:20:31Z"
        crossplane.io/external-create-succeeded: "2022-09-23T12:20:33Z"
        crossplane.io/external-name: vpc-008f150c8f525bf24
        kubectl.kubernetes.io/last-applied-configuration: |
          {"apiVersion":"ec2.aws.crossplane.io/v1beta1","kind":"VPC","metadata":{"annotations":{},"name":"ezgi-vpc"},"spec":{"deletionPolicy":"Delete","forProvider":{"cidrBlock":"192.168.0.0/16","enableDnsHostNames":true,"enableDnsSupport":true,"instanceTenancy":"default","region":"us-west-2","tags":[{"key":"Name","value":"platformref-vpc"},{"key":"Owner","value":"Platform Team"},{"key":"crossplane-kind","value":"vpc.ec2.aws.crossplane.io"},{"key":"crossplane-name","value":"ezgi-plat-ref-aws-tcg6t-n6zph"},{"key":"crossplane-providerconfig","value":"default"}]},"providerConfigRef":{"name":"default"}}}
      creationTimestamp: "2022-09-23T12:18:21Z"
      finalizers:
      - finalizer.managedresource.crossplane.io
      generation: 2
      name: ezgi-vpc
      resourceVersion: "22685"
      uid: 81211d98-57f2-4f2e-a6db-04bb75cc60ff
    spec:
      deletionPolicy: Delete
      forProvider:
        cidrBlock: 192.168.0.0/16
-       enableDnsHostNames: true
+       enableDnsHostnames: true
        enableDnsSupport: true
        instanceTenancy: default
        region: us-west-2
        tags:
-       - key: Name
-         value: platformref-vpc
-       - key: Owner
-         value: Platform Team
-       - key: crossplane-kind
-         value: vpc.ec2.aws.crossplane.io
-       - key: crossplane-name
-         value: ezgi-vpc
-       - key: crossplane-providerconfig
-         value: default
+         Name: platformref-vpc
+         Owner: Platform Team
+         crossplane-kind: vpc.ec2.aws.upbound.io
+         crossplane-name: ezgi-vpc
+         crossplane-providerconfig: default
      providerConfigRef:
        name: default
```


### Migrating Crossplane Configurations

Configuration migration can be more challenging. Because, in addition to managed resource migration, we need to 
update our composition and claim files to match the new CRDs. Just like managed resource migration, we first start to import
our existing resources to official provider and then update our configuration package version to point to the 
official provider. 


1) Backup managed resource manifests
```bash
kubectl get managed -o yaml > backup-mrs.yaml
```
2) Scale down Crossplane deployment
```bash
kubectl scale deploy crossplane --replicas=0
```
3) Update deletion policy to `Orphan` with the command below:
```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec": {"deletionPolicy":"Orphan"}}' --type=merge
```
4) Update composition files to the new API version `upbound.io`, external-name annotations and new field names/types. You can use
[Upbound Marketplace] for comparing CRD schema changes. It is also planned to extend current documentation with external-name syntax in this [issue].
5) Update `crossplane.yaml` file with official provider dependency.
6) Build and push the new configuration version
7) Install Official Provider
8) Install provider config
9) Update managed resource manifests with the same changes done on composition files
```bash
cp backup-mrs.yaml op-mrs.yaml
vi op-mrs.yaml
```
10) Scale down native provider deployment
```bash
kubectl scale deploy ${deployment_name} --replicas=0
```
11) Apply updated managed resources and wait until they become ready
```bash
kubectl apply -f op-mrs.yaml
```
12) Delete old MRs
```bash
kubectl delete -f backup-mrs.yaml
kubectl patch -f backup-mrs.yaml -p '{"metadata":{"finalizers":[]}}' --type=merge
```
13) Update the configuration to the new version
```bash
cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Configuration
metadata:
  name: ${configuration_name}
spec:
  package: ${configuration_registry}/${configuration_repository}:${new_version}
EOF
```
14) Scale up Crossplane deployment
```bash
kubectl scale deploy crossplane --replicas=1
```
15) Delete old provider config
```bash
kubectl delete providerconfigs ${provider_config_name}
```
16) Delete old provider
```bash
kubectl delete providers ${provider_name}
```

#### Migrating VPC in a Composition

In the below, there is a small code snippet from platform-ref-aws to update VPC resource.  

```diff
   resources:
     - base:
-        apiVersion: ec2.aws.crossplane.io/v1beta1
+        apiVersion: ec2.aws.upbound.io/v1beta1
         kind: VPC
         spec:
           forProvider:
             spec:
               region: us-west-2
               cidrBlock: 192.168.0.0/16
               enableDnsSupport: true
-              enableDnsHostNames: true
+              enableDnsHostnames: true
-              tags:
-              - key: Owner
-                value: Platform Team
-              - key: Name
-                value: platformref-vpc
+              tags:
+                Owner: Platform Team
+                Name: platformref-vpc
       name: platformref-vcp
```


PRs which fully update existing platform-refs can be found below:
- platform-ref-aws: https://github.com/upbound/platform-ref-aws/pull/69
- platform-ref-azure: https://github.com/upbound/platform-ref-azure/pull/10
- platform-ref-gcp: https://github.com/upbound/platform-ref-gcp/pull/22

[pause annotation feature]: https://github.com/upbound/product/issues/227
[Upbound Marketplace]: https://marketplace.upbound.io/
[issue]: https://github.com/upbound/official-providers/issues/792