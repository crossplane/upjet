/*
Copyright 2021 Upbound Inc.
*/

package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/template"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/pipeline/templates"
)

// NewSetupGenerator returns a new SetupGenerator.
func NewSetupGenerator(rootDir, modulePath string) *SetupGenerator {
	return &SetupGenerator{
		ProviderPath:       filepath.Join(rootDir, "cmd", "provider"),
		LocalDirectoryPath: filepath.Join(rootDir, "internal", "controller"),
		LicenseHeaderPath:  filepath.Join(rootDir, "hack", "boilerplate.go.txt"),
		ModulePath:         modulePath,
	}
}

// SetupGenerator generates controller setup file.
type SetupGenerator struct {
	ProviderPath       string
	LocalDirectoryPath string
	LicenseHeaderPath  string
	ModulePath         string
}

// Generate writes the setup file with the content produced using given
// list of version packages.

func (sg *SetupGenerator) Generate(versionPkgMap map[string][]string, mainTemplate string) error {
	t, err := template.New("main").Parse(mainTemplate)
	if err != nil {
		return errors.Wrap(err, "failed to parse provider main program template")
	}
	for g, versionPkgList := range versionPkgMap {
		if err := sg.generate(g, versionPkgList); err != nil {
			return errors.Wrapf(err, "failed to generate controller setup file for group: %s", g)
		}
		if err := writeMainProgram(sg.ProviderPath, g, t); err != nil {
			return errors.Wrapf(err, "failed to write main program for group: %s", g)
		}
	}
	return nil
}

func writeMainProgram(providerPath, group string, t *template.Template) error {
	f := filepath.Join(providerPath, group)
	if err := os.MkdirAll(f, 0755); err != nil {
		return errors.Wrapf(err, "failed to mkdir provider main program path: %s", f)
	}
	m, err := os.OpenFile(filepath.Join(f, "main.go"), os.O_WRONLY|os.O_CREATE, 0755)
	if err != nil {
		return errors.Wrap(err, "failed to open provider main program file")
	}
	defer m.Close()
	if err := t.Execute(m, map[string]any{
		"Group": group,
	}); err != nil {
		return errors.Wrap(err, "failed to execute provider main program template")
	}
	return nil
}

func (sg *SetupGenerator) generate(group string, versionPkgList []string) error {
	setupFile := wrapper.NewFile(filepath.Join(sg.ModulePath, "apis"), "apis", templates.SetupTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath(sg.LicenseHeaderPath),
	)
	sort.Strings(versionPkgList)
	aliases := make([]string, len(versionPkgList))
	for i, pkgPath := range versionPkgList {
		aliases[i] = setupFile.Imports.UsePackage(pkgPath)
	}
	vars := map[string]any{
		"Aliases": aliases,
		"Group":   group,
	}
	filePath := filepath.Join(sg.LocalDirectoryPath, fmt.Sprintf("zz_%s_setup.go", group))
	return errors.Wrap(setupFile.Write(filePath, vars, os.ModePerm), "cannot write setup file")
}
