// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/pipeline/templates"
)

// NewRegisterGenerator returns a new RegisterGenerator.
func NewRegisterGenerator(apiDir, hackDir, apiModulePath string) *RegisterGenerator {
	return &RegisterGenerator{
		LocalDirectoryPath: apiDir,
		LicenseHeaderPath:  filepath.Join(hackDir, "boilerplate.go.txt"),
		ModulePath:         apiModulePath,
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
	registerFile := wrapper.NewFile(rg.ModulePath, "apis", templates.RegisterTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath(rg.LicenseHeaderPath),
	)
	slices.Sort(versionPkgList)
	versionPkgList = slices.Compact(versionPkgList)
	aliases := make([]string, len(versionPkgList))
	for i, pkgPath := range versionPkgList {
		aliases[i] = registerFile.Imports.UsePackage(pkgPath)
	}
	vars := map[string]any{
		"Aliases": aliases,
	}
	filePath := filepath.Join(rg.LocalDirectoryPath, "zz_register.go")
	return errors.Wrap(registerFile.Write(filePath, vars, os.ModePerm), "cannot write register file")
}
