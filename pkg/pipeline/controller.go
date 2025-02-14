// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/pipeline/templates"
)

// NewControllerGenerator returns a new ControllerGenerator.
func NewControllerGenerator(ctrlDir, hackDir, ctrlModulePath, group string) *ControllerGenerator {
	return &ControllerGenerator{
		Group:              group,
		ControllerGroupDir: filepath.Join(ctrlDir, strings.Split(group, ".")[0]),
		ModulePath:         ctrlModulePath,
		LicenseHeaderPath:  filepath.Join(hackDir, "boilerplate.go.txt"),
	}
}

// ControllerGenerator generates controller setup functions.
type ControllerGenerator struct {
	Group              string
	ControllerGroupDir string
	ModulePath         string
	LicenseHeaderPath  string
}

// Generate writes controller setup functions.
func (cg *ControllerGenerator) Generate(cfg *config.Resource, typesPkgPath string, featuresPkgPath string) (pkgPath string, err error) {
	controllerPkgPath := filepath.Join(cg.ModulePath, strings.ToLower(strings.Split(cg.Group, ".")[0]), strings.ToLower(cfg.Kind))
	ctrlFile := wrapper.NewFile(controllerPkgPath, strings.ToLower(cfg.Kind), templates.ControllerTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath(cg.LicenseHeaderPath),
	)

	vars := map[string]any{
		"Package": strings.ToLower(cfg.Kind),
		"CRD": map[string]string{
			"Kind": cfg.Kind,
		},
		"DisableNameInitializer":            cfg.ExternalName.DisableNameInitializer,
		"TypePackageAlias":                  ctrlFile.Imports.UsePackage(typesPkgPath),
		"UseAsync":                          cfg.UseAsync,
		"UseTerraformPluginSDKClient":       cfg.ShouldUseTerraformPluginSDKClient(),
		"UseTerraformPluginFrameworkClient": cfg.ShouldUseTerraformPluginFrameworkClient(),
		"ResourceType":                      cfg.Name,
		"Initializers":                      cfg.InitializerFns,
	}

	// If the provider has a features package, add it to the controller template.
	// This is to ensure we don't break existing providers that don't have a
	// features package (yet).
	if featuresPkgPath != "" {
		vars["FeaturesPackageAlias"] = ctrlFile.Imports.UsePackage(featuresPkgPath)
	}

	filePath := filepath.Join(cg.ControllerGroupDir, strings.ToLower(cfg.Kind), "zz_controller.go")
	return controllerPkgPath, errors.Wrap(
		ctrlFile.Write(filePath, vars, os.ModePerm),
		"cannot write controller file",
	)
}
