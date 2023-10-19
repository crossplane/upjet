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
"**Use this template**" button in the [provider-template] repository. The
expected repository name is in the format `provider-<name>`. For example,
`provider-github`. The script in step 3 expects this format and fails if you
follow a different naming convention.
1. Clone the repository to your local environment and `cd` into the repository
directory.
1. Fetch the [upbound/build] submodule by running the following
command:

    ```bash
    make submodules
    ```

1. To setup your provider name and group run the `./hack/helpers/prepare.sh`
script from the repository root to prepare the code.

    ```bash
    PROVIER=github ./hack/helpers/prepare.sh
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
  export TERRAFORM_PROVIDER_VERSION := 5.32.0
  export TERRAFORM_PROVIDER_DOWNLOAD_NAME := terraform-provider-github
  export TERRAFORM_NATIVE_PROVIDER_BINARY := terraform-provider-github_v5.32.0
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

    - Create custom configuration directory for whole repository group

    ```bash
    mkdir config/repository    
    ```

    - Create custom configuration directory for whole branch group

    ```bash
    mkdir config/branch
    ```

    - Create the repository group configuration file

    ```bash
    cat <<EOF > config/repository/config.go
    package repository

    import "github.com/crossplane/upjet/pkg/config"

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
    cat <<EOF > config/branch/config.go
    package branch

    import "github.com/crossplane/upjet/pkg/config"

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

        ujconfig "github.com/upbound/crossplane/pkg/config"

    -   "github.com/myorg/provider-github/config/null"
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
   `upjet-provider-template` repo as template for the repository to be created:

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

   Create `branch` resource which refers to the above repository managed
   resource:

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

   In order to change the `apiVersion`, you can use `WithRootGroup` and
   `WithShortName` options in `config/provider.go` as arguments to
   `ujconfig.NewProvider`.

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

## Next steps

Now that you've seen the basics of generating `CustomResourceDefinitions` for
your provider, you can learn more about
[configuring resources](configuring-a-resource.md) or
[testing your resources](testing-with-uptest.md) with Uptest.

[Terraform GitHub provider]: https://registry.terraform.io/providers/integrations/github/latest/docs
[upjet-provider-template]: https://github.com/crossplane/upjet-provider-template
[upbound/build]: https://github.com/upbound/build
[github_repository]: https://registry.terraform.io/providers/integrations/github/latest/docs/resources/repository
[github_branch]: https://registry.terraform.io/providers/integrations/github/latest/docs/resources/branch
