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
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane-contrib/terrajet/pkg/config"
	"github.com/crossplane-contrib/terrajet/pkg/pipeline/templates"
)

// NewTerraformedGenerator returns a new TerraformedGenerator.
func NewTerraformedGenerator(pkg *types.Package, localDirectoryPath string) *TerraformedGenerator {
	return &TerraformedGenerator{
		LocalDirectoryPath: localDirectoryPath,
		pkg:                pkg,
	}
}

// TerraformedGenerator generates conversion methods implementing Terraformed
// interface on CRD structs.
type TerraformedGenerator struct {
	LocalDirectoryPath string

	pkg *types.Package
}

// Generate writes generated Terraformed interface functions
func (tg *TerraformedGenerator) Generate(c *config.Resource, sch *schema.Resource) error {
	trFile := wrapper.NewFile(tg.pkg.Path(), tg.pkg.Name(), templates.TerraformedTemplate,
		wrapper.WithGenStatement(GenStatement),
		wrapper.WithHeaderPath("hack/boilerplate.go.txt"), // todo
	)
	cfgPath := ""
	if c.ExternalName.ConfigureFunctionPath != "" {
		cfgPath = trFile.Imports.UseType(c.ExternalName.ConfigureFunctionPath)
	}
	vars := map[string]interface{}{
		"CRD": map[string]string{
			"APIVersion":            c.Version,
			"Kind":                  c.Kind,
			"ConfigureExternalName": cfgPath,
		},
		"Terraform": map[string]interface{}{
			// TODO(hasan): This identifier is used to generate external name.
			//  However, external-name generation is not as straightforward as
			//  just finding out the identifier field since Terraform uses
			//  more complex logic like combining multiple fields etc.
			//  I'll revisit this with
			//  https://github.com/crossplane-contrib/terrajet/issues/11
			"IdentifierField": c.TerraformIDFieldName,
			"ResourceType":    c.TerraformResourceType,
			"SchemaVersion":   sch.SchemaVersion,
		},
		"SensitiveFields": c.Sensitive.GetFieldPaths(),
		"LateInitializer": map[string]interface{}{
			"IgnoredFields": c.LateInitializer.IgnoredFields,
		},
	}

	filePath := filepath.Join(tg.LocalDirectoryPath, fmt.Sprintf("zz_%s_terraformed.go", strings.ToLower(c.Kind)))
	return errors.Wrap(
		trFile.Write(filePath, vars, os.ModePerm),
		"cannot write terraformed conversion methods file",
	)
}
