/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pipeline

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/pipeline/templates"
)

// NewControllerGenerator returns a new ControllerGenerator.
func NewControllerGenerator(groupDir, controllerGroupDir, rootModulePath, group, providerConfigBuilderPath string) *ControllerGenerator {
	return &ControllerGenerator{
		Group:                     group,
		GroupDir:                  groupDir,
		ControllerGroupDir:        controllerGroupDir,
		RootModulePath:            rootModulePath,
		ProviderConfigBuilderPath: providerConfigBuilderPath,
	}
}

// ControllerGenerator generates controller setup functions.
type ControllerGenerator struct {
	Group                     string
	GroupDir                  string
	ControllerGroupDir        string
	RootModulePath            string
	ProviderConfigBuilderPath string
}

// Generate writes controller setup functions.
func (tg *ControllerGenerator) Generate(version, kind string) error {
	groupPkgPath := filepath.Join(tg.RootModulePath, "apis", strings.ToLower(strings.Split(tg.Group, ".")[0]), strings.ToLower(version))
	kindPkgPath := filepath.Join(groupPkgPath, strings.ToLower(kind))

	pkgPath := filepath.Join(tg.ControllerGroupDir, strings.ToLower(kind))
	ctrlFile := wrapper.NewFile(pkgPath, version, templates.ControllerTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath("hack/boilerplate.go.txt"), // todo
		wrapper.LinterEnabled(),
	)

	vars := map[string]interface{}{
		"Package": strings.ToLower(kind),
		"CRD": map[string]string{
			"APIVersion": version,
			"Kind":       kind,
		},
		"TypePackageAlias":                  ctrlFile.Imports.UsePackage(kindPkgPath),
		"ProviderConfigBuilderPackageAlias": ctrlFile.Imports.UsePackage(tg.ProviderConfigBuilderPath),
	}

	filePath := filepath.Join(tg.ControllerGroupDir, strings.ToLower(kind), "zz_controller.go")
	return errors.Wrap(
		ctrlFile.Write(filePath, vars, os.ModePerm),
		"cannot write controller file",
	)
}
