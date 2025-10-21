<!--
SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->
# Upgrading the provider to Upjet v2

## Overview

Upjet v2 introduces support for generating Crossplane v2 compatible providers with namespaced MRs.

This guide describes how to transition an existing Crossplane v1 Upjet-based provider to support Crossplane v2, using Upjet v2.

### Changes in the namespaced MR APIs

To allow smoother transitions for the provider consumers, Upjet v2 generates providers with both legacy cluster-scoped MRs and modern namespaced MRs.

Namespaced MRs are functionally equivalent to their cluster-scoped counterparts.
To facilitate Crossplane v2 concepts, Upjet v2 has the following changes **for namespaced MR APIs**:

- Namespace-scoped MRs should have the root API group as `acme.m.example.org` to differentiate from the cluster-scoped MRs `acme.example.org`. Although this is not enforced, it is strongly recommended to follow this convention.

- `spec.providerConfigRef` is now a typed reference, with `kind` and `name`. MRs can either reference a cluster-scoped or namespace-scoped provider config.
Providers should introduce namespace-scoped `ProviderConfig.acme.m.example.org` and cluster-scoped `ClusterProviderConfig.acme.m.example.org`.
  - when omitted, defaults to `kind: "ClusterProviderConfig"  name: "default"`.

- Secret references for sensitive input parameters (e.g. `spec.forProvider.fooSecretRef`) and connection secrets (`spec.writeConnectionSecretToRef`) are now generated as local secret references.

- Cross-resource references are generated with optional namespace parameter, that defaults to the same namespace as the MRs. You can make cross-namespace cross-resource references.

- Alpha feature External Secret Store support is dropped from Crossplane v2. Therefore `spec.publishConnectionDetailsTo` is removed from **ALL** MRs.

```yaml
apiGroup: demogroup.acme.m.example.org
kind: DemoResource
metadata:
  name: foo-demo
  namespace: default
spec:
  providerConfigRef:
    kind: ClusterProviderConfig
    name: some-pc
  writeConnectionSecretToRef:
    name: foo-demo-conn
  forProvider:
    coolField: "I am cool"
    passwordSecretRef: # sensitive input parameter
      name: very-important-secret # local k8s secret reference only
    barRef: # cross-resource reference
      name: some-bar-resource
      namespace: other-ns # optional, defaults to same namespace as the MR.
```

- Crossplane c2 has introduced support for `SafeStart` capability, which starts
MR controllers after their CRDs become available.
This needs to be implemented by the provider, as described in this guide.

### Backward-compatibility with Crossplane v1

Providers generated with Upjet v2 is backward compatible with Crossplane v1 environments,
with following the notes:

- Providers still serve legacy cluster-scoped MRs as is.
  - After upgrade, existing cluster-scoped MRs continue to work.
  - Only exception is the removal of `spec.publishConnectionDetailsTo` which was an alpha feature, you need to remove those before upgrading if any usage.  
- `SafeStart` capability will be disabled. The guide explains the implementation details for properly implementing the safe start.
- Namespaced MRs still get installed alongside cluster-scoped MRs. They can be used standalone, but you cannot compose them in Crossplane v1.

## Steps

You can refer to [Crossplane v2 compatibility PR](https://github.com/crossplane/upjet-provider-template/pull/115) in the [crossplane/upjet-provider-template](https://github.com/crossplane/upjet-provider-template) repo.

### Summary

- update to latest Upjet version that supports namespaced resources
- duplicate content into both cluster and namespaced copies and make some minor updates for api groups and import paths
  - config, apis, controllers
- remove all legacy conversion/migration logic, because only the latest version needs to be supported
- update the provider main.go template to setup both cluster and namespaced apis and controllers
- update code generation comment markers to run on both cluster and namespaced types
- update the generator cmd to init both cluster and namespaced config to pass to code gen pipeline
- manually copy and update the handful of manual API and controller files to namespaced dirs

### Update your go.mod to include latest `upjet`, `crossplane-runtime` and `crossplane-tools`

```go
	github.com/crossplane/crossplane-runtime/v2 v2.0.0
	github.com/crossplane/crossplane-tools master
	github.com/crossplane/upjet/v2 v2.1.0
```

```shell
go mod tidy
```

### Bump to the latest build submodule

```shell
cd build
git checkout main
git pull
# back to the repo root
cd ..
```

### Replace all the import paths for crossplane-runtime and upjet to v2

In all of your source files, using your favorite editor or CLI tools like `sed`,
do the following replacements in your import paths

```text
github.com/crossplane/crossplane-runtime/ => github.com/crossplane/crossplane-runtime/v2/
github.com/crossplane/upjet/ => github.com/crossplane/upjet/v2/ 
```

### Remove External Secret Store (ESS)-related APIs

- in `apis/v1alpha1` remove `StoreConfig` api types and registration
[Example commit](https://github.com/crossplane-contrib/provider-upjet-azuread/commit/2ece9b6bd4178fe280d81d401683b0ac70a81bef)

### Refactor repo directory structure for Upjet v2

- move `apis/` to `apis/cluster`, except `generate.go`
- create empty `apis/namespaced` directory
- copy only root api group folders to `apis/namespaced`, e.g. `v1alpha1`, `v1beta1` and any manually authored files if any.

In the `apis/namespaced/<version>/`:

- update api group markers from `yourprovider.crossplane.io` to `yourprovider.m.crossplane.io`, typically in `apis/namespaced/<version>/`:

```go
// Package type metadata.
const (
	Group   = "yourprovider.m.upbound.io"
	Version = "v1beta1"
)
```

- update `scope` markers to namespaced

```go
// +kubebuilder:resource:scope=Namespaced
type ProviderConfig struct {
   ...
}

```

### `apis` directory

- In `apis/namespaced/v1beta1/types.go`, make sure that `ProviderConfigUsage` type inlines `xpv2.TypedProviderConfigUsage`

```diff
import(
	...
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
+	xpv2 "github.com/crossplane/crossplane-runtime/apis/common/v2"
)

type ProviderConfigUsage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

-	xpv1.ProviderConfigUsage `json:",inline"`
+	xpv2.TypedProviderConfigUsage `json:",inline"`
}
```

- Add the new `ClusterProviderConfig` and `ClusterProviderConfigList` types into `apis/namespaced/v1beta1/types.go`
  - You can duplicate existing `ProviderConfig` and `ProviderConfigList` struct definitions, and rename them
  - Ensure that it has the scope kubebuilder marker `Cluster`
  - It is registered to the scheme
  - See [Example Commit](https://github.com/crossplane-contrib/provider-upjet-azuread/commit/613885121e30ae3e46634630164332415a288a10)

```go
// +kubebuilder:object:root=true

// A ClusterProviderConfig configures the Template provider.
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="SECRET-NAME",type="string",JSONPath=".spec.credentials.secretRef.name",priority=1
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:storageversion
type ClusterProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderConfigSpec   `json:"spec"`
	Status ProviderConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterProviderConfigList contains a list of ProviderConfig.
type ClusterProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterProviderConfig `json:"items"`
}
```

### `Makefile`

Update several dependencies to their latest versions. At the time of writing,
these are the latest versions.

```diff
KIND_VERSION = v0.30.0
UP_VERSION = v0.41.0
UP_CHANNEL = stable
CROSSPLANE_VERSION = 2.0.2
```

### `internal/controller`

- Move `internal/controller` to `/internal/controller/cluster`
- In `internal/controller/cluster/providerconfig/config.go`, ensure you pass the `Usage` kind
[example](https://github.com/crossplane-contrib/provider-upjet-azuread/commit/b7d64d88c010f16c36ee4fed9400950b8b6a9ba3)

- In `internal/controller/namespaced/providerconfig/config.go` adjust the setup function, so that it registers controllers for both `ProviderConfig` and `ClusterProviderConfig` types.

- provider-config [example commit](https://github.com/crossplane-contrib/provider-upjet-azuread/commit/f24ecfa3c64112fffb2d2e502752cf095a1944fd)

### Define Controller Gated Setup functions

Ensure the provider config controller setup functions have a new variant `SetupGated`. This should register a func that wraps the original `Setup` call, and specify the GVKs to wait for before doing the controller setup.

`internal/controller/cluster/providerconfig/config.go`

```go
// SetupGated adds a controller that reconciles ProviderConfigs by accounting for
// their current usage.
func SetupGated(mgr ctrl.Manager, o controller.Options) error {
	o.Options.Gate.Register(func() {
		if err := Setup(mgr, o); err != nil {
			mgr.GetLogger().Error(err, "unable to setup reconciler", "gvk", v1beta1.ProviderConfigGroupVersionKind.String())
		}
	}, v1beta1.ProviderConfigGroupVersionKind, v1beta1.ProviderConfigUsageGroupVersionKind)
	return nil
}
```

`internal/controller/namespaced/providerconfig/config.go`

Note that, we specify 3 GVKs here.

```go
// SetupGated adds a controller that reconciles ProviderConfigs by accounting for
// their current usage.
func SetupGated(mgr ctrl.Manager, o controller.Options) error {
	o.Options.Gate.Register(func() {
		if err := Setup(mgr, o); err != nil {
			mgr.GetLogger().Error(err, "unable to setup reconcilers", "gvk", v1beta1.ClusterProviderConfigGroupVersionKind.String(), "gvk", v1beta1.ProviderConfigGroupVersionKind.String())
		}
	}, v1beta1.ClusterProviderConfigGroupVersionKind, v1beta1.ProviderConfigGroupVersionKind, v1beta1.ProviderConfigUsageGroupVersionKind)
	return nil
}
```

[example commit](https://github.com/crossplane-contrib/provider-upjet-azuread/commit/3edaa44a35270175bd6cd7baad05c3d620d90591)

### Kind aware ProviderConfig handling

At this point, you should have 3 provider config API type.

- cluster-scoped `ProviderConfig.template.example.org` in the existing legacy API group
- namespace-scoped `ProviderConfig.template.m.example.org` in the new modern API group
- cluster-scoped `ClusterProviderConfig.template.m.example.org` in the new modern API group

Legacy cluster-scoped MRs should only resolve Legacy ProviderConfig references in the legacy API group.

Modern namespaced MRs should only resolve Modern provider config references, in the modern api group.

After resolving the referenced provider config, convert the resolved config to a common runtime type. `ProviderConfigSpec` type is recommended. This allows the rest of the logic to operate on a single type.
If there are namespaced references to the secrets, overwrite them with MR namespace.

> [!TIP]
> See the [example reference implementation](https://github.com/crossplane/upjet-provider-template/pull/115/files#diff-6f2f274d5365acc2b5e2349350552e6b0b3cc952374a585ee47bb9a1ca96c298) in the provider template repo.

### `config` directory

- duplicate individual resource configurators by their scope. It should be similar to:

```plaintext
config/
  cluster/
    foo/
	  config.go
	bar/
	  config.go
  namespaced/
    foo/
	  config.go
	bar/
	  config.go
  provider-metadata.yaml
  schema.json
  provider.go
```

- in `provider.go` , duplicate the `GetProvider()` function and define the `GetProviderNamespaced()`

- Ensure root group name includes `.m` to distinguish from cluster-scoped API group.

- Ensure the namespaced custom configurations are used for this provider

```go
import (
	// 
	fooCluster "github.com/upbound/upjet-provider-template/config/cluster/foo"
	fooNamespaced "github.com/upbound/upjet-provider-template/config/namespaced/foo"
)

// ...

func GetProviderNamespaced() *ujconfig.Provider {
	pc := ujconfig.NewProvider([]byte(providerSchema), resourcePrefix, modulePath, []byte(providerMetadata),
		ujconfig.WithRootGroup("template.m.upbound.io"),
		// ...
	)

	for _, configure := range []func(provider *ujconfig.Provider){
		// add custom config functions
		fooNamespaced.Configure,
	} {
		configure(pc)
	}
	pc.ConfigureResources()
	return pc
}
```

> [!TIP]
> Check the [example change](https://github.com/crossplane/upjet-provider-template/pull/115/files#diff-483ebdb1139323e5256859b870054a99a208f4a1d60a0f5e51583c22d8600240) upjet provider template.

### `cmd/generator/main.go`

pipeline run should be invoked with both cluster-scoped and namespace-scoped provider

```diff
- pipeline.Run(config.GetProvider(), absRootDir)
+ pipeline.Run(config.GetProvider(), config.GetProviderNamespaced(), absRootDir)
```

### `cmd/provider/main.go`

- import both cluster-scoped and namespaced `apis` and `config` packages

```diff
+	authv1 "k8s.io/api/authorization/v1"
+	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
+	"k8s.io/apimachinery/pkg/runtime/schema"

-	"github.com/upbound/upjet-provider-template/apis"
-	"github.com/upbound/upjet-provider-template/apis/v1alpha1"
+	apisCluster "github.com/upbound/upjet-provider-template/apis/cluster"
+	apisNamespaced "github.com/upbound/upjet-provider-template/apis/namespaced"
	"github.com/upbound/upjet-provider-template/config"
	"github.com/upbound/upjet-provider-template/internal/clients"
-	"github.com/upbound/upjet-provider-template/internal/controller"
+	controllerCluster "github.com/upbound/upjet-provider-template/internal/controller/cluster"
+	controllerNamespaced "github.com/upbound/upjet-provider-template/internal/controller/namespaced"
	"github.com/upbound/upjet-provider-template/internal/features"
+	"github.com/upbound/upjet-provider-template/internal/version"
```

- add both cluster-scoped and namespaced apis to `scheme`
- also add k8s authv1 APIs to the `scheme`
- remove external secret store options if any
- duplicate controller options, `clusterOptions` and `namespacedOptions`

```go
	clusterOpts := tjcontroller.Options{
		//...
		Provider: config.GetProvider(),
	}
	namespacedOpts := tjcontroller.Options{
		//...
		Provider: config.GetProvider(),
	}
```

- make sure if you have some additional configuration, they are added to both, for example:

```go
	if *enableManagementPolicies {
		clusterOpts.Features.Enable(features.EnableBetaManagementPolicies)
		namespacedOpts.Features.Enable(features.EnableBetaManagementPolicies)
		log.Info("Beta feature enabled", "flag", features.EnableBetaManagementPolicies)
	}
```

- setup both cluster-scoped and namespaced controllers
- implement safe-start capability

```go
	canSafeStart, err := canWatchCRD(context.TODO(), mgr)
	kingpin.FatalIfError(err, "SafeStart precheck failed")
	if canSafeStart {
		crdGate := new(gate.Gate[schema.GroupVersionKind])
		clusterOpts.Gate = crdGate
		namespacedOpts.Gate = crdGate
		kingpin.FatalIfError(customresourcesgate.Setup(mgr, xpcontroller.Options{
			Logger:                  log,
			Gate:                    crdGate,
			MaxConcurrentReconciles: 1,
		}), "Cannot setup CRD gate")
		kingpin.FatalIfError(controllerCluster.SetupGated(mgr, clusterOpts), "Cannot setup cluster-scoped Template controllers")
		kingpin.FatalIfError(controllerNamespaced.SetupGated(mgr, namespacedOpts), "Cannot setup namespaced Template controllers")
	} else {
		log.Info("Provider has missing RBAC permissions for watching CRDs, controller SafeStart capability will be disabled")
		kingpin.FatalIfError(controllerCluster.Setup(mgr, clusterOpts), "Cannot setup cluster-scoped Template controllers")
		kingpin.FatalIfError(controllerNamespaced.Setup(mgr, namespacedOpts), "Cannot setup namespaced Template controllers")
	}

	func canWatchCRD(ctx context.Context, mgr manager.Manager) (bool, error) {
	if err := authv1.AddToScheme(mgr.GetScheme()); err != nil {
		return false, err
	}
	verbs := []string{"get", "list", "watch"}
	for _, verb := range verbs {
		sar := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Group:    "apiextensions.k8s.io",
					Resource: "customresourcedefinitions",
					Verb:     verb,
				},
			},
		}
		if err := mgr.GetClient().Create(ctx, sar); err != nil {
			return false, errors.Wrapf(err, "unable to perform RBAC check for verb %s on CustomResourceDefinitions", verbs)
		}
		if !sar.Status.Allowed {
			return false, nil
		}
	}
	return true, nil
}
```

> [!TIP]
> Check the [example commit](https://github.com/crossplane/upjet-provider-template/pull/115/files#diff-36b6d20eb5aea66cf39f8d94111bd96513626ef7f61459f0d9e8e9507ded1d17) in the upjet provider template.

### `package/crossplane.yaml`

After implementing the safe start capability in the above steps,
mark your provider as `SafeStart` capable in the package metadata.

```diff
apiVersion: meta.pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-azuread
  annotations:
    ...
spec:
+  capabilities:
+  - SafeStart
```

### `examples-generated`

- fully remove the contents of this directory, it will be regenerated

### examples

Add examples for the new API groups.

- Update `apiGroup` to foo.m.crossplane.io
- add `metadata.namespace`
- remove `namespace` from all `SecretRef` fields in `spec.forProvider` if any
- remove `namespace` from `spec.writeConnectionSecretToRef`

### Uptest

If you have an Uptest setup script and you create a default provider config
for tests, make sure that you create a provider config with the new
API group `ClusterProviderConfig.foo.m.crossplane.io`.

[example](https://github.com/crossplane-contrib/provider-upjet-azuread/commit/4a5c1acafe2dd2981de1e02279740e6b9a3b7d91)

### Generate the provider

```sh
make generate
```

After generating the provider:

- check `apis/namespaced` directory and ensure namespaced types are generated.
- check `internal/controller/namespaced` directory for namespaced controllers
- check the `package/crds` directory and ensure namespaced CRDs are generated.
- do a local deploy of the provider

```sh
make local-deploy
```

This will create a kind cluster with your provider deployed. Apply some example MRs to validate.
Optionally use Uptest.
