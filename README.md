# Terrajet - Generate Crossplane Providers from any Terraform Provider

Terrajet is a code generator framework that allows developers to build code
generation pipelines that can generate Crossplane controllers. Developers can
start building their code generation pipeline targeting specific Terraform Providers
by importing Terrajet and wiring all generators together, customizing the whole
pipeline in the process.

See [design document][design-doc] for more details.

**NOTE**: Terrajet is in its very early stages. We expect many breaking changes
in the coming weeks. Relying on it for production usage is not recommended yet.

## Getting Started

A Crossplane managed resource support consists of two main parts; type definition
of `CustomResourceDefinition` that satisfy `xpresource.Managed` interface and
an implementation of `managed.ExternalClient` acting on the instances of that
`CustomResourceDefinition`.

In Terrajet, we have code generators for generating the Go types of the resource
from imported Terraform provider. Additionally, we have a common controller that
implements `managed.ExternalClient`. Using `pipeline` package, you can generate
the API types as well as the code that stitches it together with the implementation
of `managed.ExternalClient`.

The structure of the generator is just like any other Go program; we'll create
a `cmd/generator/main.go` file and call Terrajet tools from there.

The first step would be to use [`provider-template`][provider-template] to
bootstrap your provide repository.

### Setup Code Generators

Every CRD has to have an API group alongside its kind. Since Terraform doesn't
have the notion of API groups in its schema, first thing we need to do is to
derive it from the name of the resource for all kinds. We need a `map[string]map[string]*schema.Resource`
that holds the resources in groups. Something like the following should allow you
to get that map:
```go
// Example resource name: aws_eks_cluster
groups := map[string]map[string]*schema.Resource{}
for name, resource := range aws.Provider().ResourcesMap {
    words := strings.Split(name, "_")
    groupName := words[1]
    if len(groups[groupName]) == 0 {
        groups[groupName] = map[string]*schema.Resource{}
    }
    groups[groupName][name] = resource
}
```

Now that we have the map, we can run the code generators using the elements in
the map as input:
```go
// In `apis` folder, we need to register schema of every CRD so that that schema
// can be the only one used in `cmd/provider/main.go` for registration with the
// manager. Similarly, every controller should be set up using its `Setup` function.
// We hold two lists for both so that every time a new version or controller call
// is generated, we add them here and in the end run the generators for `apis/zz_generate.go`
// and `internal/controller/zz_setup.go` using these lists.
versionPkgList := []string{
    "github.com/crossplane-contrib/provider-tf-aws/apis/v1alpha1",
}
controllerPkgList := []string{
    "github.com/crossplane-contrib/provider-tf-aws/internal/controller/tfaws",
}


for group, resources := range groups {
	// For now, we want to generate only v1alpha1 versions.
    version := "v1alpha1"
    // This generator will create the `apis/<group name>/v1alpha1` folder together
    // with `zz_groupversion_info.go`.
    versionGen := pipeline.NewVersionGenerator(wd, modulePath, strings.ToLower(group)+groupSuffix, version)
    
    // Since version generator doesn't have any CRD-specific input, we can run
    // it right away.
    if err := versionGen.Generate(); err != nil {
        panic(errors.Wrap(err, "cannot generate version files"))
    }

    // CRDGenerator writes a single Go file that contains the API type and all
    // the types it uses.
    crdGen := pipeline.NewCRDGenerator(versionGen.Package(), versionGen.DirectoryPath(), strings.ToLower(group)+groupSuffix, providerShortName)
    
    // TerraformedGenerator generates the functions that make generated CRDs
    // satisfy resource.Terraformed interface for certain operations that are
    // needed by the common controller.
    tfGen := pipeline.NewTerraformedGenerator(versionGen.Package(), versionGen.DirectoryPath())
    
    // ControllerGenerator writes the `internal/controller/<group>/<kind>/zz_controller.go`
    // that has the `Setup` function of the CRD which constructs a new managed
    // reconciler with given type information and other details coming from
    // main.go
    ctrlGen := pipeline.NewControllerGenerator(wd, modulePath, strings.ToLower(group)+groupSuffix, providerConfigBuilderPath)
    
    // When the types that are used by different CRDs collide, we try to find a
    // unique name for both. But that depends on which type we encounter first.
    // So, the order of CRD generation needs to stay same every time generator
    // is run for stable output.
    keys := make([]string, len(resources))
    i := 0
    for k := range resources {
        keys[i] = k
        i++
    }
    sort.Strings(keys)
    
    // Here we run generators for every CRD kind. Note that since there is a separate
    // controller package for every kind, in contrast to type definitions, we add
    // that package path to the list we defined earlier.
    for _, name := range keys {
        // We don't want Aws prefix in all kinds.
        kind := strings.TrimPrefix(strcase.ToCamel(name), "Aws")
        if err := crdGen.Generate(version, kind, resources[name]); err != nil {
            panic(errors.Wrap(err, "cannot generate crd"))
        }
        if err := tfGen.Generate(version, kind, name, "id"); err != nil {
            panic(errors.Wrap(err, "cannot generate terraformed"))
        }
        ctrlPkgPath, err := ctrlGen.Generate(versionGen.Package().Path(), kind)
        if err != nil {
            panic(errors.Wrap(err, "cannot generate controller"))
        }
        // Here we add the version so that the `zz_setup.go` that is called in
        // `main.go` can include it.
        controllerPkgList = append(controllerPkgList, ctrlPkgPath)
    }
    // Here we add the version so that the `zz_register.go` can include it.
    versionPkgList = append(versionPkgList, versionGen.Package().Path())
}

// Since the list of versions is now complete, we can write the `apis/zz_register.go`
// file.
if err := pipeline.NewRegisterGenerator(wd, modulePath).Generate(versionPkgList); err != nil {
    panic(errors.Wrap(err, "cannot generate register file"))
}

// Since the list of controllers is now complete, we can write the
// `internal/controller/zz_setup.go` file.
if err := pipeline.NewSetupGenerator(wd, modulePath).Generate(controllerPkgList); err != nil {
    panic(errors.Wrap(err, "cannot generate setup file"))
}

// We run goimports on generated files for easier human interaction.
if err := exec.Command("bash", "-c", "goimports -w $(find apis -iname 'zz_*')").Run(); err != nil {
    panic(errors.Wrap(err, "cannot run goimports for apis folder"))
}
if err := exec.Command("bash", "-c", "goimports -w $(find internal -iname 'zz_*')").Run(); err != nil {
    panic(errors.Wrap(err, "cannot run goimports for internal folder"))
}
```

This code excerpt is the main piece that you need to implement to get the code
generators working. After the first run, you'll see many CRDs generated. In order
to run other generic code generators, run the following command:
```
make generate
```

### Provider Config Builder

<to be filled>

### Local Testing

Once you complete the first section, you'll have many CRDs in `package/crds`. You
can install them into your cluster by running `kubectl apply -f package/crds` and
then start `cmd/provider/main.go` to start their controllers.

At this point, the managed resources you create will be reconciled.

### Packaging

> This section might go away once we have a provider-tf-template or a branch
> in provider-template that you can use as template for your Terraform-based
> provider.

Since the common controller shells out to Terraform CLI, which in turn talks to
its provider plugin, we'd like to make those two available in the final controller
image. Change the content of `cluster/images/<provider name>-controller/Dockerfile`
to something similar to the following:
```Dockerfile
FROM BASEIMAGE
RUN apk --no-cache add ca-certificates bash

ARG ARCH
ARG TINI_VERSION
ENV USER_ID=1001

# Setup Terraform environment
ENV TERRAFORM_VERSION 1.0.5
ENV TERRAFORM_PROVIDER_AWS_VERSION 3.56.0
ENV PLUGIN_DIR /terraform/provider-mirror/registry.terraform.io/hashicorp/aws/${TERRAFORM_PROVIDER_AWS_VERSION}/linux_${ARCH}
ENV TF_CLI_CONFIG_FILE /terraform/.terraformrc
ENV TF_FORK 0

RUN mkdir -p ${PLUGIN_DIR}

ADD https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip /tmp
ADD https://releases.hashicorp.com/terraform-provider-aws/${TERRAFORM_PROVIDER_AWS_VERSION}/terraform-provider-aws_${TERRAFORM_PROVIDER_AWS_VERSION}_linux_${ARCH}.zip /tmp
ADD terraformrc.hcl ${TF_CLI_CONFIG_FILE}

RUN unzip /tmp/terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip -d /usr/local/bin \
  && chmod +x /usr/local/bin/terraform \
  && rm /tmp/terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip \
  && unzip /tmp/terraform-provider-aws_${TERRAFORM_PROVIDER_AWS_VERSION}_linux_${ARCH}.zip -d ${PLUGIN_DIR} \
  && chmod +x ${PLUGIN_DIR}/* \
  && rm /tmp/terraform-provider-aws_${TERRAFORM_PROVIDER_AWS_VERSION}_linux_${ARCH}.zip \
  && chown -R ${USER_ID}:${USER_ID} /terraform
# End of - Setup Terraform environment

ADD provider /usr/local/bin/crossplane-tf-aws-provider

USER ${USER_ID}
EXPOSE 8080
ENTRYPOINT ["crossplane-tf-aws-provider"]
```

We want all Terraform workspaces to use the same plugin so we need to add the
following file to `cluster/images/<provider name>-controller/terraformrc.hcl`:
```hcl
provider_installation {
  filesystem_mirror {
    path    = "/terraform/provider-mirror"
    include = ["*/*"]
  }
  direct {
    exclude = ["*/*"]
  }
}
```

And the following is how `cluster/images/<provider name>-controller/Makefile`
should look like:
```Makefile
# ====================================================================================
# Setup Project

PLATFORMS := linux_amd64 linux_arm64
include ../../../build/makelib/common.mk

# ====================================================================================
#  Options
IMAGE = $(BUILD_REGISTRY)/provider-tf-aws-controller-$(ARCH)
include ../../../build/makelib/image.mk

# ====================================================================================
# Targets

img.build:
	@$(INFO) docker build $(IMAGE)
	@cp Dockerfile $(IMAGE_TEMP_DIR) || $(FAIL)
	@cp terraformrc.hcl $(IMAGE_TEMP_DIR) || $(FAIL)
	@cp $(OUTPUT_DIR)/bin/$(OS)_$(ARCH)/provider $(IMAGE_TEMP_DIR) || $(FAIL)
	@cd $(IMAGE_TEMP_DIR) && $(SED_CMD) 's|BASEIMAGE|$(OSBASEIMAGE)|g' Dockerfile || $(FAIL)
	@docker build $(BUILD_ARGS) \
		--build-arg ARCH=$(ARCH) \
		--build-arg TINI_VERSION=$(TINI_VERSION) \
		-t $(IMAGE) \
		$(IMAGE_TEMP_DIR) || $(FAIL)
	@$(OK) docker build $(IMAGE)
```

## Report a Bug

For filing bugs, suggesting improvements, or requesting new features, please
open an [issue](https://github.com/crossplane-contrib/terrajet/issues).

## Contact

Please use the following to reach members of the community:

* Slack: Join our [slack channel](https://slack.crossplane.io)
* Forums:
  [crossplane-dev](https://groups.google.com/forum/#!forum/crossplane-dev)
* Twitter: [@crossplane_io](https://twitter.com/crossplane_io)
* Email: [info@crossplane.io](mailto:info@crossplane.io)

## Governance and Owners

provider-aws is run according to the same
[Governance](https://github.com/crossplane/crossplane/blob/master/GOVERNANCE.md)
and [Ownership](https://github.com/crossplane/crossplane/blob/master/OWNERS.md)
structure as the core Crossplane project.

## Prior Art

There are many projects in infrastructure space that builds on top of Terraform.
Each of the projects have their own limitations, additional features and different
license restrictions.

* [Crossplane: Terraform Provider Runtime](https://github.com/crossplane/crossplane/blob/e2d7278/design/design-doc-terraform-provider-runtime.md)
* [Crossplane: provider-terraform](https://github.com/crossplane-contrib/provider-terraform)
* [Hashicorp Terraform Cloud Operator](https://github.com/hashicorp/terraform-k8s)
* [Rancher Terraform Controller](https://github.com/rancher/terraform-controller)
* [OAM Terraform Controller](https://github.com/oam-dev/terraform-controller)
* [Kubeform](https://github.com/kubeform/kubeform)
* [Terraform Operator](https://github.com/isaaguilar/terraform-operator)

## Code of Conduct

provider-aws adheres to the same [Code of
Conduct](https://github.com/crossplane/crossplane/blob/master/CODE_OF_CONDUCT.md)
as the core Crossplane project.

## Licensing

provider-aws is under the Apache 2.0 license.

[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fcrossplane%2Fprovider-aws.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fcrossplane%2Fprovider-aws?ref=badge_large)

[design-doc]: https://github.com/crossplane/crossplane/blob/master/design/design-doc-terrajet.md
[provider-template]: https://github.com/crossplane/provider-template