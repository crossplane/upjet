<!--
SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->
# Generating a Crossplane provider

This guide shows you how to generate a Crossplane provider based on an existing
Terraform provider using Upjet. The guide uses the [Terraform GitHub provider]
as the example, but the process is similar for any other Terraform provider.

## Prepare your new provider repository

1. Create a new GitHub repository for the Crossplane provider by clicking the
"**Use this template**" button in the [upjet-provider-template] repository. The
expected repository name is in the format `provider-<name>`. For example,
`provider-github`. The script in step 3 expects this format and fails if you
follow a different naming convention.
1. Clone the repository to your local environment and `cd` into the repository
directory.
1. Fetch the [crossplane/build] submodule by running the following
command:

    ```bash
    make submodules
    ```

1. To setup your provider name and group run the `./hack/prepare.sh`
script from the repository root to prepare the code.

    ```bash
    ./hack/prepare.sh
    ```

1. Ensure your organization name is correct in the `Makefile` for the
  `PROJECT_REPO` variable.
1. To configure which Terraform provider to generate from, update the following
variables in the `Makefile`:

  | Variable | Description |
  | -------- | ----------- |
  | `TERRAFORM_PROVIDER_SOURCE` | Find this variable on the Terraform registry for the provider. You can see the source value when clicking on the "`USE PROVIDER`" dropdown button in the navigation. |
  |`TERRAFORM_PROVIDER_REPO` | The URL to the repository that hosts the provider's code. |
  | `TERRAFORM_PROVIDER_VERSION` | Find this variable on the Terraform registry for the provider. You can see the source value when clicking on the "`USE PROVIDER`" dropdown button in the navigation. |
  |`TERRAFORM_PROVIDER_DOWNLOAD_NAME` | The name of the provider in the [Terraform registry](https://releases.hashicorp.com/) |
  |`TERRAFORM_NATIVE_PROVIDER_BINARY` | The name of the binary in the Terraform provider. This follows the pattern `terraform-provider-{provider name}_v{provider version}`. |
  |`TERRAFORM_DOCS_PATH` | The relative path, from the root of the repository, where the provider resource documentation exist. |
  
  For example, for the [Terraform GitHub provider], the variables are:

  ```makefile
  export TERRAFORM_PROVIDER_SOURCE := integrations/github
  export TERRAFORM_PROVIDER_REPO := https://github.com/integrations/terraform-provider-github
  export TERRAFORM_PROVIDER_VERSION := 6.6.0
  export TERRAFORM_PROVIDER_DOWNLOAD_NAME := terraform-provider-github
  export TERRAFORM_NATIVE_PROVIDER_BINARY := terraform-provider-github_v6.6.0
  export TERRAFORM_DOCS_PATH := website/docs/r
  ```

  Refer to [the Dockerfile](https://github.com/crossplane/upjet-provider-template/blob/main/cluster/images/upjet-provider-template/Dockerfile) to see the variables called when building the provider.

## Configure the provider resources

1. First you need to add the `ProviderConfig` logic.
    - In `upjet-provider-template`, there is
    already boilerplate code in the file `internal/clients/github.go` which takes
    care of fetching secret data referenced from the `ProviderConfig` resource.
    - Reference the [Terraform Github provider] documentation for information on
    authentication and provide the necessary keys.:

    ```go
    const (
      ...
      keyBaseURL = "base_url"
      keyOwner = "owner"
      keyToken = "token"
    )
    ```

    ```go
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

1. Next add external name configurations for the [github_repository] and
    [github_branch] Terraform resources.

    > [!NOTE]
    > Only generate resources with an external name configuration defined.

    - Add external name configurations for these two resources in
    `config/external_name.go` as an entry to the map called
    `ExternalNameConfigs`

    ```go
    // ExternalNameConfigs contains all external name configurations for this
    // provider.
    var ExternalNameConfigs = map[string]config.ExternalName{
      ...
      // Name is a parameter and it is also used to import the resource.
      "github_repository": config.NameAsIdentifier,
      // The import ID consists of several parameters. We'll use branch name as
      // the external name.
      "github_branch": config.TemplatedStringAsIdentifier("branch", "{{ .parameters.repository }}:{{ .external_name }}:{{ .parameters.source_branch }}"),
    }
    ```

    - Take a look at the documentation for configuring a resource for more
    information about [external name configuration](configuring-a-resource.md#external-name).

1. Next add custom configurations for these two resources as follows:

    - Create custom configuration directories for whole repository group, both for cluster-scoped and namespaced variant

    ```bash
    mkdir config/cluster/repository
    mkdir config/namespaced/repository
    ```

    - Create custom configuration directory for whole branch group, both for cluster-scoped and namespaced variant

    ```bash
    mkdir config/cluster/branch
    mkdir config/namespaced/branch
    ```

    - Create the repository group configuration file for cluster-scoped resource

    ```bash
    cat <<EOF > config/cluster/repository/config.go
    package repository

    import "github.com/crossplane/upjet/v2/pkg/config"

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

    - Create the repository group configuration file for namespace-scoped resource

    ```bash
    cat <<EOF > config/namespaced/repository/config.go
    package repository

    import "github.com/crossplane/upjet/v2/pkg/config"

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

    - Create the branch group configuration file

    > [!NOTE]
    > Note that you need to change `myorg/provider-github` to your organization.

    ```bash
    cat <<EOF > config/cluster/branch/config.go
    package branch

    import "github.com/crossplane/upjet/v2/pkg/config"

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
                Type: "github.com/myorg/provider-github/apis/cluster/repository/v1alpha1.Repository",
            }
        })
    }
    EOF
    ```

    - Now add the same configuration for namespace-scoped provider configuration

    ```bash
    cat <<EOF > config/namespaced/branch/config.go
    package branch

    import "github.com/crossplane/upjet/v2/pkg/config"

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
                Type: "github.com/myorg/provider-github/apis/namespaced/repository/v1alpha1.Repository",
            }
        })
    }
    EOF
    ```

    And register custom configurations in `config/provider.go`, both for cluster-scoped and namespace-scoped provider configuration:

    ```diff
    import (
        ...

        ujconfig "github.com/crossplane/upjet/pkg/config"

    -   nullCluster "github.com/myorg/provider-github/config/cluster/null"
    -   nullNamespaced "github.com/myorg/provider-github/config/namespaced/null"
    +   branchCluster "github.com/myorg/provider-github/config/cluster/branch"
    +   repositoryCluster "github.com/myorg/provider-github/config/cluster/repository"
    +   branchNamespaced "github.com/myorg/provider-github/config/namespaced/branch"
    +   repositoryNamespaced "github.com/myorg/provider-github/config/namespaced/repository"
     )

     func GetProvider() *ujconfig.Provider {
        ...
        for _, configure := range []func(provider *ujconfig.Provider){
                // add custom config functions
    -           null.Configure,
    +           repositoryCluster.Configure,
    +           branchCluster.Configure,
        } {
                configure(pc)
        }
     }
     func GetProviderNamespaced() *ujconfig.Provider {
        ...
        for _, configure := range []func(provider *ujconfig.Provider){
                // add custom config functions
    -           nullNamespaced.Configure,
    +           repositoryNamespaced.Configure,
    +           branchNamespaced.Configure,
        } {
                configure(pc)
        }
     }
    ```

    _To learn more about custom resource configurations (in step 7), please
    see the [Configuring a Resource](configuring-a-resource.md) document._

1. Now we can generate our Upjet Provider:

    Before we run `make generate` ensure to install `goimports`

    ```bash
    go install golang.org/x/tools/cmd/goimports@latest
    ```

    ```bash
    make generate
    ```

## Testing the generated resources

Now let's test our generated resources.

1. First, we will create example resources under the `examples` directory:

    Create example directories for repository and branch groups:

    ```bash
    mkdir examples/cluster/repository
    mkdir examples/cluster/branch
    mkdir examples/namespaced/repository
    mkdir examples/namespaced/branch
  
    # remove the sample directory which was an example in the template
    rm -rf examples/cluster/null
    rm -rf examples/namespaced/null
    ```
  
    Create a provider secret template:
  
    ```bash
    cat <<EOF > examples/cluster/providerconfig/secret.yaml.tmpl
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

    # namespaced
    cat <<EOF > examples/namespaced/providerconfig/secret.yaml.tmpl
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
    `upjet-provider-template` repo as template for the repository to be created:
  
    ```bash
    # cluster-scoped
    cat <<EOF > examples/cluster/repository/repository.yaml
    apiVersion: repository.github.crossplane.io/v1alpha1
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
  
    # namespace-scoped
    cat <<EOF > examples/namespaced/repository/repository.yaml
    apiVersion: repository.github.m.crossplane.io/v1alpha1
    kind: Repository
    metadata:
      name: hello-crossplane-v2
    spec:
      forProvider:
        description: "Managed with Crossplane Github Provider (generated with Upjet)"
        visibility: public
        template:
          - owner: upbound
            repository: upjet-provider-template
      providerConfigRef:
        kind: ClusterProviderConfig
        name: default
    EOF
    ```

    Create `branch` resource which refers to the above repository managed
    resource:
  
    ```bash
    # cluster-scoped
    cat <<EOF > examples/cluster/branch/branch.yaml
    apiVersion: branch.github.crossplane.io/v1alpha1
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

    # namespace-scoped
    cat <<EOF > examples/namespaced/branch/branch.yaml
    apiVersion: branch.github.m.crossplane.io/v1alpha1
    kind: Branch
    metadata:
      name: hello-upjet-v2
    spec:
      forProvider:
        repositoryRef:
          name: hello-crossplane-v2
      providerConfigRef:
        kind: ClusterProviderConfig
        name: default
    EOF
    ```

    In order to change the `apiVersion`, you can use `WithRootGroup` and
    `WithShortName` options in `config/provider.go` as arguments to
    `ujconfig.NewProvider`.

2. Generate a [Personal Access Token](https://github.com/settings/tokens) for
   your Github account with `repo/public_repo` and `delete_repo` scopes.

3. Create `examples/cluster/providerconfig/secret.yaml` from
  `examples/cluster/providerconfig/secret.yaml.tmpl` and set your token in the file:

    ```bash
    GITHUB_TOKEN=<your-token-here>
    cat examples/cluster/providerconfig/secret.yaml.tmpl | sed -e "s/y0ur-t0k3n/${GITHUB_TOKEN}/g" > examples/cluster/providerconfig/secret.yaml
    # namespaced
    cat examples/namespaced/providerconfig/secret.yaml.tmpl | sed -e "s/y0ur-t0k3n/${GITHUB_TOKEN}/g" > examples/namespaced/providerconfig/secret.yaml
    ```

4. Apply CRDs:

   ```bash
   kubectl apply -f package/crds
   ```

5. Run the provider:

   Please make sure Terraform is installed before running the "make run"
   command, you can check
   [this guide](https://developer.hashicorp.com/terraform/downloads).

   ```bash
   make run
   ```

6. Apply ProviderConfig and example manifests (_In another terminal since the
   previous command is blocking_):

    ```bash
    # Create "crossplane-system" namespace if not exists
    kubectl create namespace crossplane-system --dry-run=client -o yaml | kubectl apply -f -

    kubectl apply -f examples/cluster/providerconfig/
    kubectl apply -f examples/cluster/repository/repository.yaml
    kubectl apply -f examples/cluster/branch/branch.yaml

    kubectl apply -f examples/namespaced/providerconfig/
    kubectl apply -f examples/namespaced/repository/repository.yaml
    kubectl apply -f examples/namespaced/branch/branch.yaml
    ```

7. Observe managed resources and wait until they are ready:

    ```bash
    watch kubectl get managed -A
    ```

    ```bash
    NAME                                             SYNCED   READY   EXTERNAL-NAME   AGE
    branch.branch.github.crossplane.io/hello-upjet   True     True    hello-upjet     3m30s

    NAMESPACE   NAME                                                  SYNCED   READY   EXTERNAL-NAME    AGE
    default     branch.branch.github.m.crossplane.io/hello-upjet-v2   True     True    hello-upjet-v2   100s

    NAMESPACE   NAME                                                          SYNCED   READY   EXTERNAL-NAME      AGE
                repository.repository.github.crossplane.io/hello-crossplane   True     True    hello-crossplane   3m30s

    NAMESPACE   NAME                                                               SYNCED   READY   EXTERNAL-NAME         AGE
    default     repository.repository.github.m.crossplane.io/hello-crossplane-v2   True     True    hello-crossplane-v2   100s
    ```

    Verify that:
    - repo `hello-crossplane` and branch `hello-upjet`
    - repo `hello-crossplane-v2` and branch `hello-upjet-v2`
    created under your GitHub account.

8. You can check the errors and events by calling `kubectl describe` for either of the resources.

9. Cleanup

    ```bash
    kubectl delete -f examples/cluster/branch/branch.yaml
    kubectl delete -f examples/cluster/repository/repository.yaml

    kubectl delete -f examples/namespaced/branch/branch.yaml
    kubectl delete -f examples/namespaced/repository/repository.yaml
    ```

    Verify that the repo got deleted once deletion is completed on the control plane.

## Next steps

Now that you've seen the basics of generating `CustomResourceDefinitions` for
your provider, you can learn more about
[configuring resources](configuring-a-resource.md) or
[testing your resources](testing-with-uptest.md) with Uptest.

[Terraform GitHub provider]: https://registry.terraform.io/providers/integrations/github/latest/docs
[upjet-provider-template]: https://github.com/crossplane/upjet-provider-template
[crossplane/build]: https://github.com/crossplane/build
[github_repository]: https://registry.terraform.io/providers/integrations/github/latest/docs/resources/repository
[github_branch]: https://registry.terraform.io/providers/integrations/github/latest/docs/resources/branch
