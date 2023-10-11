<!--
SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->

# Adding Support for Management Policies and initProvider

## Regenerating a provider with Management Policies

Check out the provider repo, e.g., upbound/provider-aws, and go to the project
directory on your local machine.

1. Generate with management policy and update crossplane-runtime dependency:

    ```bash
    # Consume the latest crossplane-tools:
    go get github.com/crossplane/crossplane-tools@master
    go mod tidy
    # Generate getters/setters for management policies
    make generate
    
    # Consume the latest crossplane-runtime:
    go get github.com/crossplane/crossplane-runtime@master
    go mod tidy
    ```

1. Introduce a feature flag for `Management Policies`.

    Add the feature flag definition into the `internal/features/features.go`
    file.

    ```diff
    diff --git a/internal/features/features.go b/internal/features/features.go
    index 9c6b1fc8..de261ca4 100644
    --- a/internal/features/features.go
    +++ b/internal/features/features.go
    @@ -12,4 +12,9 @@ const (
            // External Secret Stores. See the below design for more details.
            // https://github.com/crossplane/crossplane/blob/390ddd/design/design-doc-external-secret-stores.md
            EnableAlphaExternalSecretStores feature.Flag = "EnableAlphaExternalSecretStores"
    +
    +       // EnableAlphaManagementPolicies enables alpha support for
    +       // Management Policies. See the below design for more details.
    +       // https://github.com/crossplane/crossplane/pull/3531
    +       EnableAlphaManagementPolicies feature.Flag = "EnableAlphaManagementPolicies"
     )
    ```

   Add the actual flag in `cmd/provider/main.go` file and pass the flag to the
   workspace store:

    ```diff
    diff --git a/cmd/provider/main.go b/cmd/provider/main.go
    index 669b01f9..a60df983 100644
    --- a/cmd/provider/main.go
    +++ b/cmd/provider/main.go
    @@ -48,6 +48,7 @@ func main() {

                    namespace                  = app.Flag("namespace", "Namespace used to set as default scope in default secret store config.").Default("crossplane-system").Envar("POD_NAMESPACE").String()
                    enableExternalSecretStores = app.Flag("enable-external-secret-stores", "Enable support for ExternalSecretStores.").Default("false").Envar("ENABLE_EXTERNAL_SECRET_STORES").Bool()
    +               enableManagementPolicies   = app.Flag("enable-management-policies", "Enable support for Management Policies.").Default("false").Envar("ENABLE_MANAGEMENT_POLICIES").Bool()
            )

            kingpin.MustParse(app.Parse(os.Args[1:]))
    @@ -122,6 +123,11 @@ func main() {
                    })), "cannot create default store config")
            }
                        terraform.WithSharedProviderOptions(terraform.WithNativeProviderPath(*setupConfig.NativeProviderPath), terraform.WithNativeProviderName("registry.terraform.io/"+*setupConfig.NativeProviderSource)))
        }

   +       featureFlags := &feature.Flags{}
           o := tjcontroller.Options{
                   Options: xpcontroller.Options{
                           Logger:                  log,
                           GlobalRateLimiter:       ratelimiter.NewGlobal(*maxReconcileRate),
                           PollInterval:            *pollInterval,
                           MaxConcurrentReconciles: *maxReconcileRate,
   -                       Features:                &feature.Flags{},
   +                       Features:                featureFlags,
                   },

                   Provider:       config.GetProvider(),
       -           WorkspaceStore: terraform.NewWorkspaceStore(log, terraform.WithDisableInit(len(*setupConfig.NativeProviderPath) != 0), terraform.WithProcessReportInterval(*pollInterval)),
       +           WorkspaceStore: terraform.NewWorkspaceStore(log, terraform.WithDisableInit(len(*setupConfig.NativeProviderPath) != 0), terraform.WithProcessReportInterval(*pollInterval), terraform.WithFeatures(featureFlags)),
                   SetupFn:        clients.SelectTerraformSetup(log, setupConfig),
                   EventHandler:   eventHandler,
           }

       +      if *enableManagementPolicies {
       +              o.Features.Enable(features.EnableAlphaManagementPolicies)
       +              log.Info("Alpha feature enabled", "flag", features.EnableAlphaManagementPolicies)
       +      }
       +
               kingpin.FatalIfError(controller.Setup(mgr, o), "Cannot setup AWS controllers")
               kingpin.FatalIfError(mgr.Start(ctrl.SetupSignalHandler()), "Cannot start controller manager")
        }
       ```

> [!NOTE]
> If the provider was already updated to support observe-only resources, just
  add the feature flag to the `workspaceStore`.

1. Generate with the latest upjet and management policies:

    ```bash
    # Bump to the latest upjet
    go get github.com/crossplane/upjet@main
    go mod tidy
    ```

   Enable management policies in the generator by adding
   `config.WithFeaturesPackage` option:

    ```diff
    diff --git a/config/provider.go b/config/provider.go
    index 964883670..1c06a53e2 100644
    --- a/config/provider.go
    +++ b/config/provider.go
    @@ -141,6 +141,7 @@ func GetProvider() *config.Provider {
                  config.WithReferenceInjectors([]config.ReferenceInjector{reference.NewInjector(modulePath)}),
                  config.WithSkipList(skipList),
                  config.WithBasePackages(BasePackages),
    +             config.WithFeaturesPackage("internal/features"),
                  config.WithDefaultResourceOptions(
                          GroupKindOverrides(),
                          KindOverrides(),
    ```

   Generate:

    ```bash
    make generate
    ```

## Testing: Locally Running the Provider with Management Policies Enabled

1. Create a fresh Kubernetes cluster.
1. Apply all of the provider's CRDs with `kubectl apply -f package/crds`.
1. Run the provider with `--enable-management-policies`.

   You can update the `run` target in the Makefile as below

    ```diff
    diff --git a/Makefile b/Makefile
    index d529a0d6..84411669 100644
    --- a/Makefile
    +++ b/Makefile
    @@ -111,7 +111,7 @@ submodules:
     run: go.build
            @$(INFO) Running Crossplane locally out-of-cluster . . .
            @# To see other arguments that can be provided, run the command with --help instead
    -       UPBOUND_CONTEXT="local" $(GO_OUT_DIR)/provider --debug
    +       UPBOUND_CONTEXT="local" $(GO_OUT_DIR)/provider --debug --enable-management-policies
    ```

    and run with:

    ```shell
    make run
    ```

1. Create some resources in the provider's management console and try observing
them by creating a managed resource with `managementPolicies: ["Observe"]`.

    For example:

    ```yaml
    apiVersion: rds.aws.upbound.io/v1beta1
    kind: Instance
    metadata:
      name: an-existing-dbinstance
    spec:
      managementPolicies: ["Observe"]
      forProvider:
        region: us-west-1
    ```

    You should see the managed resource is ready & synced:

    ```bash
    NAME                              READY   SYNCED   EXTERNAL-NAME                     AGE
    an-existing-dbinstance            True    True     an-existing-dbinstance            3m
    ```

    and the `status.atProvider` is updated with the actual state of the resource:

    ```bash
    kubectl get instance.rds.aws.upbound.io an-existing-dbinstance -o yaml
    ```

> [!NOTE]
> You need the `terraform` executable installed on your local machine.

1. Create a managed resource without `LateInitialize` like
`managementPolicies: ["Observe", "Create", "Update", "Delete"]` with
`spec.initProvider` fields to see the provider create the resource with
combining `spec.initProvider` and `spec.forProvider` fields:

   For example:

   ```yaml
   apiVersion: dynamodb.aws.upbound.io/v1beta1
   kind: Table
   metadata:
     name: example
   annotations:
     meta.upbound.io/example-id: dynamodb/v1beta1/table
   spec:
    managementPolicies: ["Observe", "Create", "Update", "Delete"]
    initProvider:
      writeCapacity: 20
      readCapacity: 19
    forProvider:
      region: us-west-1
      attribute:
      - name: UserId
        type: S
      - name: GameTitle
        type: S
      - name: TopScore
        type: "N"
      billingMode: PROVISIONED
      globalSecondaryIndex:
        - hashKey: GameTitle
          name: GameTitleIndex
          nonKeyAttributes:
            - UserId
          projectionType: INCLUDE
          rangeKey: TopScore
          readCapacity: 10
          writeCapacity: 10
          hashKey: UserId
          rangeKey: GameTitle
   ```

   You should see the managed resource is ready & synced:

    ```bash
    NAME                              READY   SYNCED   EXTERNAL-NAME                     AGE
    example                           True    True     example                           3m
    ```

   and the `status.atProvider` is updated with the actual state of the resource,
   including the `initProvider` fields:

    ```bash
    kubectl get tables.dynamodb.aws.upbound.io example  -o yaml
    ```

   As the late initialization is skipped, the `spec.forProvider` should be the
   same when we created the resource.

   In the provider console, you should see that the resource was created with
   the values in the `initProvider` field.
