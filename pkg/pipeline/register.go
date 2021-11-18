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

	"github.com/crossplane-contrib/terrajet/pkg/pipeline/templates"
)

// NewRegisterGenerator returns a new RegisterGenerator.
func NewRegisterGenerator(rootDir, modulePath string) *RegisterGenerator {
	return &RegisterGenerator{
		LocalDirectoryPath: filepath.Join(rootDir, "apis"),
		LicenseHeaderPath:  filepath.Join(rootDir, "hack", "boilerplate.go.txt"),
		ModulePath:         modulePath,
	}
}

// RegisterGenerator generates scheme registration file.
type RegisterGenerator struct {
	LocalDirectoryPath string
	ModulePath         string
	LicenseHeaderPath  string
}

// Generate writes the register file with the content produced using given
// list of version packages.
func (rg *RegisterGenerator) Generate(versionPkgList []string) error {
	registerFile := wrapper.NewFile(filepath.Join(rg.ModulePath, "apis"), "apis", templates.RegisterTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath(rg.LicenseHeaderPath),
	)
	sort.Strings(versionPkgList)
	aliases := make([]string, len(versionPkgList))
	for i, pkgPath := range versionPkgList {
		aliases[i] = registerFile.Imports.UsePackage(pkgPath)
	}
	vars := map[string]interface{}{
		"Aliases": aliases,
	}
	filePath := filepath.Join(rg.LocalDirectoryPath, "zz_register.go")
	return errors.Wrap(registerFile.Write(filePath, vars, os.ModePerm), "cannot write register file")
}
