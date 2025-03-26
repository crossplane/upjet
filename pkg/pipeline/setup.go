// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"text/template"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/pipeline/templates"
)

// NewSetupGenerator returns a new generator that sets up controllers.
func NewSetupGenerator(ctrlDir, hackDir, apiModulePath string) *SetupGenerator {
	return &SetupGenerator{
		LocalDirectoryPath: ctrlDir,
		LicenseHeaderPath:  filepath.Join(hackDir, "boilerplate.go.txt"),
		ModulePath:         apiModulePath,
	}
}

// SetupGenerator generates controller setup file.
type SetupGenerator struct {
	LocalDirectoryPath string
	LicenseHeaderPath  string
	ModulePath         string
}

// Generate writes the setup file given list of version packages.
func (sg *SetupGenerator) Generate(versionPkgMap map[string][]string, monolith bool) error {
	if monolith {
		return errors.Wrap(sg.generate("", versionPkgMap[config.PackageNameMonolith]), "failed to generate the controller setup file")
	}

	for group, versionPkgList := range versionPkgMap {
		if err := sg.generate(group, versionPkgList); err != nil {
			return errors.Wrapf(err, "failed to generate the controller setup file for group: %s", group)
		}
	}
	return nil
}

func (sg *SetupGenerator) generate(group string, versionPkgList []string) error {
	// TODO(negz): Should this really be apis? They're not imported for setup...
	setupFile := wrapper.NewFile(sg.ModulePath, filepath.Base(sg.ModulePath), templates.SetupTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath(sg.LicenseHeaderPath),
	)
	sort.Strings(versionPkgList)
	aliases := make([]string, len(versionPkgList))
	for i, pkgPath := range versionPkgList {
		aliases[i] = setupFile.Imports.UsePackage(pkgPath)
	}
	g := ""
	filePath := filepath.Join(sg.LocalDirectoryPath, "zz_setup.go")
	if group != "" {
		filePath = filepath.Join(sg.LocalDirectoryPath, fmt.Sprintf("zz_%s_setup.go", group))
		g = "_" + group
	}
	vars := map[string]any{
		"Aliases": aliases,
		"Group":   g,
	}
	if err := setupFile.Write(filePath, vars, os.ModePerm); err != nil {
		return errors.Wrap(err, "cannot write setup file")
	}
	return nil
}

func NewMainGenerator(cmdDir, template string) *MainGenerator {
	return &MainGenerator{ProviderPath: cmdDir, Template: template}
}

type MainGenerator struct {
	ProviderPath string
	Template     string
}

// Generate writes the setup file given list of version packages.
func (mg *MainGenerator) Generate(groups []string) error {
	t, err := template.New("main").Parse(mg.Template)
	if err != nil {
		return errors.Wrap(err, "failed to parse the provider main program template")
	}

	for _, g := range groups {
		f := filepath.Join(mg.ProviderPath, g)
		if err := os.MkdirAll(f, 0o750); err != nil {
			return errors.Wrapf(err, "failed to mkdir provider main program path: %s", f)
		}
		m, err := os.OpenFile(filepath.Join(filepath.Clean(f), "zz_main.go"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return errors.Wrap(err, "failed to open provider main program file")
		}
		defer func() {
			if err := m.Close(); err != nil {
				log.Fatalf("Failed to close the templated main %q: %s", f, err.Error())
			}
		}()
		if err := t.Execute(m, map[string]any{
			"Group": g,
		}); err != nil {
			return errors.Wrap(err, "failed to execute provider main program template")
		}
	}
	return nil
}
