## Moving Untested Resources to v1beta1

For outside contributors, we wanted to form a baseline for resource test
efforts. Therefore, we created a map: `ExternalNameNotTestedConfigs`. This map
contains the external name configurations of resources, but they were not tested.
And also, the resources’ schemas and controllers will not be generated after
running `make generate`/`make reviewable` commands.

For the generation of this resource’s schema and controller, we need to add it to
the `ExternalNameConfigs` map. After this addition, this resource’s schema and
the controller will be started to generate. By default, every resource that was 
added to this map will be generated in the `v1beta1` version.

Here there are two important points. For starting to test efforts, you need a
generated CRD and controller. And for this generation, you need to move your
resource to the `ExternalNameConfigs` map. Then you can start testing and if the
test effort is successful, the new entry can remain on the main map. However, if
there are some problems in tests, and you cannot validate the resource please
move the entry to `ExternalNameNotTestedConfigs` again.
