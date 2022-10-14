## Auto Cross Resource Reference Generation

Cross Resource Referencing is one of the key concepts of the resource 
configuration. As a very common case, cloud services depend on other cloud 
services. For example, AWS Subnet resource needs an AWS VPC for creation. So, 
for creating a Subnet successfully, before you have to create a VPC resource. 
Please see the [Dependencies] documentation for more details. And also, for 
resource configuration-related parts of cross-resource referencing, please see 
[this part] of [Configuring a Resource] documentation.

These documentations focus on the general concepts and manual configurations
of Cross Resource References. However, the main topic of this documentation is
automatic example&reference generation.

Upjet has a scraper tool for scraping provider metadata from the Terraform
Registry. The scraped metadata are:
- Resource Descriptions
- Examples of Resources (in HCL format)
- Field Documentations
- Import Statements

These are very critical information for our automation processes. We use this
scraped metadata in many contexts. For example, field documentation of
resources and descriptions are used as Golang comments for schema fields and 
CRDs.

Another important scraped information is examples of resources. As a part
of testing efforts, finding the correct combination of field values is not easy
for every scenario. So, having a working example (combination) is very important
for easy testing.

At this point, this example that is in HCL format is converted to a Managed
Resource manifest, and we can use this manifest in our test efforts.

This is an example from Terraform Registry AWS Ebs Volume resource:

```
resource "aws_ebs_volume" "example" {
  availability_zone = "us-west-2a"
  size              = 40

  tags = {
    Name = "HelloWorld"
  }
}

resource "aws_ebs_snapshot" "example_snapshot" {
  volume_id = aws_ebs_volume.example.id

  tags = {
    Name = "HelloWorld_snap"
  }
}
```

The generated example:

```yaml
apiVersion: ec2.aws.upbound.io/v1beta1
kind: EBSSnapshot
metadata:
  annotations:
    meta.upbound.io/example-id: ec2/v1beta1/ebssnapshot
  labels:
    testing.upbound.io/example-name: example_snapshot
  name: example-snapshot
spec:
  forProvider:
    region: us-west-1
    tags:
      Name: HelloWorld_snap
    volumeIdSelector:
      matchLabels:
        testing.upbound.io/example-name: example

---

apiVersion: ec2.aws.upbound.io/v1beta1
kind: EBSVolume
metadata:
  annotations:
    meta.upbound.io/example-id: ec2/v1beta1/ebssnapshot
  labels:
    testing.upbound.io/example-name: example
  name: example
spec:
  forProvider:
    availabilityZone: us-west-2a
    region: us-west-1
    size: 40
    tags:
      Name: HelloWorld
```

Here, there are three very important points that scraper makes easy our life:

- We do not have to find the correct value combinations for fields. So, we can
  easily use the generated example manifest in our tests.
- The HCL example was scraped from registry documentation of the `aws_ebs_snapshot` 
  resource. In the example, you also see the `aws_ebs_volume` resource manifest 
  because, for the creation of an EBS Snapshot, you need an EBS Volume resource. 
  Thanks to the source Registry, (in many cases, there are the dependent resources 
  of target resources) we can also scrape the dependencies of target resources.
- The last item is actually what is intended to be explained in this document.
  For using the Cross Resource References, as I mentioned above, you need to add
  some references to the resource configuration. But, in many cases, if in the
  scraped example, the mentioned dependencies are already described you do not 
  have to write explicit references to resource configuration. The Cross Resource
  Reference generator generates the mentioned references.

### Validating the Cross Resource References

As I mentioned, many references are generated from scraped metadata by an auto
reference generator. However, there are two cases where we miss generating the
references.

The first one is related to some bugs or improvement points in the generator. 
This means that the generator can handle many references in the scraped 
examples and generate correctly them. But we cannot say that the ratio is %100. 
For some cases, the generator cannot generate references although, they are in 
the scraped example manifests.

The second one is related to the scraped example itself. As I mentioned above,
the source of the generator is the scraped example manifest. So, it checks the
manifest and tries to generate the found cross-resource references. In some
cases, although there are other reference fields, these do not exist in the
example manifest. They can only be mentioned in schema/field documentation.

For these types of situations, you must configure cross-resource references
explicitly.

### Removing Auto-Generated Cross Resource References In Some Corner Cases

In some cases, the generated references can narrow the reference pool covered by
the field. For example, X resource has an A field and Y and Z resources can be
referenced via this field. However, since the reference to Y is mentioned in the
example manifest, this reference field will only be defined over Y. In this case,
since the reference pool of the relevant field will be narrowed, it would be
more appropriate to delete this reference. For example,

```
resource "aws_route53_record" "www" {
  zone_id = aws_route53_zone.primary.zone_id
  name    = "example.com"
  type    = "A"

  alias {
    name                   = aws_elb.main.dns_name
    zone_id                = aws_elb.main.zone_id
    evaluate_target_health = true
  }
}
```

Route53 Record resource’s alias.name field has a reference. In the example, this
reference is shown by using the `aws_elb` resource. However, when we check the
field documentation, we see that this field can also be used for reference
for other resources:

```
Alias
Alias records support the following:

name - (Required) DNS domain name for a CloudFront distribution, S3 bucket, ELB, 
or another resource record set in this hosted zone.
```

### Conclusion

As a result, mentioned scraper and example&reference generators are very useful 
for making easy the test efforts. But using these functionalities, we must be
careful to avoid undesired states.

[Dependencies]: https://crossplane.io/docs/v1.7/concepts/managed-resources.html#dependencies
[this part]: https://github.com/upbound/upjet/blob/main/docs/configuring-a-resource.md#cross-resource-referencing
[Configuring a Resource]: https://github.com/upbound/upjet/blob/main/docs/configuring-a-resource.md
