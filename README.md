# Upjet - Generate Crossplane Providers from any Terraform Provider

Upjet is a code generator framework that allows developers to build code
generation pipelines that can generate Crossplane controllers. Developers can
start building their code generation pipeline targeting specific Terraform Providers
by importing Upjet and wiring all generators together, customizing the whole
pipeline in the process.

See [design document][design-doc] for more details.

Feel free to test the following Crossplane providers built using Upjet:

* [Provider AWS](https://github.com/upbound/provider-aws/releases)
* [Provider Azure](https://github.com/upbound/provider-azure/releases)
* [Provider GCP](https://github.com/upbound/provider-gcp/releases)

**NOTE**: Upjet is in its very early stages and we're making many changes that
can affect the output and the runtime. Please check the generated code before
running in production.

## Generating a New Provider Using Upjet

Please see [this guide](docs/generating-a-provider.md) for detailed steps on how
to generate a Crossplane provider based on an existing Terraform provider.

## Report a Bug

For filing bugs, suggesting improvements, or requesting new features, please
open an [issue](https://github.com/upbound/upjet/issues).

## Contact

Please use the following to reach members of the community:

* Slack: Join our [slack channel](https://slack.crossplane.io)
* Forums:
  [crossplane-dev](https://groups.google.com/forum/#!forum/crossplane-dev)
* Twitter: [@crossplane_io](https://twitter.com/crossplane_io)
* Email: [info@crossplane.io](mailto:info@crossplane.io)

## Governance and Owners

upjet is governed solely by Upbound Inc.

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

upjet adheres to the [Code of
Conduct](https://github.com/crossplane/crossplane/blob/master/CODE_OF_CONDUCT.md)
as the core Crossplane project.

## Licensing

All rights of upjet belongs to Upbound Inc.

[design-doc]: https://github.com/crossplane/crossplane/blob/master/design/design-doc-terrajet.md
[provider-template]: https://github.com/crossplane/provider-template