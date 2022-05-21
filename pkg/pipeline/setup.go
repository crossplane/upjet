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
	"sort"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/pipeline/templates"
)

// NewSetupGenerator returns a new SetupGenerator.
func NewSetupGenerator(rootDir, modulePath string) *SetupGenerator {
	return &SetupGenerator{
		LocalDirectoryPath: filepath.Join(rootDir, "internal", "controller"),
		LicenseHeaderPath:  filepath.Join(rootDir, "hack", "boilerplate.go.txt"),
		ModulePath:         modulePath,
	}
}

// SetupGenerator generates controller setup file.
type SetupGenerator struct {
	LocalDirectoryPath string
	LicenseHeaderPath  string
	ModulePath         string
}

// Generate writes the setup file with the content produced using given
// list of version packages.
func (sg *SetupGenerator) Generate(versionPkgList []string) error {
	setupFile := wrapper.NewFile(filepath.Join(sg.ModulePath, "apis"), "apis", templates.SetupTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath(sg.LicenseHeaderPath),
	)
	sort.Strings(versionPkgList)
	aliases := make([]string, len(versionPkgList))
	for i, pkgPath := range versionPkgList {
		aliases[i] = setupFile.Imports.UsePackage(pkgPath)
	}
	vars := map[string]interface{}{
		"Aliases": aliases,
	}
	filePath := filepath.Join(sg.LocalDirectoryPath, "zz_setup.go")
	return errors.Wrap(setupFile.Write(filePath, vars, os.ModePerm), "cannot write setup file")
}
