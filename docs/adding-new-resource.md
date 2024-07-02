<!--
SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->

### Prerequisites

To follow this guide, you will need:

1. A Kubernetes Cluster: For manual/local effort, generally a Kind cluster
is sufficient and can be used. For detailed information about Kind see
[this repo]. An alternative way to obtain a cluster is: [k3d]
2. [Go] installed and configured. Check the provider repo you will be working
with and install the version in the `go.mod` file.
3. [Terraform v1.5.5] installed locally. The last version we used before the
license change.
4. [goimports] installed.

# Adding a New Resource

There are long and detailed guides showing [how to bootstrap a
provider][provider-guide] and [how to configure resources][config-guide]. Here
we will go over the steps that will take us to `v1beta1` quality.

1. Fork the provider repo to which you will add resources and create a feature
branch.

2. Go to the Terraform Registry page of the resource you will add. We will add
the resource [`aws_redshift_endpoint_access`] as an example in this guide.
We will use this page in the following steps, especially in determining the
external name configuration, determining conflicting fields, etc.

3. Determine the resource's external name configuration:
Our external name configuration relies on the Terraform ID format of the
resource which we find in the import section on the Terraform Registry page.
Here we'll look for clues about how the Terraform ID is shaped so that we can
infer the external name configuration. In this case, there is an `endpoint_name`
argument seen under the `Argument Reference` section and when we look at
[Import] section, we see that this is what's used to import, i.e. Terraform ID
is the same as the `endpoint_name` argument. This means that we can use
`config.ParameterAsIdentifier("endpoint_name")` configuration from Upjet as our
external name config. See section [External Name Cases] to see how you can infer
in many different cases of Terraform ID.

4. Check if the resource is an Terraform Plugin SDK resource or Terraform Plugin
Framework resource from the [source code].

- For SDK resources, you will see a comment line like `// @SDKResource` in the
source code.
The `aws_redshift_endpoint_access` resource is an SDK resource, go to
`config/externalname.go` and add the following line to the
`TerraformPluginSDKExternalNameConfigs` table:

  - Check the `redshift` group, if there is a group, add the external-name config below:
  ```golang
  // redshift
  ...
  // Redshift endpoint access can be imported using the endpoint_name
  "aws_redshift_endpoint_access": config.ParameterAsIdentifier("endpoint_name"),
  ```
  - If there is no group, continue by adding the group name as a comment line.

- For Framework resources, you will see a comment line like 
`// @FrameworkResource` in the source code. If the resource is a Framework
resource, add the external-name config to the
`TerraformPluginFrameworkExternalNameConfigs` table.

*Note: Look at the `config/externalnamenottested.go` file and check if there is
a configuration for the resource and remove it from there.*

5. Run `make submodules` to initialize the build submodule and run 
`make generate`. When the command process is completed, you will see that the
controller, CRD, generated example, and other necessary files for the resource
have been created and modified.

```bash
> git status
On branch add-redshift-endpoint-access
Changes not staged for commit:
  (use "git add <file>..." to update what will be committed)
  (use "git restore <file>..." to discard changes in working directory)
	modified:   apis/redshift/v1beta1/zz_generated.conversion_hubs.go
	modified:   apis/redshift/v1beta1/zz_generated.deepcopy.go
	modified:   apis/redshift/v1beta1/zz_generated.managed.go
	modified:   apis/redshift/v1beta1/zz_generated.managedlist.go
	modified:   apis/redshift/v1beta1/zz_generated.resolvers.go
	modified:   config/externalname.go
	modified:   config/externalnamenottested.go
	modified:   config/generated.lst
	modified:   internal/controller/zz_monolith_setup.go
	modified:   internal/controller/zz_redshift_setup.go

Untracked files:
  (use "git add <file>..." to include in what will be committed)
	apis/redshift/v1beta1/zz_endpointaccess_terraformed.go
	apis/redshift/v1beta1/zz_endpointaccess_types.go
	examples-generated/redshift/v1beta1/endpointaccess.yaml
	internal/controller/redshift/endpointaccess/
	package/crds/redshift.aws.upbound.io_endpointaccesses.yaml
```

6. Go through the "Warning" boxes (if any) in the Terraform Registry page to
see whether any of the fields are represented as separate resources as well.
It usually goes like this:

    > Routes can be defined either directly on the azurerm_iothub
    > resource, or using the azurerm_iothub_route resource - but the two cannot be
    > used together.

    In such cases, the field should be moved to status since we prefer to
    represent it only as a separate CRD. Go ahead and add a configuration block
    for that resource similar to the following:

    ```golang
    p.AddResourceConfigurator("azurerm_iothub", func(r *config.Resource) {
      // Mutually exclusive with azurerm_iothub_route
      config.MoveToStatus(r.TerraformResource, "route")
    })
    ```

7. Resource configuration is largely done, so we need to prepare the example
YAML for testing. Copy `examples-generated/redshift/v1beta1/endpointaccess.yaml`
into `examples/redshift/v1beta1/endpointaccess.yaml` and check the dependent
resources, if not, please add them to the YAML file.

```
NOTE: The resources that are tried to be created may have dependencies. For
example, you might actually need resources Y and Z while trying to test resource
X. Many of the generated examples include these dependencies. However, in some
cases, there may be missing dependencies. In these cases, please add the
relevant dependencies to your example manifest. This is important both for you
to pass the tests and to provide the correct manifests.
``` 

- In our case, the generated example has required fields
`spec.forProvider.clusterIdentifierSelector` and
`spec.forProvider.subnetGroupNameSelector`. We need to check its argument list
in Terraform documentation and figure out which field needs a reference to
which resource. Let's check the [cluster_identifier] field, we see that the
field requires a reference to the `Cluster.redshift` resource identifier.
For the [subnet_group_name] field, we see that the field requires a reference
to the `SubnetGroup.redshift` resource ID.

Then add the `Cluster.redshift` and `SubnetGroup.redshift` resource examples
to our YAML file and edit the annotations and labels.

```yaml
apiVersion: redshift.aws.upbound.io/v1beta1
kind: EndpointAccess
metadata:
  annotations:
    meta.upbound.io/example-id: redshift/v1beta1/endpointaccess
  labels:
    testing.upbound.io/example-name: example
  name: example-endpointaccess
spec:
  forProvider:
    clusterIdentifierSelector:
      matchLabels:
        testing.upbound.io/example-name: example-endpointaccess
    region: us-west-1
    subnetGroupNameSelector:
      matchLabels:
        testing.upbound.io/example-name: example-endpointaccess
---
apiVersion: redshift.aws.upbound.io/v1beta1
kind: Cluster
metadata:
  annotations:
    meta.upbound.io/example-id: redshift/v1beta1/endpointaccess
  labels:
    testing.upbound.io/example-name: example-endpointaccess
  name: example-endpointaccess-c
spec:
  forProvider:
    clusterType: single-node
    databaseName: mydb
    masterPasswordSecretRef:
      key: example-key
      name: cluster-secret
      namespace: upbound-system
    masterUsername: exampleuser
    nodeType: ra3.xlplus
    region: us-west-1
    skipFinalSnapshot: true
---
apiVersion: redshift.aws.upbound.io/v1beta1
kind: SubnetGroup
metadata:
  annotations:
    meta.upbound.io/example-id: redshift/v1beta1/endpointaccess
  labels:
    testing.upbound.io/example-name: example-endpointaccess
  name: example-endpointaccess-sg
spec:
  forProvider:
    region: us-west-1
    subnetIdRefs:
    - name: foo
    - name: bar
    tags:
      environment: Production
```

Here the references for `clusterIdentifier` and `subnetGroupName` are
[automatically] defined.

If it is not defined automatically or if you want to define a reference for
another field, please see [Cross Resource Referencing].

8. Create a commit to cover all changes so that it's easier for the reviewer
with a message like the following:
`Configure EndpointAccess.redshift resource and add example`

9. Run `make reviewable` to ensure this PR is ready for review.

10. That's pretty much all we need to do in the codebase, we can open a
new PR: `git push --set-upstream origin add-redshift-endpoint-access`

# Testing Instructions

While configuring resources, the testing effort is the longest part, because the
characteristics of cloud providers and services can change. This test effort can
be executed in two main methods. The first one is testing the resources in a
manual way and the second one is using the [Uptest] which is an automated test
tool for Official Providers. `Uptest` provides a framework to test resources in
an end-to-end pipeline during the resource configuration process. Together with
the example manifest generation tool, it allows us to avoid manual interventions
and shortens testing processes.

## Automated Tests - Uptest 

After providing all the required fields of the resource we added and added
dependent resources, if any, we can start with automatic testing. To trigger
automated tests, you must have one approved PR and be a contributor in the
relevant repo. In other cases, maintainers will trigger automatic tests when
your PR is ready. To trigger it, you can drop [a comment] on the PR containing
the following:

```
/test-examples="examples/redshift/v1beta1/endpointaccess.yaml"
```

Once the automated tests pass, we're good to go. All you have to do is put
the link to the successful uptest run in the `How has this code been tested`
section in the PR description.

If the automatic test fails, click on the uptest run details, then click
`e2e/uptest` -> `Run uptest` and try to debug from the logs.

In adding the `EndpointAccess.redshift` resource case, we see the following
error from uptest run logs:

```
    logger.go:42: 14:32:49 | case/0-apply |     - lastTransitionTime: "2024-05-20T14:25:08Z"
    logger.go:42: 14:32:49 | case/0-apply |       message: 'cannot resolve references: mg.Spec.ForProvider.SubnetGroupName: no
    logger.go:42: 14:32:49 | case/0-apply |         resources matched selector'
    logger.go:42: 14:32:49 | case/0-apply |       reason: ReconcileError
    logger.go:42: 14:32:49 | case/0-apply |       status: "False"
    logger.go:42: 14:32:49 | case/0-apply |       type: Synced
```

Make the fixes, create a [new commit], and trigger the automated test again.

**Ignoring Some Resources in Automated Tests**

Some resources require manual intervention such as providing valid public keys
or using on-the-fly values. These cases can be handled in manual tests, but in
cases where we cannot provide generic values for automated tests, we can skip
some resources in the tests of the relevant group via an annotation:

```yaml
upjet.upbound.io/manual-intervention: "The Certificate needs to be provisioned successfully which requires a real domain."
```

The key is important for skipping. We are checking this
`upjet.upbound.io/manual-intervention` annotation key and if it is in there, we
skip the related resource. The value is also important to see why we skip this
resource.

```
NOTE: For resources that are ignored during Automated Tests, manual testing is a
must, because we need to make sure that all resources published in the `v1beta1`
version is working.
```

### Running Uptest locally

For a faster feedback loop, you might want to run `uptest` locally in your
development setup. For this, you can use the e2e make target available in
the provider repositories. This target requires the following environment
variables to be set:

- `UPTEST_CLOUD_CREDENTIALS`: cloud credentials for the provider being tested.
- `UPTEST_EXAMPLE_LIST`: a comma-separated list of examples to test.
- `UPTEST_DATASOURCE_PATH`: (optional), see [Injecting Dynamic Values (and Datasource)]

You can check the e2e target in the Makefile for each provider. Let's check the [target]
in provider-upjet-aws and run a test for the resource `examples/ec2/v1beta1/vpc.yaml`.

- You can either save your credentials in a file as stated in the target's [comments],
or you can do it by adding your credentials to the command below.

```console
export UPTEST_CLOUD_CREDENTIALS="DEFAULT='[default]
aws_access_key_id = <YOUR-ACCESS_KEY_ID>
aws_secret_access_key = <YOUR-ACCESS_KEY'"
```

```console
export UPTEST_EXAMPLE_LIST="examples/ec2/v1beta1/vpc.yaml"
```

After setting the above environment variables, run `make e2e`. If the test is
successful, you will see a log like the one below, kindly add to the PR
description this log:

```console
--- PASS: kuttl (37.41s)
    --- PASS: kuttl/harness (0.00s)
        --- PASS: kuttl/harness/case (36.62s)
PASS
14:02:30 [ OK ] running automated tests
```

## Manual Test

Configured resources can be tested by using manual methods. This method generally
contains the environment preparation and creating the example manifest in the
Kubernetes cluster steps. The following steps can be followed for preparing the
environment:

1. Registering the CRDs (Custom Resource Definitions) to Cluster: We need to
apply the CRD manifests to the cluster. The relevant manifests are located in
the `package/crds` folder of provider subdirectories such as:
`provider-aws/package/crds`. For registering them please run the following
command: `kubectl apply -f package/crds`

2. Create ProviderConfig: ProviderConfig Custom Resource contains some
configurations and credentials for the provider. For example, to connect to the
cloud provider, we use the credentials field of ProviderConfig. For creating the
ProviderConfig with correct credentials, please see:

- [Create a Kubernetes secret with the AWS credentials]
- [Create a Kubernetes secret with the Azure credentials]
- [Create a Kubernetes secret with the GCP credentials]

3. Start Provider: For every Custom Resource, there is a controller and these
controllers are part of the provider. So, for starting the reconciliations for
Custom Resources, we need to run the provider (collect of controllers). For
running provider: Run `make run`

4. Now, you can create the examples you've generated and check events/logs to
spot problems and fix them.

- Start Testing: After completing the steps above, your environment is ready for
testing. There are 3 steps we need to verify in manual tests: `Apply`, `Import`,
`Delete`.

### Apply:

We need to apply the example manifest to the cluster.

```bash
kubectl apply -f examples/redshift/v1beta1/endpointaccess.yaml
```

Successfully applying the example manifests to the cluster is only the first
step. After successfully creating the Managed Resources, we need to check
whether their statuses are ready or not. So we need to expect a `True` value for
`Synced` and `Ready` conditions. To check the statuses of all created example
manifests quickly you can run the `kubectl get managed` command. We will wait
for all values to be `True` in this list:

```bash
NAME                            SYNCED   READY   EXTERNAL-NAME              AGE
subnet.ec2.aws.upbound.io/bar   True     True    subnet-0149bf6c20720d596   26m
subnet.ec2.aws.upbound.io/foo   True     True    subnet-02971ebb943f5bb6e   26m

NAME                         SYNCED   READY   EXTERNAL-NAME           AGE
vpc.ec2.aws.upbound.io/foo   True     True    vpc-0ee6157df1f5a116a   26m

NAME                                                       SYNCED   READY   EXTERNAL-NAME              AGE
cluster.redshift.aws.upbound.io/example-endpointaccess-c   True     True    example-endpointaccess-c   26m

NAME                                                            SYNCED   READY   EXTERNAL-NAME            AGE
endpointaccess.redshift.aws.upbound.io/example-endpointaccess   True     True    example-endpointaccess   26m

NAME                                                            SYNCED   READY   EXTERNAL-NAME               AGE
subnetgroup.redshift.aws.upbound.io/example-endpointaccess-sg   True     True    example-endpointaccess-sg   26m
```

As a second step, we need to check the `UpToDate` status condition. This status
condition will be visible when you set the annotation: `upjet.upbound.io/test=true`.
Without adding this annotation you cannot see the mentioned condition. The rough
significance of this condition is to make sure that the resource does not remain
in an update loop. To check the `UpToDate` condition for all MRs in the cluster,
run:

```bash
kubectl annotate managed --all upjet.upbound.io/test=true --overwrite
# check the conditions
kubectl get endpointaccess.redshift.aws.upbound.io/example-endpointaccess -o yaml
```

You should see the output below:

```yaml
  conditions:
  - lastTransitionTime: "2024-05-20T17:37:20Z"
    reason: Available
    status: "True"
    type: Ready
  - lastTransitionTime: "2024-05-20T17:37:11Z"
    reason: ReconcileSuccess
    status: "True"
    type: Synced
  - lastTransitionTime: "2024-05-20T17:37:15Z"
    reason: Success
    status: "True"
    type: LastAsyncOperation
  - lastTransitionTime: "2024-05-20T17:37:48Z"
    reason: UpToDate
    status: "True"
    type: Test
```

When all of the fields are `True`, the `Apply` test was successfully completed!

### Import

There are a few steps to perform the import test, here we will stop the provider,
delete the status conditions, and check the conditions when we re-run the provider.

- Stop `make run`
- Delete the status conditions with the following command:
```bash
kubectl --subresource=status patch endpointaccess.redshift.aws.upbound.io/example-endpointaccess --type=merge -p '{"status":{"conditions":[]}}'
```
- Store the `status.atProvider.id` field for comparison
- Run `make run`
- Make sure that the `Ready`, `Synced`, and `UpToDate` conditions are `True`
- Compare the new `status.atProvider.id` with the one you stored and make sure
they are the same

The import test was successful when the above conditions were met.

### Delete

Make sure the resource has been successfully deleted by running the following
command:

```bash
kubectl delete endpointaccess.redshift.aws.upbound.io/example-endpointaccess
```

When the resource is successfully deleted, the manual testing steps are completed.

```
IMPORTANT NOTE: `make generate` and `kubectl apply -f package/crds` commands
must be run after any change that will affect the schema or controller of the
configured/tested resource.

In addition, the provider needs to be restarted after the changes in the
controllers, because the controller change actually corresponds to the changes
made in the running code.
```

You can look at the [PR] we created for the `EndpointAccess.redshift` resource
we added in this guide.

## External Name Cases

### Case 1: `name` As Identifier

There is a `name` argument under the `Argument Reference` section and `Import`
section suggests to use `name` to import the resource.

Use `config.NameAsIdentifier`.

An example would be [`aws_eks_cluster`] and [here][eks-config] is its
configuration.

### Case 2: Parameter As Identifier

There is an argument under the `Argument Reference` section that is used like
name, i.e. `cluster_name` or `group_name`, and the `Import` section suggests
using the value of that argument to import the resource.

Use `config.ParameterAsIdentifier(<name of the argument parameter>)`.

An example would be [`aws_elasticache_cluster`] and [here][cache-config] is its
configuration.

### Case 3: Random Identifier From Provider

The ID used in the `Import` section is completely random and assigned by the
provider, like a UUID, where you don't have any means of impact on it.

Use `config.IdentifierFromProvider`.

An example would be [`aws_vpc`] and [here][vpc-config] is its configuration.

### Case 4: Random Identifier Substring From Provider

The ID used in the `Import` section is partially random and assigned by the
provider. For example, a node in a cluster could have a random ID like `13213`
but the Terraform Identifier could include the name of the cluster that's
represented as an argument field under `Argument Reference`, i.e.
`cluster-name:23123`. In that case, we'll use only the randomly assigned part
as external name and we need to tell Upjet how to construct the full ID back
and forth.

```golang
func resourceName() config.ExternalName{
  e := config.IdentifierFromProvider
  e.GetIDFn = func(_ context.Context, externalName string, parameters map[string]interface{}, _ map[string]interface{}) (string, error) {
		cl, ok := parameters["cluster_name"]
		if !ok {
			return "", errors.New("cluster_name cannot be empty")
		}
		return fmt.Sprintf("%s:%s", cl.(string), externalName), nil
	}
	e.GetExternalNameFn = func(tfstate map[string]interface{}) (string, error) {
		id, ok := tfstate["id"]
		if !ok {
			return "", errors.New("id in tfstate cannot be empty")
		}
		w := strings.Split(s.(string), ":")
		return w[len(w)-1], nil
	}
}
```

### Case 5: Non-random Substrings as Identifier

There are more than a single argument under `Argument Reference` that are
concatenated to make up the whole identifier, e.g. `<region>/<cluster
name>/<node name>`. We will need to tell Upjet to use `<node name>` as external
name and take the rest from the parameters.

Use `config.TemplatedStringAsIdentifier("<name argument>", "<go template>")` in
such cases. The following is the list of available parameters for you to use in
your go template:

```
parameters: A tree of parameters that you'd normally see in a Terraform HCL
            file. You can use TF registry documentation of given resource to
            see what's available.

terraformProviderConfig: The Terraform configuration object of the provider. You can
                take a look at the TF registry provider configuration object
                to see what's available. Not to be confused with ProviderConfig
                custom resource of the Crossplane provider.

externalName: The value of external name annotation of the custom resource.
              It is required to use this as part of the template.
```

You can see example usages in the big three providers below.

#### AWS

For `aws_glue_user_defined_function`, we see that the `name` argument is used
to name the resource and the import instructions read as the following:

> Glue User Defined Functions can be imported using the
> `catalog_id:database_name:function_name`. If you have not set a Catalog ID
> specify the AWS Account ID that the database is in, e.g.,

> $ terraform import aws_glue_user_defined_function.func
123456789012:my_database:my_func


Our configuration would look like the following:

```golang
"aws_glue_user_defined_function":  config.TemplatedStringAsIdentifier("name", "{{ .parameters.catalog_id }}:{{ .parameters.database_name }}:{{ .externalName }}")
```

Another prevalent case in AWS is the usage of Amazon Resource Name (ARN) to
identify a resource. We can use `config.TemplatedStringAsIdentifier` in many of
those cases like the following:

```
"aws_glue_registry": config.TemplatedStringAsIdentifier("registry_name", "arn:aws:glue:{{ .parameters.region }}:{{ .setup.client_metadata.account_id }}:registry/{{ .external_name }}"),
```

However, there are cases where the ARN includes random substring and that would
fall under Case 4. The following is such an example:

```
// arn:aws:acm-pca:eu-central-1:609897127049:certificate-authority/ba0c7989-9641-4f36-a033-dee60121d595
	"aws_acmpca_certificate_authority_certificate": config.IdentifierFromProvider,
```

#### Azure

Most Azure resources fall under this case since they use fully qualified
identifier as Terraform ID.

For `azurerm_mariadb_firewall_rule`, we see that the `name` argument is used to
name the resource and the import instructions read as the following:

> MariaDB Firewall rules can be imported using the resource ID, e.g.
>
> `terraform import azurerm_mariadb_firewall_rule.rule1 /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/mygroup1/providers/Microsoft.DBforMariaDB/servers/server1/firewallRules/rule1`

Our configuration would look like the following:

```golang
"azurerm_mariadb_firewall_rule":  config.TemplatedStringAsIdentifier("name", "/subscriptions/{{ .terraformProviderConfig.subscription_id }}/resourceGroups/{{ .parameters.resource_group_name }}/providers/Microsoft.DBforMariaDB/servers/{{ .parameters.server_name }}/firewallRules/{{ .externalName }}")
```

In some resources, an argument requires ID, like `azurerm_cosmosdb_sql_function`
where it has `container_id` and `name` but no separate `resource_group_name`
which would be required to build the full ID. Our configuration would look like
the following in this case:

```golang
config.TemplatedStringAsIdentifier("name", "{{ .parameters.container_id }}/userDefinedFunctions/{{ .externalName }}")
```

#### GCP

Most GCP resources fall under this case since they use fully qualified
identifier as Terraform ID.

For `google_container_cluster`, we see that the `name` argument is used to name
the resource and the import instructions read as the following:

> GKE clusters can be imported using the project, location, and name.
> If the project is omitted, the default provider value will be used.
> Examples:
> 
> ```console
> $ terraform import google_container_cluster.mycluster projects/my-gcp-project/locations/us-east1-a/clusters/my-cluster
> $ terraform import google_container_cluster.mycluster my-gcp-project/us-east1-a/my-cluster
> $ terraform import google_container_cluster.mycluster us-east1-a/my-cluster
> ```

In cases where there are multiple ways to construct the ID, we should take the
one with the least parameters so that we rely only on required fields because
optional fields may have some defaults that are assigned after the creation
which may make it tricky to work with. In this case, the following would be our
configuration:

```golang
"google_compute_instance":  config.TemplatedStringAsIdentifier("name", "{{ .parameters.location }}/{{ .externalName }}")
```

There are cases where one of the example import commands uses just `name`, like
`google_compute_instance`:

```console
terraform import google_compute_instance.default {{name}}
```

In such cases, we should use `config.NameAsIdentifier` since we'd like to have
the least complexity in our configuration as possible.

### Case 6: No Import Statement

There are no instructions under the `Import` section of the resource page in
Terraform Registry, like `aws_acm_certificate_validation` from AWS.

Use the following in such cases with a comment indicating the case:

```golang
// No import documented.
"aws_acm_certificate_validation": config.IdentifierFromProvider,
```

### Case 7: Using Identifier of Another Resource

There are auxiliary resources that don't have an ID and since they map
one-to-one to another resource, they just opt to use the identifier of that
other resource. In many cases, the identifier is also a valid argument, maybe
even the only argument, to configure this resource.

An example would be
[`aws_ecrpublic_repository_policy`] from AWS where the identifier is
`repository_name`.

Use `config.IdentifierFromProvider` because in these cases `repository_name` is
more meaningful as an argument rather than the name of the policy for users,
hence we assume the ID is coming from the provider.

### Case 8: Using Identifiers of Other Resources

There are resources that mostly represent a relation between two resources
without any particular name that identifies the relation. An example would be
[`azurerm_subnet_nat_gateway_association`] where the ID is made up of two
arguments `nat_gateway_id` and `subnet_id` without any particular field used
to give a name to the resource.

Use `config.IdentifierFromProvider` because in these cases, there is no name
argument to be used as external name and both creation and import scenarios
would work the same way even if you configured the resources with conversion
functions between arguments and ID.

## No Matching Case

If it doesn't match any of the cases above, then we'll need to implement the
external name configuration from the ground up. Though in most cases, it's just
a little bit different that we only need to override a few things on top of
common functions.

One example is [`aws_route`] resource where the ID could use a different
argument depending on which one is given. You can take a look at the
implementation [here][route-impl]. [This section] in the
detailed guide could also help you.


[comment]: <> (References)

[this repo]: https://github.com/kubernetes-sigs/kind
[k3d]: https://k3d.io/
[Go]: https://go.dev/doc/install
[Terraform v1.5.5]: https://developer.hashicorp.com/terraform/install
[goimports]: https://pkg.go.dev/golang.org/x/tools/cmd/goimports
[provider-guide]: https://github.com/upbound/upjet/blob/main/docs/generating-a-provider.md
[config-guide]: https://github.com/crossplane/upjet/blob/main/docs/configuring-a-resource.md
[`aws_redshift_endpoint_access`]: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/redshift_endpoint_access
[Import]: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/redshift_endpoint_access#import
[External Name Cases]: #external-name-cases
[source code]: https://github.com/hashicorp/terraform-provider-aws/blob/f222bd785228729dc1f5aad7d85c4d04a6109075/internal/service/redshift/endpoint_access.go#L24
[cluster_identifier]: https://registry.terraform.io/providers/hashicorp/aws/5.35.0/docs/resources/redshift_endpoint_access#cluster_identifier
[subnet_group_name]: https://registry.terraform.io/providers/hashicorp/aws/5.35.0/docs/resources/redshift_endpoint_access#subnet_group_name
[automatically]: https://github.com/crossplane/upjet/blob/main/docs/configuring-a-resource.md#auto-cross-resource-reference-generation
[Cross Resource Referencing]: https://github.com/crossplane/upjet/blob/main/docs/configuring-a-resource.md#cross-resource-referencing
[a comment]: https://github.com/crossplane-contrib/provider-upjet-aws/pull/1314#issuecomment-2120539099
[new commit]: https://github.com/crossplane-contrib/provider-upjet-aws/pull/1314/commits/b76e566eea5bd53450f2175e7e5a6e274934255b
[Create a Kubernetes secret with the AWS credentials]: https://docs.crossplane.io/latest/getting-started/provider-aws/#create-a-kubernetes-secret-with-the-aws-credentials
[Create a Kubernetes secret with the Azure credentials]: https://docs.crossplane.io/latest/getting-started/provider-azure/#create-a-kubernetes-secret-with-the-azure-credentials
[Create a Kubernetes secret with the GCP credentials]: https://docs.crossplane.io/latest/getting-started/provider-gcp/#create-a-kubernetes-secret-with-the-gcp-credentials
[PR]: https://github.com/crossplane-contrib/provider-upjet-aws/pull/1314
[`aws_eks_cluster`]: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/eks_cluster
[eks-config]: https://github.com/upbound/provider-aws/blob/8b3887c91c4b44dc14e1123b3a5ae1a70e0e45ed/config/externalname.go#L284
[`aws_elasticache_cluster`]: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_cluster
[cache-config]: https://github.com/upbound/provider-aws/blob/8b3887c91c4b44dc14e1123b3a5ae1a70e0e45ed/config/externalname.go#L299
[`aws_vpc`]: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc
[vpc-config]: https://github.com/upbound/provider-aws/blob/8b3887c91c4b44dc14e1123b3a5ae1a70e0e45ed/config/externalname.go#L155
[`aws_ecrpublic_repository_policy`]: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecrpublic_repository_policy
[`azurerm_subnet_nat_gateway_association`]: https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet_nat_gateway_association
[`aws_route`]: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route
[route-impl]: https://github.com/upbound/provider-aws/blob/8b3887c91c4b44dc14e1123b3a5ae1a70e0e45ed/config/externalname.go#L172
[This section]: #external-name-cases
[Injecting Dynamic Values (and Datasource)]: https://github.com/crossplane/uptest?tab=readme-ov-file#injecting-dynamic-values-and-datasource
[target]: https://github.com/crossplane-contrib/provider-upjet-aws/blob/e4b8f222a4baf0ea37caf1d348fe109bf8235dc2/Makefile#L257
[comments]: https://github.com/crossplane-contrib/provider-upjet-aws/blob/e4b8f222a4baf0ea37caf1d348fe109bf8235dc2/Makefile#L259
[Uptest]: https://github.com/crossplane/uptest
