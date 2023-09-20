<!--
SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->

# Upjet - Generate Crossplane Providers from any Terraform Provider

<div align="center">

![CI](https://github.com/crossplane/upjet/workflows/CI/badge.svg)
[![GitHub release](https://img.shields.io/github/release/crossplane/upjet/all.svg)](https://github.com/crossplane/upjet/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/crossplane/upjet)](https://goreportcard.com/report/github.com/crossplane/upjet)
[![Contributors](https://img.shields.io/github/contributors/crossplane/upjet)](https://github.com/crossplane/upjet/graphs/contributors)
[![Slack](https://img.shields.io/badge/Slack-4A154B?logo=slack)](https://crossplane.slack.com/archives/C05T19TB729)
[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/crossplane_io)](https://twitter.com/crossplane_io)

</div>

Upjet is a code generator framework that allows developers to build code
generation pipelines that can generate Crossplane controllers. Developers can
start building their code generation pipeline targeting specific Terraform
Providers by importing Upjet and wiring all generators together, customizing the
whole pipeline in the process.

Here are some Crossplane providers built using Upjet:

- [upbound/provider-aws](https://github.com/upbound/provider-aws)
- [upbound/provider-azure](https://github.com/upbound/provider-azure)
- [upbound/provider-gcp](https://github.com/upbound/provider-gcp)
- [aviatrix/crossplane-provider-aviatrix](https://github.com/Aviatrix/crossplane-provider-aviatrix)

## Getting Started

You can get started by following the guides in the [docs](docs/README.md)
directory.

## Report a Bug

For filing bugs, suggesting improvements, or requesting new features, please
open an [issue](https://github.com/crossplane/upjet/issues).

## Contact

[#upjet](https://crossplane.slack.com/archives/C05T19TB729) channel in
[Crossplane Slack](https://slack.crossplane.io)

## Prior Art

Upjet originates from the [Terrajet][terrajet] project. See the original
[design document][terrajet-design-doc].

## Licensing

Upjet is under [the Apache 2.0 license](LICENSE) with [notice](NOTICE).

[terrajet-design-doc]: https://github.com/crossplane/crossplane/blob/master/design/design-doc-terrajet.md
[terrajet]: https://github.com/crossplane/terrajet
