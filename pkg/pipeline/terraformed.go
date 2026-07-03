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

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/muvaf/typewriter/pkg/wrapper"

	"github.com/crossplane/upjet/v2/pkg/pipeline/templates"
)

// NewTerraformedGenerator returns a new TerraformedGenerator.
func NewTerraformedGenerator(pkg *types.Package, apiDir, hackDir, group, version string, o ...TerraformedGeneratorOption) *TerraformedGenerator {
	g := &TerraformedGenerator{
		LocalDirectoryPath: filepath.Join(apiDir, strings.ToLower(strings.Split(group, ".")[0]), version),
		LicenseHeaderPath:  filepath.Join(hackDir, "boilerplate.go.txt"),
		pkg:                pkg,
	}

	// apply the specified configuration options.
	for _, fn := range o {
		fn(g)
	}

	return g
}

// TerraformedGenerator generates conversion methods implementing Terraformed
// interface on CRD structs.
type TerraformedGenerator struct {
	LocalDirectoryPath string
	LicenseHeaderPath  string

	pkg                 *types.Package
	terraformedTemplate string
}

// A TerraformedGeneratorOption configures a TerraformedGenerator option.
type TerraformedGeneratorOption func(generator *TerraformedGenerator)

// WithTerraformedTemplate configures the terraformed template to be used.
func WithTerraformedTemplate(template string) TerraformedGeneratorOption {
	return func(g *TerraformedGenerator) {
		g.terraformedTemplate = template
	}
}

// Generate writes generated Terraformed interface functions
func (tg *TerraformedGenerator) Generate(cfgs []*terraformedInput, apiVersion string) error {
	tfTemplate := templateOrDefault(tg.terraformedTemplate, templates.TerraformedTemplate)

	for _, cfg := range cfgs {
		trFile := wrapper.NewFile(tg.pkg.Path(), tg.pkg.Name(), tfTemplate,
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
			"ResourceType":   cfg.Name,
			"ResourceSchema": cfg.TerraformResource,
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
