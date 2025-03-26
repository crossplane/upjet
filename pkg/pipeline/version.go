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

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"
	"golang.org/x/tools/go/packages"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/pipeline/templates"
)

// NewVersionGenerator returns a new VersionGenerator.
func NewVersionGenerator(apiDir, hackDir, apisModulePath, group, version string) *VersionGenerator {
	pkgPath := filepath.Join(apisModulePath, strings.ToLower(strings.Split(group, ".")[0]), version)
	return &VersionGenerator{
		Group:             group,
		Version:           version,
		DirectoryPath:     filepath.Join(apiDir, strings.ToLower(strings.Split(group, ".")[0]), version),
		LicenseHeaderPath: filepath.Join(hackDir, "boilerplate.go.txt"),
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

// InsertPreviousObjects inserts into this VersionGenerator's package scope all
// the type definitions from the previous versions of the managed resource APIs
// found in the Go package.
func (vg *VersionGenerator) InsertPreviousObjects(versions map[string]map[string]*config.Resource) error {
	for _, resources := range versions {
		for _, r := range resources {
			for _, v := range r.PreviousVersions {
				if vg.Version != v {
					// if this previous version v is not the version we are currently
					// processing
					continue
				}
				pkgs, err := packages.Load(&packages.Config{
					Mode: packages.NeedTypes,
					Dir:  vg.DirectoryPath,
				}, fmt.Sprintf("zz_%s_types.go", strings.ToLower(r.Kind)))
				if err != nil {
					return errors.Wrapf(err, "cannot load the previous versions of %q from path %s", r.Name, vg.pkg.Path())
				}
				for _, p := range pkgs {
					if p.Types == nil || p.Types.Scope() == nil {
						continue
					}
					for _, n := range p.Types.Scope().Names() {
						vg.pkg.Scope().Insert(p.Types.Scope().Lookup(n))
					}
				}
			}
		}
	}
	return nil
}
