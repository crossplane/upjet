/*
Copyright 2021 Upbound Inc.
*/

package pipeline

import (
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/upbound/upjet/pkg/pipeline/templates"
)

// NewVersionGenerator returns a new VersionGenerator.
func NewVersionGenerator(rootDir, modulePath, group, version string) *VersionGenerator {
	pkgPath := filepath.Join(modulePath, "apis", strings.ToLower(strings.Split(group, ".")[0]), version)
	return &VersionGenerator{
		Group:             group,
		Version:           version,
		DirectoryPath:     filepath.Join(rootDir, "apis", strings.ToLower(strings.Split(group, ".")[0]), version),
		LicenseHeaderPath: filepath.Join(rootDir, "hack", "boilerplate.go.txt"),
		pkg:               types.NewPackage(pkgPath, version),
	}
}

// VersionGenerator generates files for a version of a specific group.
type VersionGenerator struct {
	Group             string
	Version           string
	DirectoryPath     string
	LicenseHeaderPath string

	pkg *types.Package
}

// Generate writes doc and group version info files to the disk.
func (vg *VersionGenerator) Generate() error {
	vars := map[string]any{
		"CRD": map[string]string{
			"Version": vg.Version,
			"Group":   vg.Group,
		},
	}
	gviFile := wrapper.NewFile(vg.pkg.Path(), vg.Version, templates.GroupVersionInfoTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath(vg.LicenseHeaderPath),
	)
	return errors.Wrap(
		gviFile.Write(filepath.Join(vg.DirectoryPath, "zz_groupversion_info.go"), vars, os.ModePerm),
		"cannot write group version info file",
	)
}

// Package returns the package of the version that will be generated.
func (vg *VersionGenerator) Package() *types.Package {
	return vg.pkg
}
