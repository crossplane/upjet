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

	"github.com/muvaf/typewriter/pkg/packages"
	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/pipeline/templates"
)

func NewVersionGenerator(rootPath, group, version string) *VersionGenerator {
	gg := &VersionGenerator{
		RootPath: rootPath,
		Group:    group,
		Version:  version,
		cache:    packages.NewCache(),
	}
	// todo: accept cache as option
	return gg
}

type VersionGenerator struct {
	RootPath string
	Group    string
	Version  string

	cache *packages.Cache
}

func (vg *VersionGenerator) Generate() error {
	vars := map[string]interface{}{
		"CRD": map[string]string{
			"APIVersion": vg.Version,
			"Group":      vg.Group,
		},
	}
	pkgPath := filepath.Join(
		vg.RootPath,
		"apis",
		strings.ToLower(strings.Split(vg.Group, ".")[0]),
		strings.ToLower(vg.Version),
	)
	gviFile := wrapper.NewFile(pkgPath, vg.Version, templates.GroupVersionInfoTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath("hack/boilerplate.go.txt"), // todo
	)
	err := gviFile.Write(filepath.Join(pkgPath, fmt.Sprintf("zz_groupversion_info.go")), vars, os.FileMode(0o664))
	if err != nil {
		return errors.Wrap(err, "cannot write group version info file")
	}
	docFile := wrapper.NewFile(pkgPath, vg.Version, templates.DocTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath("hack/boilerplate.go.txt"), // todo
	)
	err = docFile.Write(filepath.Join(pkgPath, fmt.Sprintf("zz_doc.go")), vars, os.FileMode(0o664))
	if err != nil {
		return errors.Wrap(err, "cannot write doc file")
	}
	return nil
}
