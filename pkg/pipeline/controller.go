// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/muvaf/typewriter/pkg/wrapper"

	"github.com/crossplane/upjet/v2/pkg/config"
)

// NewControllerGenerator returns a new ControllerGenerator.
func NewControllerGenerator(ctrlDir, hackDir, ctrlModulePath, group string, o ...ControllerGeneratorOption) *ControllerGenerator {
	g := &ControllerGenerator{
		Group:              group,
		ControllerGroupDir: filepath.Join(ctrlDir, strings.Split(group, ".")[0]),
		ModulePath:         ctrlModulePath,
		LicenseHeaderPath:  filepath.Join(hackDir, "boilerplate.go.txt"),
	}

	// apply the specified configuration options.
	for _, fn := range o {
		fn(g)
	}

	return g
}

// ControllerGenerator generates controller setup functions.
type ControllerGenerator struct {
	Group              string
	ControllerGroupDir string
	ModulePath         string
	LicenseHeaderPath  string

	controllerTemplate string
}

// A ControllerGeneratorOption configures a ControllerGenerator option.
type ControllerGeneratorOption func(*ControllerGenerator)

func WithControllerTemplate(template string) ControllerGeneratorOption {
	return func(g *ControllerGenerator) {
		g.controllerTemplate = template
	}
}

// Generate writes controller setup functions.
func (cg *ControllerGenerator) Generate(cfg *config.Resource, typesPkgPath string, featuresPkgPath string) (pkgPath string, err error) {
	controllerPkgPath := filepath.Join(cg.ModulePath, strings.ToLower(strings.Split(cg.Group, ".")[0]), strings.ToLower(cfg.Kind))
	ctrlFile := wrapper.NewFile(controllerPkgPath, strings.ToLower(cfg.Kind), cg.controllerTemplate,
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
