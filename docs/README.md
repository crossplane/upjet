# Using Upjet

Upjet consists of three main pieces:
* Framework to build a code generator pipeline.
* Generic reconciler implementation used by all generated `CustomResourceDefinition`s.
* A scraper to extract documentation for all generated `CustomResourceDefinition`s.

The usual flow of development of a new provider is as following:
1. Create a provider by following the guide [here][generate-a-provider].
2. Follow the guide [here][new-v1beta1] to add a `CustomResourceDefinition` for
   every resource in the given Terraform provider.

In most cases, the two guides above would be enough for you to get up and running
with a provider.

The guides below are longer forms for when you get stuck and want a deeper
understanding:
* Description of all configuration knobs can be found [here][full-guide].
* Detailed explanation of how to use Uptest to test your resources can be found
  [here][uptest-guide].
  * You can find a troubleshooting guide [here][testing-instructions] that can
    be useful to debug a failed test.
* References are inferred from the generated examples with a best effort manner.
  Details about the process can be found [here][reference-generation].

Feel free to ask your questions by opening an issue, starting a discussion or
shooting a message on [Slack]!

[generate-a-provider]: generating-a-provider.md
[new-v1beta1]: add-new-resource-short.md
[full-guide]: add-new-resource-long.md
[uptest-guide]: testing-resources-by-using-uptest.md
[testing-instructions]: testing-instructions.md
[reference-generation]: reference-generation.md
[Slack]: https://crossplane.slack.com/archives/C01TRKD4623