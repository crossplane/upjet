# Generating a Crossplane Provider

In this guide, we will generate a Crossplane provider based on an existing
Terraform provider using Upjet.

We have chosen [Terraform GitHub provider] as an example, but the process will
be quite similar for any other Terraform provider. We will use `myorg` as the
example organization name to be used.

## Generate

1. Generate a GitHub repository for the Crossplane provider by hitting the
   "**Use this template**" button in [upjet-provider-template] repository.
2. Clone the repository to your local and `cd` into the repository directory.
   Fetch the [upbound/build] submodule by running the following:

    ```bash
    make submodules
    ```

3. Replace `template` with your provider name.

    1. Run the `./hack/prepare.sh` script from repo root to prepare the repo, e.g., to
       replace all occurrences of `template` with your provider name and `upbound`
       with your organization name:

        ```bash
       ./hack/prepare.sh
        ```

4. To configure the Terraform provider to generate from, update the following
   variables in `Makefile`:

    ```makefile
    export TERRAFORM_PROVIDER_SOURCE := integrations/github
    export TERRAFORM_PROVIDER_REPO := https://github.com/integrations/terraform-provider-github
    export TERRAFORM_PROVIDER_VERSION := 5.5.0
    export TERRAFORM_PROVIDER_DOWNLOAD_NAME := terraform-provider-github
    export TERRAFORM_NATIVE_PROVIDER_BINARY := terraform-provider-github_v5.5.0_x5
    export TERRAFORM_DOCS_PATH := website/docs/r
    ```

   You can find `TERRAFORM_PROVIDER_SOURCE` and `TERRAFORM_PROVIDER_VERSION` in
   [Terraform GitHub provider] documentation by hitting the "**USE PROVIDER**"
   button. Check [this line in controller Dockerfile] to see how these
   variables are used to build the provider plugin binary. `TERRAFORM_DOCS_PATH`
   is the directory where resource documentation is stored in the repository of
   the Terraform provider.

5. Implement `ProviderConfig` logic. In `upjet-provider-template`, there is already
   a boilerplate code in file `internal/clients/github.go` which
   takes care of properly fetching secret data referenced from `ProviderConfig`
   resource.

   For our GitHub provider, we need to check [Terraform documentation for provider
   configuration] and provide the keys there:

   ```go
   const (
     keyBaseURL = "base_url"
     keyOwner = "owner"
     keyToken = "token"
   )

   func TerraformSetupBuilder(version, providerSource, providerVersion string) terraform.SetupFn {
     ...
     // set provider configuration
     ps.Configuration = map[string]any{}
     if v, ok := creds[keyBaseURL]; ok {
       ps.Configuration[keyBaseURL] = v
     }
     if v, ok := creds[keyOwner]; ok {
       ps.Configuration[keyOwner] = v
     }
     if v, ok := creds[keyToken]; ok {
       ps.Configuration[keyToken] = v
     }
     return ps, nil
   }
   ```

6. Before generating all resources that the provider has, let's go step by step
   and only start with generating CRDs for [github_repository] and
   [github_branch] Terraform resources.

   Only the resources with external name configuration should be generated.
   Let's add external name configurations for these two resources in 
   `config/external_name.go` as an entry to the map called `ExternalNameConfigs`:

   ```go
   // ExternalNameConfigs contains all external name configurations for this
   // provider.
   var ExternalNameConfigs = map[string]config.ExternalName{
     // Name is a parameter and it is also used to import the resource.
     "github_repository": config.NameAsIdentifier,
     // The import ID consists of several parameters. We'll use branch name as
     // the external name.
     "github_branch": config.TemplatedStringAsIdentifier("branch", "{{ .parameters.repository }}:{{ .external_name }}:{{ .parameters.source_branch }}"),
   }
   ```

7. Finally, we would need to add some custom configurations for these two
   resources as follows:

   ```bash
   # Create custom configuration directory for whole repository group
   mkdir config/repository
   # Create custom configuration directory for whole branch group
   mkdir config/branch
   ```

   ```bash
   cat <<EOF > config/repository/config.go
   package repository

   import "github.com/upbound/upjet/pkg/config"

   // Configure configures individual resources by adding custom ResourceConfigurators.
   func Configure(p *config.Provider) {
       p.AddResourceConfigurator("github_repository", func(r *config.Resource) {
           // We need to override the default group that upjet generated for
           // this resource, which would be "github"
           r.ShortGroup = "repository"
       })
   }
   EOF
   ```

   ```bash
   # Note that you need to change `myorg/provider-github`.
   cat <<EOF > config/branch/config.go
   package branch

   import "github.com/upbound/upjet/pkg/config"

   func Configure(p *config.Provider) {
       p.AddResourceConfigurator("github_branch", func(r *config.Resource) {
           // We need to override the default group that upjet generated for
           // this resource, which would be "github"
           r.ShortGroup = "branch"

           // This resource need the repository in which branch would be created
           // as an input. And by defining it as a reference to Repository
           // object, we can build cross resource referencing. See
           // repositoryRef in the example in the Testing section below.
           r.References["repository"] = config.Reference{
               Type: "github.com/myorg/provider-github/apis/repository/v1alpha1.Repository",
           }
       })
   }
   EOF
   ```

   And register custom configurations in `config/provider.go`:

   ```diff
   import (
       ...

       tjconfig "github.com/upbound/upjet/pkg/config"
       "github.com/upbound/upjet/pkg/types/conversion/cli"
       "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

   +   "github.com/myorg/provider-github/config/branch"
   +   "github.com/myorg/provider-github/config/repository"
    )

    func GetProvider() *tjconfig.Provider {
       ...
       for _, configure := range []func(provider *tjconfig.Provider){
               // add custom config functions
   -           null.Configure,
   +           repository.Configure,
   +           branch.Configure,
       } {
               configure(pc)
       }
   ```

   **_To learn more about custom resource configurations (in step 7), please see
   the [Configuring a Resource](/docs/add-new-resource-long.md) document._**


8. Now we can generate our Upjet Provider:

   ```bash
   make generate
   ```

### Adding More Resources

See the guide [here][new-resource-short] to add more resources.

## Test

Now let's test our generated resources.

1. First, we will create example resources under the `examples` directory:

   Create example directories for repository and branch groups:

   ```bash
   mkdir examples/repository
   mkdir examples/branch

   # remove the sample directory which was an example in the template
   rm -rf examples/null
   ```

   Create a provider secret template:

   ```bash
   cat <<EOF > examples/providerconfig/secret.yaml.tmpl
   apiVersion: v1
   kind: Secret
   metadata:
     name: example-creds
     namespace: crossplane-system
   type: Opaque
   stringData:
     credentials: |
       {
         "token": "y0ur-t0k3n"
       }
   EOF
   ```

   Create example for `repository` resource, which will use
   `upjet-provider-template` repo as template for the repository
   to be created:

   ```bash
   cat <<EOF > examples/repository/repository.yaml
   apiVersion: repository.github.upbound.io/v1alpha1
   kind: Repository
   metadata:
     name: hello-crossplane
   spec:
     forProvider:
       description: "Managed with Crossplane Github Provider (generated with Upjet)"
       visibility: public
       template:
         - owner: upbound
           repository: upjet-provider-template
     providerConfigRef:
       name: default
   EOF
   ```

   Create `branch` resource which refers to the above repository
   managed resource:

   ```bash
   cat <<EOF > examples/branch/branch.yaml
   apiVersion: branch.github.upbound.io/v1alpha1
   kind: Branch
   metadata:
     name: hello-upjet
   spec:
     forProvider:
       repositoryRef:
         name: hello-crossplane
     providerConfigRef:
       name: default
   EOF
   ```

   In order to change the `apiVersion`, you can use `WithRootGroup` and `WithShortName`
   options in `config/provider.go` as arguments to `ujconfig.NewProvider`.

2. Generate a [Personal Access Token](https://github.com/settings/tokens) for
   your Github account with `repo/public_repo` and `delete_repo` scopes.

3. Create `examples/providerconfig/secret.yaml` from
   `examples/providerconfig/secret.yaml.tmpl` and set your token in the file:

   ```bash
   GITHUB_TOKEN=<your-token-here>
   cat examples/providerconfig/secret.yaml.tmpl | sed -e "s/y0ur-t0k3n/${GITHUB_TOKEN}/g" > examples/providerconfig/secret.yaml
   ```

4. Apply CRDs:

   ```bash
   kubectl apply -f package/crds
   ```

5. Run the provider:

   ```bash
   make run
   ```

6. Apply ProviderConfig and example manifests (_In another terminal since
   the previous command is blocking_):

   ```bash
   # Create "crossplane-system" namespace if not exists
   kubectl create namespace crossplane-system --dry-run=client -o yaml | kubectl apply -f -

   kubectl apply -f examples/providerconfig/
   kubectl apply -f examples/repository/repository.yaml
   kubectl apply -f examples/branch/branch.yaml
   ```

7. Observe managed resources and wait until they are ready:

   ```bash
   watch kubectl get managed
   ```

   ```bash
   NAME                                                   READY   SYNCED   EXTERNAL-NAME                     AGE
   branch.branch.github.jet.crossplane.io/hello-upjet   True    True     hello-crossplane:hello-upjet   89s

   NAME                                                             READY   SYNCED   EXTERNAL-NAME      AGE
   repository.repository.github.jet.crossplane.io/hello-crossplane   True    True     hello-crossplane   89s
   ```

   Verify that repo `hello-crossplane` and branch `hello-upjet` created under
   your GitHub account.

8. You can check the errors and events by calling `kubectl describe` for either
   of the resources.

9. Cleanup

   ```bash
   kubectl delete -f examples/branch/branch.yaml
   kubectl delete -f examples/repository/repository.yaml
   ```

   Verify that the repo got deleted once deletion is completed on the control
   plane.


[Terraform GitHub provider]: https://registry.terraform.io/providers/integrations/github/latest/docs
[upjet-provider-template]: https://github.com/upbound/upjet-provider-template
[upbound/build]: https://github.com/upbound/build
[Terraform documentation for provider configuration]: https://registry.terraform.io/providers/integrations/github/latest/docs#argument-reference
[github_repository]: https://registry.terraform.io/providers/integrations/github/latest/docs/resources/repository
[github_branch]: https://registry.terraform.io/providers/integrations/github/latest/docs/resources/branch
[this line in controller Dockerfile]: https://github.com/upbound/upjet-provider-template/blob/main/cluster/images/official-provider-template-controller/Dockerfile#L18-L26
[terraform-plugin-sdk]: https://github.com/hashicorp/terraform-plugin-sdk
[new-resource-short]: add-new-resource-short.md