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
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/pipeline/templates"
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
	vars := map[string]interface{}{
		"CRD": map[string]string{
			"APIVersion": vg.Version,
			"Group":      vg.Group,
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
