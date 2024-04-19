// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"
)

var (
	regexTypeFile = regexp.MustCompile(`zz_(.+)_types.go`)
)

// generationPredicate controls whether a resource configuration will be marked
// as a hub or spoke based on the API version of the resource file
// being considered.
type generationPredicate func(c *terraformedInput, fileAPIVersion string) bool

// NewConversionNodeGenerator returns a new ConversionNodeGenerator.
func NewConversionNodeGenerator(pkg *types.Package, rootDir, group, generatedFileName, fileTemplate string, p generationPredicate) *ConversionNodeGenerator {
	return &ConversionNodeGenerator{
		localDirectoryPath: filepath.Join(rootDir, "apis", strings.ToLower(strings.Split(group, ".")[0])),
		licenseHeaderPath:  filepath.Join(rootDir, "hack", "boilerplate.go.txt"),
		nodeVersionsMap:    make(map[string][]string),
		pkg:                pkg,
		generatedFileName:  generatedFileName,
		fileTemplate:       fileTemplate,
		predicate:          p,
	}
}

// ConversionNodeGenerator generates conversion methods implementing the
// conversion.Convertible interface on the CRD structs.
type ConversionNodeGenerator struct {
	localDirectoryPath string
	licenseHeaderPath  string
	nodeVersionsMap    map[string][]string
	pkg                *types.Package
	generatedFileName  string
	fileTemplate       string
	predicate          generationPredicate
}

// Generate writes generated conversion.Convertible interface functions
func (cg *ConversionNodeGenerator) Generate(cfgs []*terraformedInput) error { //nolint:gocyclo
	entries, err := os.ReadDir(cg.localDirectoryPath)
	if err != nil {
		return errors.Wrapf(err, "cannot list the directory entries for the source folder %s while generating the conversion.Convertible interface functions", cg.localDirectoryPath)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		trFile := wrapper.NewFile(cg.pkg.Path(), cg.pkg.Name(), cg.fileTemplate,
			wrapper.WithGenStatement(GenStatement),
			wrapper.WithHeaderPath(cg.licenseHeaderPath),
		)
		filePath := filepath.Join(cg.localDirectoryPath, e.Name(), cg.generatedFileName)
		vars := map[string]any{
			"APIVersion": e.Name(),
		}

		var resources []map[string]any
		versionDir := filepath.Join(cg.localDirectoryPath, e.Name())
		files, err := os.ReadDir(versionDir)
		if err != nil {
			return errors.Wrapf(err, "cannot list the directory entries for the source folder %s while looking for the generated types", versionDir)
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			m := regexTypeFile.FindStringSubmatch(f.Name())
			if len(m) < 2 {
				continue
			}
			c := findKindTerraformedInput(cfgs, m[1])
			if c == nil {
				// type may not be available in the new version =>
				// no conversion is possible.
				continue
			}
			// skip resource configurations that do not match the predicate
			if !cg.predicate(c, e.Name()) {
				continue
			}
			resources = append(resources, map[string]any{
				"CRD": map[string]string{
					"Kind": c.Kind,
				},
			})
			sk := fmt.Sprintf("%s.%s", c.ShortGroup, c.Kind)
			cg.nodeVersionsMap[sk] = append(cg.nodeVersionsMap[sk], filepath.Base(versionDir))
		}

		vars["Resources"] = resources
		if len(resources) == 0 {
			continue
		}
		if err := trFile.Write(filePath, vars, os.ModePerm); err != nil {
			return errors.Wrapf(err, "cannot write the generated conversion Hub functions file %s", filePath)
		}
	}
	return nil
}

func findKindTerraformedInput(cfgs []*terraformedInput, name string) *terraformedInput {
	for _, c := range cfgs {
		if strings.EqualFold(name, c.Kind) {
			return c
		}
	}
	return nil
}
