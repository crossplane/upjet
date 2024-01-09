// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/pipeline/templates"
)

// NewConversionHubGenerator returns a new ConversionHubGenerator.
func NewConversionHubGenerator(pkg *types.Package, rootDir, group, version string) *ConversionHubGenerator {
	return &ConversionHubGenerator{
		LocalDirectoryPath: filepath.Join(rootDir, "apis", strings.ToLower(strings.Split(group, ".")[0]), version),
		LicenseHeaderPath:  filepath.Join(rootDir, "hack", "boilerplate.go.txt"),
		pkg:                pkg,
	}
}

// ConversionHubGenerator generates conversion methods implementing the
// conversion.Hub interface on the CRD structs.
type ConversionHubGenerator struct {
	LocalDirectoryPath string
	LicenseHeaderPath  string

	pkg *types.Package
}

// Generate writes generated conversion.Hub interface functions
func (cg *ConversionHubGenerator) Generate(cfgs []*terraformedInput, apiVersion string) error {
	trFile := wrapper.NewFile(cg.pkg.Path(), cg.pkg.Name(), templates.ConversionHubTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath(cg.LicenseHeaderPath),
	)
	filePath := filepath.Join(cg.LocalDirectoryPath, "zz_generated.conversion_hubs.go")
	vars := map[string]any{
		"APIVersion": apiVersion,
	}
	resources := make([]map[string]any, len(cfgs))
	index := 0
	for _, cfg := range cfgs {
		resources[index] = map[string]any{
			"CRD": map[string]string{
				"Kind": cfg.Kind,
			},
		}
		index++
	}
	vars["Resources"] = resources
	if len(resources) == 0 {
		return nil
	}
	return errors.Wrapf(
		trFile.Write(filePath, vars, os.ModePerm),
		"cannot write the generated conversion Hub functions file %s", filePath,
	)
}
