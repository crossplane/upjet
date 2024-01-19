// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"

	"gopkg.in/alecthomas/kingpin.v2"
)

func main() {
	var (
		app                     = kingpin.New(filepath.Base(os.Args[0]), "Transformer for the generated resolvers by the crossplane-tools so that cross API-group imports are removed.").DefaultEnvars()
		apiGroupSuffix          = app.Flag("apiGroupSuffix", "Resource API group suffix, such as aws.upbound.io. The resource API group names are suffixed with this to get the canonical API group name.").Short('g').Required().String()
		pattern                 = app.Flag("pattern", "List patterns for the packages to process, such as ./apis/...").Short('p').Default("./apis/...").Strings()
		resolverFilePattern     = app.Flag("resolver", "Name of the generated resolver files to process.").Short('r').Default("zz_generated.resolvers.go").String()
		ignorePackageLoadErrors = app.Flag("ignoreLoadErrors", "Ignore errors encountered while loading the packages.").Short('s').Bool()
	)
	kingpin.MustParse(app.Parse(os.Args[1:]))
	kingpin.FatalIfError(transformPackages(*apiGroupSuffix, *resolverFilePattern, *ignorePackageLoadErrors, *pattern...), "Failed to transform the resolver files in the specified packages.")
}
