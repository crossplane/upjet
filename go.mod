module github.com/crossplane-contrib/terrajet

go 1.16

require (
	github.com/crossplane/crossplane-runtime v0.15.1-0.20211004150827-579c1833b513
	github.com/fatih/camelcase v1.0.0
	github.com/golang/mock v1.6.0
	github.com/google/go-cmp v0.5.6
	github.com/hashicorp/terraform-plugin-sdk v1.17.2
	github.com/hashicorp/terraform-plugin-sdk/v2 v2.7.0
	github.com/iancoleman/strcase v0.2.0
	github.com/json-iterator/go v1.1.11
	github.com/muvaf/typewriter v0.0.0-20210910160850-80e49fe1eb32
	github.com/pkg/errors v0.9.1
	github.com/spf13/afero v1.6.0
	golang.org/x/tools v0.1.5
	k8s.io/api v0.21.3
	k8s.io/apimachinery v0.21.3
	sigs.k8s.io/controller-runtime v0.9.6
)

replace github.com/hashicorp/terraform-plugin-sdk => github.com/turkenh/terraform-plugin-sdk v1.17.2-patch1
