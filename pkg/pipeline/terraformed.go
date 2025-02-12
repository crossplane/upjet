// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/pipeline/templates"
)

// NewTerraformedGenerator returns a new TerraformedGenerator.
func NewTerraformedGenerator(pkg *types.Package, apiDir, hackDir, group, version string) *TerraformedGenerator {
	return &TerraformedGenerator{
		LocalDirectoryPath: filepath.Join(apiDir, strings.ToLower(strings.Split(group, ".")[0]), version),
		LicenseHeaderPath:  filepath.Join(hackDir, "boilerplate.go.txt"),
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
	for _, cfg := range cfgs {
		trFile := wrapper.NewFile(tg.pkg.Path(), tg.pkg.Name(), templates.TerraformedTemplate,
			wrapper.WithGenStatement(GenStatement),
			wrapper.WithHeaderPath(tg.LicenseHeaderPath),
		)
		filePath := filepath.Join(tg.LocalDirectoryPath, fmt.Sprintf("zz_%s_terraformed.go", strings.ToLower(cfg.Kind)))

		vars := map[string]any{
			"APIVersion": apiVersion,
		}
		vars["CRD"] = map[string]string{
			"Kind":               cfg.Kind,
			"ParametersTypeName": cfg.ParametersTypeName,
		}
		vars["Terraform"] = map[string]any{
			"ResourceType":  cfg.Name,
			"SchemaVersion": cfg.TerraformResource.SchemaVersion,
		}
		vars["Sensitive"] = map[string]any{
			"Fields": cfg.Sensitive.GetFieldPaths(),
		}
		vars["LateInitializer"] = map[string]any{
			"IgnoredFields":            cfg.LateInitializer.GetIgnoredCanonicalFields(),
			"ConditionalIgnoredFields": cfg.LateInitializer.GetConditionalIgnoredCanonicalFields(),
		}

		if err := trFile.Write(filePath, vars, os.ModePerm); err != nil {
			return errors.Wrapf(err, "cannot write the Terraformed interface implementation file %s", filePath)
		}
	}
	return nil
}
