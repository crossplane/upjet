# Generating a Crossplane Provider

In this guide, we will generate an official provider based on an existing
Terraform provider using Upjet.

We have chosen [Terraform GitHub provider] as an example, but the process will
be quite similar for any other Terraform provider.

## Creating a Provider

### Initial Setup

Official providers are kept in a mono repo
[`upbound/official-providers`][official-providers]. So, before starting any
work, you'll need to clone that repo to your local filesystem.

```console
# You can use "upstream" as remote target name and save "origin" for your
# fork, which you'll need to have to open pull requests.
git clone --name upstream git@github.com:upbound/official-providers.git
```

We have the clone of the upstream but we'll need to fork the upstream into our
own account in Github so that we push to that fork and create pull requests
against it. Go ahead and fork using the Github UI and then add your fork as the
`origin` with the following command:
```
USERNAME=<your github username>
git clone --name origin git@github.com:${USERNAME}/official-providers.git
```

Now, we're ready to create a new branch. Once we're done with our changes, we'll
push this branch to our own fork and then open a pull request against the
upstream repo.
```
# Create a new branch to add provider-github
git checkout -b provider-github
```

### Generation

1. Run the following command to bootstrap your new provider directory with the
   scaffolding that's kept in [`upbound/provider-template`][provider-template]:

   ```console
   # This needs to be run in "upbound/official-providers" directory.
   make provider name=provider-github
   ```

2. We will use our small script for rename and find&replace operations.

    1. Go to the directory of the provider you created:
        ```
        cd provider-github
        ```
    2. Export `ProviderName`:

        ```bash
        export ProviderNameLower=github
        export ProviderNameUpper=GitHub
        ```

    3. Run the `./hack/prepare.sh` script from repo root to prepare the repo,
       e.g., to replace all occurrences of `template` with your provider name:

        ```bash
       ./hack/prepare.sh
        ```

3. To configure the Terraform provider to generate from, update the following
   variables in `Makefile`:

    ```makefile
    export TERRAFORM_PROVIDER_SOURCE := integrations/github
    export TERRAFORM_PROVIDER_VERSION := 4.19.2
    export TERRAFORM_PROVIDER_DOWNLOAD_NAME := terraform-provider-github
    export TERRAFORM_PROVIDER_DOWNLOAD_URL_PREFIX := https://releases.hashicorp.com/terraform-provider-github/4.19.2
    ```

   You can find `TERRAFORM_PROVIDER_SOURCE` and `TERRAFORM_PROVIDER_VERSION` in
   [Terraform GitHub provider] documentation by hitting the "**USE PROVIDER**"
   button. Check [this line in controller Dockerfile] to see how these variables
   are used to build the provider plugin binary.


4. Implement `ProviderConfig` logic. In `provider-jet-template`, there is
   already a boilerplate code in file `internal/clients/${ProviderNameLower}.go`
   which takes care of properly fetching secret data referenced from
   `ProviderConfig` resource.

   For our GitHub provider, we need to check [Terraform documentation for
   provider configuration] and provide the keys there:

   ```go
   const (
     keyBaseURL = "base_url"
     keyOwner = "owner"
     keyToken = "token"

     // GitHub credentials environment variable names
     envToken = "GITHUB_TOKEN"
   )

   func TerraformSetupBuilder(version, providerSource, providerVersion string) terraform.SetupFn {
     ...
     // set provider configuration
     ps.Configuration = map[string]interface{}{}
     if v, ok := githubCreds[keyBaseURL]; ok {
         ps.Configuration[keyBaseURL] = v
     }
     if v, ok := githubCreds[keyOwner]; ok {
         ps.Configuration[keyOwner] = v
     }
     // set environment variables for sensitive provider configuration
     ps.Env = []string{
         fmt.Sprintf(fmtEnvVar, envToken, githubCreds[keyToken]),
     }
     return ps, nil
   }
   ```

5. Before generating all resources that the provider has, let's go step by step
   and only start with generating CRDs for [github_repository] and
   [github_branch] Terraform resources.

   To limit the resources to be generated, we need to provide an include list
   option with `tjconfig.WithIncludeList` in file `config/provider.go`:

   ```go
   pc := tjconfig.NewProviderWithSchema([]byte(providerSchema), resourcePrefix, modulePath,
       tjconfig.WithDefaultResourceFn(defaultResourceFn),
       tjconfig.WithIncludeList([]string{
           "github_repository$",
           "github_branch$",
       }))
   ```

6. Finally, we would need to add some custom configurations for these two
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
   
           // we need to override the default group that upjet generated for
           // this resource, which would be "github"  
           r.ShortGroup = "repository"
       })
   }
   EOF
   ```

   ```bash
   cat <<EOF > config/branch/config.go
   package branch

   import "github.com/upbound/upjet/pkg/config"

   func Configure(p *config.Provider) {
       p.AddResourceConfigurator("github_branch", func(r *config.Resource) {
   
           // we need to override the default group that upjet generated for
           // this resource, which would be "github" 
           r.ShortGroup = "branch"
   
           // Identifier for this resource is assigned by the provider. In other
           // words it is not simply the name of the resource.
           r.ExternalName = config.IdentifierFromProvider
   
           // This resource need the repository in which branch would be created
           // as an input. And by defining it as a reference to Repository
           // object, we can build cross resource referencing. See 
           // repositoryRef in the example in the Testing section below.
           r.References["repository"] = config.Reference{
               Type: "github.com/crossplane-contrib/provider-jet-github/apis/repository/v1alpha1.Repository",
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

   +   "github.com/crossplane-contrib/provider-jet-github/config/branch"
   +   "github.com/crossplane-contrib/provider-jet-github/config/repository"
    )

    func GetProvider() *tjconfig.Provider {
       ...
       for _, configure := range []func(provider *tjconfig.Provider){
               // add custom config functions
   +           repository.Configure,
   +           branch.Configure,
       } {
               configure(pc)
       }
   ```

   **_To learn more about custom resource configurations (in step 7), please see
   the [Configuring a Resource](/docs/configuring-a-resource.md) document._**


7. Now we can generate our Upjet Provider:

   ```bash
   make generate
   ```

## Test

Now let's test our generated resources.

1. First, we will create example resources under the `examples` directory:

   Create example directories for repository and branch groups:

   ```bash
   mkdir examples/repository
   mkdir examples/branch
   
   # remove the sample directory which was an example in the template
   rm -rf examples/sample
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
   `provider-jet-template` repo as template for the repository to be created:

   ```bash
   cat <<EOF > examples/repository/repository.yaml
   apiVersion: repository.github.jet.crossplane.io/v1alpha1
   kind: Repository
   metadata:
     name: hello-crossplane
   spec:
     forProvider:
       description: "Managed with Crossplane Github Provider (generated with Upjet)"
       visibility: public
       template:
         - owner: crossplane-contrib
           repository: provider-jet-template
     providerConfigRef:
       name: default
   EOF
   ```

   Create `branch` resource which refers to the above repository managed
   resource:

   ```bash
   cat <<EOF > examples/branch/branch.yaml
   apiVersion: branch.github.jet.crossplane.io/v1alpha1
   kind: Branch
   metadata:
     name: hello-upjet
   spec:
     forProvider:
       branch: hello-upjet
       repositoryRef:
         name: hello-crossplane
     providerConfigRef:
       name: default
   EOF
   ```

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


9. Cleanup

   ```bash
   kubectl delete -f examples/branch/branch.yaml
   kubectl delete -f examples/repository/repository.yaml
   ```

   Verify that the repo got deleted once deletion is completed on the control
   plane.

## Adding New Resources

In the earlier steps, we generated only two resources and added all
configurations for them to test the whole flow is working. Now, we will expand
the list of generated resources by adding a select set of configurations and
mark them as `v1beta1`. We will need to go through the following steps for every
resource we want to add.

For `v1beta1` level resources, we will do only the external name configuration
and make sure no field is represented as a separate resource. Then we'll test
the example YAML we have created to make sure all `v1beta1` scenarios listed
[here][v1beta1-criteria] are working.

1. Initially, let's create a file exclusive for external name generation named
   `config/external_name.go` with the following content
   ```
   # Assuming you are in official-providers/provider-github repo already.
   touch config/external_name.go
   ```

1. Enable the resource by adding its Terraform name to the include list. You can
   use a global list for this task. Once all resources are enabled you can
   remove `tjconfig.WithIncludeList` function completely so that all resources
   are included.

   ```go

   pc := tjconfig.NewProviderWithSchema([]byte(providerSchema), resourcePrefix, modulePath,
       tjconfig.WithDefaultResourceFn(defaultResourceFn),
       tjconfig.WithIncludeList([]string{
           "github_repository$",
           "github_branch$",
           "github_new_resource$",
       }))
   ```

* case1: there is a parameter called `name` and can be used to name the resource and to import the resource directly without a change: `config.NameAsIdentifier`
* case2: there is a parameter that can be used to name the resource and to import the resource directly without a change: `config.ParameterAsIdentifier(<field name of the parameter, like cluster_name>)`
* case3: Import requires an ID that's completely random, like VPC id `vpc-3211`: `config.IdentifierFromProvider`
* case4: There is no import statement at all: `config.IdentifierFromProvider`
* case5: The ID in import can be constructed with parameters:
* case6: Some parts of the ID can be constructed with parameters, the rest is random:
* case7: Some parts of the ID can be constructed with parameters, the rest is info you can't get directly, like account ID:
* case8: a parameter is used directly as ID but it is name of another resource or a configuration rather than name and it's immutable, i.e. forces new: `config.IdentifierFromProvider`, `aws_ecr_pull_through_cache_rule`

[comment]: <> (References)

[Terraform GitHub provider]:
    https://registry.terraform.io/providers/integrations/github/latest/docs
[provider-jet-template]:
    https://github.com/crossplane-contrib/provider-jet-template
[upbound/build]: https://github.com/upbound/build
[Terraform documentation for provider configuration]:
    https://registry.terraform.io/providers/integrations/github/latest/docs#argument-reference
[github_repository]:
    https://registry.terraform.io/providers/integrations/github/latest/docs/resources/repository
[github_branch]:
    https://registry.terraform.io/providers/integrations/github/latest/docs/resources/branch
[this line in controller Dockerfile]:
    https://github.com/crossplane-contrib/provider-jet-template/blob/d9a793dd8a304f09bb2e9694c47c1bade1b6b057/cluster/images/provider-jet-template-controller/Dockerfile#L18-L25
[terraform-plugin-sdk]: https://github.com/hashicorp/terraform-plugin-sdk
[official-providers]: https://github.com/upbound/official-providers
[provider-template]: https://github.com/upbound/provider-template
[v1beta1-criteria]: https://github.com/upbound/arch/pull/33