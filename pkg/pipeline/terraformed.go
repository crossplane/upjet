/*
Copyright 2021 Upbound Inc.
*/

package pipeline

import (
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/pipeline/templates"
)

// NewTerraformedGenerator returns a new TerraformedGenerator.
func NewTerraformedGenerator(pkg *types.Package, rootDir, group, version string) *TerraformedGenerator {
	return &TerraformedGenerator{
		LocalDirectoryPath: filepath.Join(rootDir, "apis", strings.ToLower(strings.Split(group, ".")[0]), version),
		LicenseHeaderPath:  filepath.Join(rootDir, "hack", "boilerplate.go.txt"),
		pkg:                pkg,
	}
}

// TerraformedGenerator generates conversion methods implementing Terraformed
// interface on CRD structs.
type TerraformedGenerator struct {
	LocalDirectoryPath string
	LicenseHeaderPath  string

	pkg *types.Package
}

// Generate writes generated Terraformed interface functions
func (tg *TerraformedGenerator) Generate(cfgs []*terraformedInput, apiVersion string) error {
	trFile := wrapper.NewFile(tg.pkg.Path(), tg.pkg.Name(), templates.TerraformedTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath(tg.LicenseHeaderPath),
	)
	filePath := filepath.Join(tg.LocalDirectoryPath, "zz_generated_terraformed.go")
	vars := map[string]any{
		"APIVersion": apiVersion,
	}
	resources := make([]map[string]any, len(cfgs))
	index := 0
	for _, cfg := range cfgs {
		resources[index] = map[string]any{
			"CRD": map[string]string{
				"Kind":               cfg.Kind,
				"ParametersTypeName": cfg.ParametersTypeName,
			},
			"Terraform": map[string]any{
				"ResourceType":  cfg.Name,
				"SchemaVersion": cfg.TerraformResource.SchemaVersion,
			},
			"Sensitive": map[string]any{
				"Fields": cfg.Sensitive.GetFieldPaths(),
			},
			"LateInitializer": map[string]any{
				"IgnoredFields": cfg.LateInitializer.GetIgnoredCanonicalFields(),
			},
		}
		index++
	}
	vars["Resources"] = resources
	return errors.Wrap(
		trFile.Write(filePath, vars, os.ModePerm),
		"cannot write terraformed conversion methods file",
	)
}
