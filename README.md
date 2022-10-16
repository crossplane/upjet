# Upjet - Generate Crossplane Providers from any Terraform Provider
<div align="center">

![CI](https://github.com/upbound/upjet/workflows/CI/badge.svg) [![GitHub release](https://img.shields.io/github/release/upbound/upjet/all.svg?style=flat-square)](https://github.com/upbound/upjet/releases) [![Go Report Card](https://goreportcard.com/badge/github.com/upbound/upjet)](https://goreportcard.com/report/github.com/upbound/upjet) [![Slack](https://slack.crossplane.io/badge.svg)](https://crossplane.slack.com/archives/C01TRKD4623) [![Twitter Follow](https://img.shields.io/twitter/follow/upbound_io.svg?style=social&label=Follow)](https://twitter.com/intent/follow?screen_name=upbound_io&user_id=788180534543339520)

</div>

Upjet is a code generator framework that allows developers to build code
generation pipelines that can generate Crossplane controllers. Developers can
start building their code generation pipeline targeting specific Terraform Providers
by importing Upjet and wiring all generators together, customizing the whole
pipeline in the process.

Here is some Crossplane providers built using Upjet:

* [Provider AWS](https://github.com/upbound/provider-aws)
* [Provider Azure](https://github.com/upbound/provider-azure)
* [Provider GCP](https://github.com/upbound/provider-gcp)

## Getting Started

You can get started by following the guides in [docs](docs/README.md) directory!

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

## Prior Art

Upjet originates from the [Terrajet][terrajet] project. See the original 
[design document][terrajet-design-doc].

## Licensing

Provider AWS is under [the Apache 2.0 license](LICENSE) with [notice](NOTICE).

[terrajet-design-doc]: https://github.com/crossplane/crossplane/blob/master/design/design-doc-terrajet.md
[terrajet]: https://github.com/crossplane/terrajet