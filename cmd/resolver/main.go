// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/spf13/afero"
	"gopkg.in/alecthomas/kingpin.v2"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/crossplane/upjet/pkg/transformers"
)

func main() {
	var (
		app                     = kingpin.New(filepath.Base(os.Args[0]), "Transformer for the generated resolvers by the crossplane-tools so that cross API-group imports are removed.").DefaultEnvars()
		apiGroupSuffix          = app.Flag("apiGroupSuffix", "Resource API group suffix, such as aws.upbound.io. The resource API group names are suffixed with this to get the canonical API group name.").Short('g').Required().String()
		apiGroupOverride        = app.Flag("apiGroupOverride", "API group overrides").Short('o').StringMap()
		apiResolverPackage      = app.Flag("apiResolverPackage", "The package that contains the implementation for the GetManagedResource function, such as github.com/upbound/provider-aws/internal/apis.").Short('a').Required().String()
		pattern                 = app.Flag("pattern", "List patterns for the packages to process, such as ./...").Short('p').Default("./...").Strings()
		resolverFilePattern     = app.Flag("resolver", "Name of the generated resolver files to process.").Short('r').Default("zz_generated.resolvers.go").String()
		ignorePackageLoadErrors = app.Flag("ignoreLoadErrors", "Ignore errors encountered while loading the packages.").Short('s').Bool()
	)
	kingpin.MustParse(app.Parse(os.Args[1:]))
	logger := logging.NewLogrLogger(zap.New().WithName("transformer-resolver"))
	r := transformers.NewResolver(afero.NewOsFs(), *apiGroupSuffix, *apiResolverPackage, *ignorePackageLoadErrors, logger, transformers.WithAPIGroupOverrides(*apiGroupOverride))
	kingpin.FatalIfError(r.TransformPackages(*resolverFilePattern, *pattern...), "Failed to transform the resolver files in the specified packages.")
}
