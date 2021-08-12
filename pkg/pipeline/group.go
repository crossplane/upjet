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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/pipeline/templates"
	"github.com/muvaf/typewriter/pkg/packages"
	"github.com/muvaf/typewriter/pkg/wrapper"
)

func NewVersionGenerator(rootPath, group, version string) *VersionGenerator {
	gg := &VersionGenerator{
		RootPath: rootPath,
		Group:    group,
		Version:  version,
		cache:    packages.NewCache(),
	}
	// todo: accept cache as
	return gg
}

type VersionGenerator struct {
	RootPath string
	Group    string
	Version  string

	cache *packages.Cache
}

func (vg *VersionGenerator) Generate() error {
	pkgPath := filepath.Join(vg.RootPath, "apis", strings.ToLower(strings.Split(vg.Group, ".")[0]), strings.ToLower(vg.Version))
	pkg, err := vg.cache.GetPackage(pkgPath)
	if err != nil {
		return errors.Wrap(err, "cannot get package")
	}
	file := wrapper.NewFile(pkg.PkgPath, pkg.Name, templates.GroupVersionInfoTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath("hack/boilerplate.go.txt"), // todo
	)
	vars := map[string]interface{}{
		"CRD": map[string]string{
			"APIVersion": vg.Version,
			"Group":      vg.Group,
		},
	}
	data, err := file.Wrap(vars)
	if err != nil {
		return errors.Wrap(err, "cannot wrap file")
	}
	if err := os.MkdirAll(pkgPath, os.ModeDir); err != nil {
		return errors.Wrap(err, "cannot create directory for version")
	}
	filePath := filepath.Join(pkgPath, fmt.Sprintf("zz_groupversion_info.go"))
	return errors.Wrap(os.WriteFile(filePath, data, os.FileMode(0o664)), "cannot write groupversion_info file")
}
