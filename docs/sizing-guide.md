# Sizing Guide

As a result of various tests (see [provider-aws], [provider-azure], and 
[provider-gcp] issues) on the new runtime features, the following average resource 
utilization has been observed.

| Provider Name  | Memory (Avg.) | Memory (Peak) | CPU (Avg.) |
|----------------|---------------|---------------|------------|
| provider-aws   | 1.1 GB        | 3 GB          | 8 Cores    |
| provider-azure | 1 GB          | 3.5 GB        | 4 Cores    |
| provider-gcp   | 750 MB        | 2 GB          | 3 Cores    |

**Memory (Avg.)** represents the average memory consumption. This value was 
obtained from end-to-end tests that contain the provision and deletion of many 
MRs.

**Memory (Peak)** represents the peak consumption observed during end-to-end 
tests. This metric is important because the terraform provider process reaches 
this consumption value. So, if you use a machine with lower memory, some 
`OOMKilled` errors may be observed for the provider pod. In short, these values 
are the minimum recommendation for memory.

**CPU (Avg.)** represents the average consumption of the CPU. The unit of this 
metric is the number of cores. 

## Some Relevant Command-Line Options

We have some command-line options relevant to the provider's performance. 

- `max-reconcile-rate`: The global maximum rate per second at which resources may 
be checked for drift from the desired state. This option directly affects the 
number of reconciliations. There are a number of internal parameters set by this
option. `max-reconcile-rate` configures both the average rate and the burstiness 
of the global token bucket rate limiter, which acts as a rate limiter across 
all the managed resource controllers. It also sets each controller’s maximum 
concurrent reconciles option. The default for this command-line option is 10. 
Thus with this default the global rate limiter allows on average 10 
reconciliations per second allowing bursts of up to 100 (10 * `max-reconcile-rate`). 
And each managed resource controller will have 10 reconciliation workers. 
This parameter has an impact on the CPU utilization of the Crossplane provider. 
Higher values will result in higher CPU utilization depending on a number of 
other factors.

- `poll`: Poll interval controls how often an individual resource should be 
checked for drift. This option has a Go [time.ParseDuration syntax]. Examples are 
`5m`, `10m`, `1h`. The default is `10m`, meaning that each managed resource 
will be reconciled to check for drifts at least every 10 minutes. An update on 
the managed resource will trigger an early reconciliation.

- `provider-ttl`: TTL for the terraform provider processes before they are 
replaced, i.e., gracefully terminated and restarted. The Upjet runtime replaces 
the shared Terraform provider processes to prevent any potential memory leaks 
from them. If the value of this option is very high, the memory consumption of 
the Crossplane provider pod may increase. The default is 100.

- `terraform-native-provider-path`: Terraform provider path for shared execution. 
To disable the shared server runtime, you can set it to an empty string: 
`--terraform-native-provider-path=""`. The default is determined at build time 
via an environment variable specific to the provider and is the path at which 
the Terraform provider binary resides in the pod’s filesystem.

## Some Limitations
The new runtime has some limitations:

- The shared scheduler currently has no cap on the number of forked Terraform 
provider processes. If you have many AWS accounts (and many corresponding 
ProviderConfigs for them) in a single cluster, and if you have MRs referencing 
these ProviderConfigs, the Upjet runtime will fork long-running Terraform 
provider processes for each. In such a case, you may just want to disable the 
shared server runtime by passing `--terraform-native-provider-path=""` as a 
command-line parameter to the provider.

- Until an expired shared Terraform provider is replaced gracefully, you may 
observe temporary errors like the following:
`cannot schedule a native provider during observe: e3415719-13ce-45fc-8c82-4753a170ea06: 
cannot schedule native Terraform provider process: native provider reuse budget 
has been exceeded: invocationCount: 115, ttl: 100` Such errors are temporary and 
the MRs should eventually be resync’ed once the Terraform provider process is 
gracefully replaced.

- The `--max-reconcile-rate` command-line option sets the maximum number of 
concurrent reconcilers to be used with each managed resource controller. Upjet 
executes certain Terrafom CLI commands asynchronously and this may result in 
more than the `max-reconcile-rate` CLI invocations to be in flight at a given time.

[provider-aws]: https://github.com/upbound/provider-aws/issues/576
[provider-azure]: https://github.com/upbound/provider-azure/issues/404
[provider-gcp]: https://github.com/upbound/provider-gcp/issues/255
[time.ParseDuration syntax]: https://pkg.go.dev/time#ParseDuration