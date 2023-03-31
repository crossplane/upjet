## Monitoring the Upjet Runtime
The [Kubernetes controller-runtime] library provides a Prometheus metrics
endpoint by default. The Upjet based providers including the
[upbound/provider-aws], [upbound/provider-azure], [upbound/provider-azuread] and
[upbound/provider-gcp] expose [various
metrics](https://book.kubebuilder.io/reference/metrics-reference.html)
from the controller-runtime to help monitor the health of the various runtime
components, such as the [`controller-runtime` client], the [leader election
client], the [controller workqueues], etc. In addition to these metrics, each
controller also
[exposes](https://github.com/kubernetes-sigs/controller-runtime/blob/60af59f5b22335516850ca11c974c8f614d5d073/pkg/internal/controller/metrics/metrics.go#L25)
various metrics related to the reconciliation of the custom resources and active
reconciliation worker goroutines.

In addition to these metrics exposed by the `controller-runtime`, the Upjet
based providers also expose metrics specific to the Upjet runtime. The Upjet
runtime registers some custom metrics using the [available extension
mechanism](https://book.kubebuilder.io/reference/metrics.html#publishing-additional-metrics),
and are available from the default `/metrics` endpoint of the provider pod. Here
are these custom metrics exposed from the Upjet runtime:
- `upjet_terraform_cli_duration`: This is a histogram metric and reports
  statistics, in seconds, on how long it takes a Terraform CLI invocation to
  complete.
- `upjet_terraform_active_cli_invocations`: This is a gauge metric and it's the
  number of active (running) Terraform CLI invocations.
- `upjet_terraform_running_processes`: This is a gauge metric and it's the
  number of running Terraform CLI and Terraform provider processes.
- `upjet_resource_ttr`: This is a histogram metric and it measures, in seconds,
  the time-to-readiness for managed resources.

Prometheus metrics can have [labels] associated with them to differentiate the
characteristics of the measurements being made, such as differentiating between
the CLI processes and the Terraform provider processes when counting the number
of active Terraform processes running. Here is a list of labels associated with
each of the above custom Upjet metrics:
- Labels associated with the `upjet_terraform_cli_duration` metric:
    - `subcommand`: The `terraform` subcommand that's run, e.g., `init`,
      `apply`, `plan`, `destroy`, etc.
    - `mode`: The execution mode of the Terraform CLI, one of `sync` (so that
      the CLI was invoked synchronously as part of a reconcile loop), `async`
      (so that the CLI was invoked asynchronously, the reconciler goroutine will
      poll and collect results in future).
- Labels associated with the `upjet_terraform_active_cli_invocations` metric:
    - `subcommand`: The `terraform` subcommand that's run, e.g., `init`,
      `apply`, `plan`, `destroy`, etc.
    - `mode`: The execution mode of the Terraform CLI, one of `sync` (so that
      the CLI was invoked synchronously as part of a reconcile loop), `async`
      (so that the CLI was invoked asynchronously, the reconciler goroutine will
      poll and collect results in future).
- Labels associated with the `upjet_terraform_running_processes` metric:
    - `type`: Either `cli` for Terraform CLI (the `terraform` process) processes
      or `provider` for the Terraform provider processes. Please note that this
      is a best effort metric that may not be able to precisely catch & report
      all relevant processes. We may, in the future, improve this if needed by
      for example watching the `fork` system calls. But currently, it may prove
      to be useful to watch rouge Terraform provider processes.
- Labels associated with the `upjet_resource_ttr` metric:
    - `group`, `version`, `kind` labels record the [API group, version and
      kind](https://kubernetes.io/docs/reference/using-api/api-concepts/) for
      the managed resource, whose
      [time-to-readiness](https://github.com/crossplane/terrajet/issues/55#issuecomment-929494212)
      measurement is captured.

## Examples
You can [export](https://book.kubebuilder.io/reference/metrics.html) all these
custom metrics and the `controller-runtime` metrics from the provider pod for
Prometheus. Here are some examples showing the custom metrics in action from the
Prometheus console:

- `upjet_terraform_active_cli_invocations` gauge metric showing the sync & async
  `terraform init/apply/plan/destroy` invocations: <img width="3000" alt="image"
  src="https://user-images.githubusercontent.com/9376684/223296539-94e7d634-58b0-4d3f-942e-8b857eb92ef7.png">

- `upjet_terraform_running_processes` gauge metric showing both `cli` and
  `provider` labels: <img width="2999" alt="image"
  src="https://user-images.githubusercontent.com/9376684/223297575-18c2232e-b5af-4cc1-916a-d61fe5dfb527.png">

- `upjet_terraform_cli_duration` histogram metric, showing average Terraform CLI
  running times for the last 5m: <img width="2993" alt="image"
  src="https://user-images.githubusercontent.com/9376684/223299401-8f128b74-8d9c-4c82-86c5-26870385bee7.png">

- The medians (0.5-quantiles) for these observations aggregated by the mode and
Terraform subcommand being invoked: <img width="2999" alt="image"
src="https://user-images.githubusercontent.com/9376684/223300766-c1adebb9-bd19-4a38-9941-116185d8d39f.png">

- `upjet_resource_ttr` histogram metric, showing average resource TTR for the
  last 10m: <img width="2999" alt="image"
  src="https://user-images.githubusercontent.com/9376684/223309711-edef690e-2a59-419b-bb93-8f837496bec8.png">

- The median (0.5-quantile) for these TTR observations: <img width="3002"
alt="image"
src="https://user-images.githubusercontent.com/9376684/223309727-d1a0f4e2-1ed2-414b-be67-478a0575ee49.png">

These samples have been collected by provisioning 10 [upbound/provider-aws]
`cognitoidp.UserPool` resources by running the provider with a poll interval of
1m. In these examples, one can observe that the resources were polled
(reconciled) twice after they acquired the `Ready=True` condition and after
that, they were destroyed.

## Reference
You can find a full reference of the exposed metrics from the Upjet-based
providers [here](provider_metrics_help.txt).

[Kubernetes controller-runtime]:
    https://github.com/kubernetes-sigs/controller-runtime
[upbound/provider-aws]: https://github.com/upbound/provider-aws
[upbound/provider-azure]: https://github.com/upbound/provider-azure
[upbound/provider-azuread]: https://github.com/upbound/provider-azuread
[upbound/provider-gcp]: https://github.com/upbound/provider-gcp
[`controller-runtime` client]:
    https://github.com/kubernetes-sigs/controller-runtime/blob/60af59f5b22335516850ca11c974c8f614d5d073/pkg/metrics/client_go_adapter.go#L40
[leader election client]:
    https://github.com/kubernetes-sigs/controller-runtime/blob/60af59f5b22335516850ca11c974c8f614d5d073/pkg/metrics/leaderelection.go#L12
[controller workqueues]:
    https://github.com/kubernetes-sigs/controller-runtime/blob/60af59f5b22335516850ca11c974c8f614d5d073/pkg/metrics/workqueue.go#L40
[labels]: https://prometheus.io/docs/practices/naming/#labels
