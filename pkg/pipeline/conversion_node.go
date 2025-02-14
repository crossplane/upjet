// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/muvaf/typewriter/pkg/wrapper"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/config"
)

var (
	regexTypeFile = regexp.MustCompile(`zz_(.+)_types.go`)
)

// generationPredicate controls whether a resource configuration will be marked
// as a hub or spoke based on the API version of the resource file
// being considered.
type generationPredicate func(c *config.Resource, fileAPIVersion string) bool

// NewConversionNodeGenerator returns a new ConversionNodeGenerator.
func NewConversionNodeGenerator(apiDir, hackDir, apiModulePath, group, generatedFileName, fileTemplate string, p generationPredicate) *ConversionNodeGenerator {
	shortGroup := strings.ToLower(strings.Split(group, ".")[0])
	return &ConversionNodeGenerator{
		apiGroupModule:    filepath.Join(apiModulePath, shortGroup),
		apiGroupDir:       filepath.Join(apiDir, shortGroup),
		licenseHeaderPath: filepath.Join(hackDir, "boilerplate.go.txt"),
		nodeVersionsMap:   make(map[string][]string),
		generatedFileName: generatedFileName,
		fileTemplate:      fileTemplate,
		predicate:         p,
	}
}

// ConversionNodeGenerator generates conversion methods implementing the
// conversion.Convertible interface on the CRD structs.
type ConversionNodeGenerator struct {
	apiGroupModule    string
	apiGroupDir       string
	licenseHeaderPath string
	nodeVersionsMap   map[string][]string
	generatedFileName string
	fileTemplate      string
	predicate         generationPredicate
}

// Generate writes generated conversion.Convertible interface functions
func (cg *ConversionNodeGenerator) Generate(versionMap map[string]map[string]*config.Resource) error { //nolint:gocyclo
	entries, err := os.ReadDir(cg.apiGroupDir)
	if err != nil {
		return errors.Wrapf(err, "cannot list the directory entries for the source folder %s while generating the conversion functions", cg.apiGroupDir)
	}
	// iterate over the versions belonging to the API group
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		version := e.Name()
		convFile := wrapper.NewFile(filepath.Join(cg.apiGroupModule, version), version, cg.fileTemplate,
			wrapper.WithGenStatement(GenStatement),
			wrapper.WithHeaderPath(cg.licenseHeaderPath),
		)
		filePath := filepath.Join(cg.apiGroupDir, version, cg.generatedFileName)
		vars := map[string]any{
			"APIVersion": version,
		}

		versionDir := filepath.Join(cg.apiGroupDir, version)
		files, err := os.ReadDir(versionDir)
		if err != nil {
			return errors.Wrapf(err, "cannot list the directory entries for the source folder %s while looking for the generated types", versionDir)
		}
		var resources []map[string]any
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			m := regexTypeFile.FindStringSubmatch(f.Name())
			if len(m) < 2 {
				continue
			}
			c := findKindTerraformedInput(versionMap, m[1])
			if c == nil {
				// type may not be available in the new version =>
				// no conversion is possible.
				continue
			}
			// skip resource configurations that do not match the predicate
			if !cg.predicate(c, version) {
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
		if err := convFile.Write(filePath, vars, os.ModePerm); err != nil {
			return errors.Wrapf(err, "cannot write the generated conversion functions file %s", filePath)
		}
	}
	return nil
}

func findKindTerraformedInput(versionMap map[string]map[string]*config.Resource, name string) *config.Resource {
	for _, resources := range versionMap {
		for _, r := range resources {
			if strings.EqualFold(name, r.Kind) {
				return r
			}
		}
	}
	return nil
}
