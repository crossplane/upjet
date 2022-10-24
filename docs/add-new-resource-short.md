## Adding a New Resource

There are a long and detailed guides showing [how to bootstrap a
provider][provider-guide] and [how to configure resources][config-guide]. Here
we will go over the steps that will take us to `v1beta1` quality without going
into much detail so that it can be followed repeatedly quickly.

The steps are generally identical, so we'll just take a resource issue from AWS
[#90][issue-90] and you can generalize steps pretty much to all other
resources in all official providers. It has several resources from different API
groups, such as `glue`, `grafana`, `guardduty` and `iam`.

1. Assign issue to yourself.
1. Start from the top and click the link for the first resource,
  [`aws_glue_workflow`] in this case.
1. Here we'll look for clues about how the Terraform ID is shaped so that we can
  infer the external name configuration. In this case, there is a `name`
  argument seen under `Argument Reference` section and when we look at `Import`
  section, we see that this is what's used to import, i.e. Terraform ID is same
  as `name` argument. This means that we can use `config.NameAsIdentifier`
  configuration from Upjet as our external name config. See section [External
  Name Cases](#external-name-cases) to see how you can infer in many different
  cases of Terraform ID.
1. First of all, please see the [Moving Untested Resources to v1beta1] 
   documentation.

    Go to `config/externalname.go` and add the following line to
   `ExternalNameConfigs` table:
    ```golang
    // glue
    //
    // Imported using "name".
    "aws_glue_workflow": config.NameAsIdentifier,
    ```
1. Run `make reviewable`.
1. Go through the "Warning" boxes (if any) in the Terraform Registry page to see
  whether any of the fields are represented as separate resources as well. It
  usually goes like
    ```
    Routes can be defined either directly on the azurerm_iothub
    resource, or using the azurerm_iothub_route resource - but the two cannot be
    used together.
    ```
    In such cases, the field should be moved to status since we prefer to
    represent it only as a separate CRD. Go ahead and add a configuration block
    for that resource similar to the following:
    ```golang
    p.AddResourceConfigurator("azurerm_iothub", func(r *config.Resource) {
      // Mutually exclusive with azurerm_iothub_route
      config.MoveToStatus(r.TerraformResource, "route")
    })
    ```
1. Go to the end of the TF registry page to see the timeouts. If they are longer
  than 10 minutes, then we need to set the `UseAsync` property of the resource
  to `true`. Go ahead and add a configuration block for that resource similar to
  the following if it doesn't exist already:
    ```golang
    p.AddResourceConfigurator("azurerm_iothub", func(r *config.Resource) {
      r.UseAsync = true
    })
    ```
    Note that some providers have certain defaults, like Azure has this on by
    default, in such cases you need to set this parameter to `false` if the
    timeouts are less than 10 minutes.
1. Resource configuration is largely done, so we need to prepare the example
  YAML for testing. Copy `examples-generated/glue/workflow.yaml` into
  `examples/glue/workflow.yaml` and then remove `spec.forProvider.name` field.
  If there is nothing left under `spec.forProvider`, then give it empty struct,
  e.g. `forProvider: {}`
1. Repeat the same process for other resources under `glue`.
1. Once `glue` is completed, the following would be the additions we made to the
   external name table and we'd have new examples under `examples/glue` folder.
    ```golang
    // glue
    //
    // Imported using "name".
    "aws_glue_workflow": config.NameAsIdentifier,
    // Imported using arn: arn:aws:glue:us-west-2:123456789012:schema/example/example
    "aws_glue_schema": config.IdentifierFromProvider,
    // Imported using "name".
    "aws_glue_trigger": config.NameAsIdentifier,
    // Imported using the catalog_id:database_name:function_name
    // 123456789012:my_database:my_func
    "aws_glue_user_defined_function":  config.TemplatedStringAsIdentifier("name", "{{ .parameters.catalog_id }}:{{ .parameters.database_name }}:{{ .externalName }}"),
    "aws_glue_security_configuration": config.NameAsIdentifier,
    // Imported using the account ID: 12356789012
    "aws_glue_resource_policy": config.IdentifierFromProvider,
    ```
1. Create a commit to cover all manual changes so that it's easier for reviewer
  with a message like the following `aws: add glue group`.
1. Run `make reviewable` so that new resources are generated.
1. Create another commit with a message like `aws: regenerate for glue group`.

That's pretty much all we need to do in the codebase. With these two commits, we
can open a new PR.

## Testing

Our first option is to run it by the automated testing tool we have. In order to
trigger it, you can drop a comment on the PR containing the following:

```console
# Wildcards like provider-aws/examples/glue/*.yaml also work.
/test-examples="provider-aws/examples/glue/catalogdatabase.yaml,provider-aws/examples/glue/catalogtable.yaml"
```

Once the automated tests pass, we're good to go. However, in some cases there is
a bug you can fix right away and in others resource is just not suitable for
automated testing, such as the ones that require you to take a special action
that a Crossplane provider cannot, such as uploading a file.


Our goal is to make it work with automated testing as much as possible. So, the
next step is to test the resources manually in your local and try to spot the
problems that prevent it from working with the automated testing. The steps for
manual testing are roughly like the following (no Crossplane is needed):
* `kubectl apply -f package/crds` to install all CRDs into cluster.
* `make run` to start the controllers.
* You need to create a `ProviderConfig` named as `default` with correct
  credentials.
* Now, you can create the examples you've got generated and check events/logs to
  spot problems and fix them.

There are cases where the resource requires user to take an action that is not
possible with a Crossplane provider or automated testing tool. In such cases, we
should leave the actions to be taken as annotation on the resource like the
following:

```yaml
apiVersion: apigatewayv2.aws.upbound.io/v1beta1
kind: VPCLink
metadata:
  name: example
  annotations:
    upjet.upbound.io/manual-intervention: "User needs to upload a authorization script and give its path in spec.forProvider.filePath"
```

If, for some reason, we cannot successfully test a managed resource even manually, 
then we do not ship it with the `v1beta1` version and thus the external-name 
configuration should be commented out with an appropriate code comment 
explaining the situation.

An issue in the official-providers repo explaining the situation 
[should be opened](https://github.com/upbound/official-providers/issues/new/choose)
preferably with the example manifests (and any resource configuration) already tried.

As explained above, if the resource can successfully be manually tested but 
not as part of the automated tests, the example manifest successfully validated 
should still be included under the examples directory but with the proper 
`upjet.upbound.io/manual-intervention` annotation. 
And successful manual testing still meets the `v1beta1` criteria.

## External Name Cases

### Case 1: `name` As Identifier

There is a `name` argument under `Argument Reference` section and `Import`
section suggests to use `name` to import the resource.

Use `config.NameAsIdentifier`.

An example would be
[`aws_eks_cluster`](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/eks_cluster)
and
[here](https://github.com/upbound/provider-aws/blob/8b3887c91c4b44dc14e1123b3a5ae1a70e0e45ed/config/externalname.go#L284)
is its configuration.

### Case 2: Parameter As Identifier

There is an argument under `Argument Reference` section that is used like name,
i.e. `cluster_name` or `group_name`, and `Import` section suggests to use the
value of that argument to import the resource.

Use `config.ParameterAsIdentifier(<name of the argument parameter>)`.

An example would be
[`aws_elasticache_cluster`](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_cluster)
and
[here](https://github.com/upbound/provider-aws/blob/8b3887c91c4b44dc14e1123b3a5ae1a70e0e45ed/config/externalname.go#L299)
is its configuration.

### Case 3: Random Identifier From Provider

The ID used in `Import` section is completely random and assigned by provider,
like a UUID, where you don't have any means of impact on it.

Use `config.IdentifierFromProvider`.

An example would be
[`aws_vpc`](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc)
and
[here](https://github.com/upbound/provider-aws/blob/8b3887c91c4b44dc14e1123b3a5ae1a70e0e45ed/config/externalname.go#L155)
is its configuration.

### Case 4: Random Identifier Substring From Provider

The ID used in `Import` section is partially random and assigned by provider.
For example, a node in a cluster could have a random ID like `13213` but the
Terraform Identifier could include the name of the cluster that's represented as
an argument field under `Argument Reference`, i.e. `cluster-name:23123`. In that
case, we'll use only the randomly assigned part as external name and we need to
tell Upjet how to construct the full ID back and forth.

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
name and take the rest from parameters.

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

For `aws_glue_user_defined_function`, we see that `name` argument is used to
name the resource and the import instructions read as following:
```
Glue User Defined Functions can be imported using the
`catalog_id:database_name:function_name`. If you have not set a Catalog ID
specify the AWS Account ID that the database is in, e.g.,

$ terraform import aws_glue_user_defined_function.func 123456789012:my_database:my_func
```

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

For `azurerm_mariadb_firewall_rule`, we see that `name` argument is used to name
the resource and the import instructions read as following:
```
MariaDB Firewall rules can be imported using the resource id, e.g.

terraform import azurerm_mariadb_firewall_rule.rule1 /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/mygroup1/providers/Microsoft.DBforMariaDB/servers/server1/firewallRules/rule1
```

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

For `google_container_cluster`, we see that `name` argument is used to name the
resource and the import instructions read as following:
```console
GKE clusters can be imported using the project , location, and name.
If the project is omitted, the default provider value will be used.
Examples:

$ terraform import google_container_cluster.mycluster projects/my-gcp-project/locations/us-east1-a/clusters/my-cluster
$ terraform import google_container_cluster.mycluster my-gcp-project/us-east1-a/my-cluster
$ terraform import google_container_cluster.mycluster us-east1-a/my-cluster
```

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

There is no instructions under `Import` section of the resource page in
Terraform Registry, like `aws_acm_certificate_validation` from AWS.

Use the following in such cases with comment indicating the case:
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
[`aws_ecrpublic_repository_policy`](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecrpublic_repository_policy)
from AWS where the identifier is `repository_name`.

Use `config.IdentifierFromProvider` because in these cases `repository_name` is
more meaningful as an argument rather than the name of the policy for users,
hence we assume the ID is coming from provider.

### Case 8: Using Identifiers of Other Resources

There are resources that mostly represent a relation between two resources
without any particular name that identifies the relation. An example would be
[`azurerm_subnet_nat_gateway_association`](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet_nat_gateway_association)
where the ID is made up of two arguments `nat_gateway_id` and `subnet_id`
without any particular field used to give a name to the resource.

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
implementation [here][route-impl]. [This section][external-name-in-guide] in the
detailed guide could also help you.


[provider-guide]:
    https://github.com/upbound/upjet/blob/main/docs/generating-a-provider.md
[config-guide]:
    https://github.com/upbound/upjet/blob/main/docs/add-new-resource-long.md
[issue-90]:
    https://github.com/upbound/provider-aws/issues/90
[`aws_glue_workflow`]:
    https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/glue_workflow
[`aws_ecrpublic_repository_policy`]:
    https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecrpublic_repository_policy#import
[`aws_route`]:
    https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route
[route-impl]:
    https://github.com/upbound/provider-aws/blob/8b3887c91c4b44dc14e1123b3a5ae1a70e0e45ed/config/externalname.go#L172
[external-name-in-guide]:
    https://github.com/upbound/upjet/blob/main/docs/add-new-resource-long.md#external-name
[Moving Untested Resources to v1beta1]: https://github.com/upbound/upjet/blob/main/docs/moving-resources-to-v1beta1.md