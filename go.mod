module github.com/crossplane-contrib/terrajet

go 1.16

require (
	github.com/crossplane/crossplane-runtime v0.15.0
	github.com/google/go-cmp v0.5.6
	github.com/hashicorp/hcl/v2 v2.8.2 // indirect
	github.com/hashicorp/terraform-plugin-sdk/v2 v2.7.0
	github.com/iancoleman/strcase v0.2.0
	github.com/json-iterator/go v1.1.11
	github.com/muvaf/typewriter v0.0.0-20210818141336-01a132960eec
	github.com/pkg/errors v0.9.1
	github.com/spf13/afero v1.6.0
	go.uber.org/multierr v1.7.0
	k8s.io/apimachinery v0.21.2
	k8s.io/client-go v0.21.2
	sigs.k8s.io/controller-runtime v0.9.2
	sigs.k8s.io/controller-tools v0.2.4
)
