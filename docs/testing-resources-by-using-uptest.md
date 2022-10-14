## Testing Resources by Using Uptest

`Uptest` provides a framework to test resources in an end-to-end
pipeline during the resource configuration process. Together with the example
manifest generation tool, it allows us to avoid manual interventions and shortens
testing processes.

These integration tests are costly as they create real resources in cloud providers.
So they are not executed by default. Instead, a comment should be posted to the PR
for triggering tests.

Tests can be run by adding something like the following expressions to the
anywhere in comment:

* `/test-examples="provider-azure/examples/kubernetes/cluster.yaml"`
* `/test-examples="provider-aws/examples/s3/bucket.yaml,
  provider-aws/examples/eks/cluster.yaml"`

You can trigger a test job for an only provider. Provider that the tests will run
is determined by using the first element of the comma separated list. If the
comment contains resources that are from different providers, then these different
resources will be skipped. So, if you want to run tests more than one provider,
you must post separate comments for each provider.

### Debugging Failed Test

After a test failed, it is important to understand what is going wrong. For
debugging the tests, we push some collected logs to GitHub Action artifacts.
These artifacts contain the following data:

* Dump of Kind Cluster
* Kuttl input files (Applied manifests, assertion files)
* Managed resource yaml outputs

To download the artifacts, firstly you must go to the `Summary` page
of the relevant job:

![images/summary.png](images/summary.png)

Then click the `1` under the `Artifacts` button in the upper right. If the
automated tests run for more than one providers, this number will be higher.

When you click this, you can see the `Artifacts` list of job. You can download
the artifact you are interested in by clicking it.

![images/artifacts.png](images/artifacts.png)

When a test fails, the first point to look is the provider container's logs. In
test environment, we run provider by using the `-d` flag to see the debug logs.
In the provider logs, it is possible to see all errors caused by the content of
the resource manifest, caused by the configuration or returned by the cloud
provider.

Also, as you know, yaml output of the managed resources (it is located in the
`managed.yaml` of the artifact archive's root level) are very useful to catch
errors.

If you have any doubts about the generated kuttl files, please check the
`kuttl-inputs.yaml` file in the archive's root.

### Running Uptest locally

For a faster feedback loop, you might want to run `uptest` locally in your
development setup.

To do so run a special `uptest-local` target that accepts `PROVIDER_NAME` and
`EXAMPLE_LIST` arguments as in the example below.

```console
make uptest-local PROVIDER_NAME=provider-azure EXAMPLE_LIST="provider-azure/examples/resource/resourcegroup.yaml"
```

You may also provide all the files in a folder like below:

```console
make uptest-local PROVIDER_NAME=provider-aws EXAMPLE_LIST=$(find provider-aws/examples/secretsmanager/*.yaml | tr '\n' ',')
```

The local invocation is intentionally lightweight and skips the local cluster,
credentials and ProviderConfig setup assuming you already have it all already
configured in your environment.

For a more heavyweight setup see `run_automated_tests` target which is used in a
centralized GitHub Actions invocation.