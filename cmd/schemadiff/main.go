// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kingpin/v2"
	"github.com/upbound/uptest/pkg/crdschema"
)

var (
	app = kingpin.New(filepath.Base(os.Args[0]), "CRD Schema Diff JSON File Generator").DefaultEnvars()
)

var (
	crdDir = app.Flag("crd-dir", "The directory of base CRDs").Short('i').Default("./package/crds").ExistingDir()
	out    = app.Flag("out", "Filename for JSON output").Short('o').Default("./config/crd-schema-changes.json").String()
)

// main is the entry point for the schemadiff tool.
// It processes all CRD files in the specified directory, detects schema changes
// between API versions, and outputs a JSON report.
func main() { //nolint:gocyclo // easier to follow as a unit
	kingpin.MustParse(app.Parse(os.Args[1:]))
	if crdDir == nil || *crdDir == "" {
		kingpin.Fatalf("base CRDs directory required")
	}
	if out == nil || *out == "" {
		kingpin.Fatalf("output directory file")
	}

	// List all YAML/YML files in the CRD directory
	crdFilePaths, err := listYAMLFiles(*crdDir)
	if err != nil {
		kingpin.FatalIfError(err, "cannot read CRD files")
	}

	// Configure the schema diff engine
	// EnableUpjetExtensions=false means we only analyze standard Kubernetes CRD schemas
	// without considering upjet-specific extensions (x-kubernetes-* annotations, etc.)
	opts := &crdschema.CommonOptions{
		EnableUpjetExtensions: false,
	}

	// jsonData will hold all change reports, keyed by "{group}/{kind}"
	// Example key: "ec2.aws.upbound.io/VPC"
	jsonData := map[string]*crdschema.ChangeReport{}

	// Process each CRD file
	for _, cfp := range crdFilePaths {
		// Create a self-diff analyzer for this CRD
		// "Self-diff" means comparing different versions within the same CRD file
		// (e.g., v1beta1 vs v1beta2 in the same CRD)
		sd, err := crdschema.NewSelfDiff(cfp, crdschema.WithSelfDiffCommonOptions(opts))
		if err != nil {
			kingpin.FatalIfError(err, "cannot create self diff object")
		}

		// Get the raw diff data comparing all version pairs in this CRD
		rawDiff, err := sd.GetRawDiff()
		if err != nil {
			kingpin.FatalIfError(err, "cannot get raw diff object")
		}

		// Convert the raw diff into a structured change report
		// The second parameter (true) indicates we want full change details
		changeReport, err := crdschema.GetChangesAsStructured(rawDiff, true)
		if err != nil {
			kingpin.FatalIfError(err, "cannot get changes as structured diff")
		}

		// Skip CRDs with no changes (all versions are identical)
		if changeReport.Empty() {
			continue
		}

		// Add this CRD's change report to the output map
		// Key format: "{group}/{kind}" matches what conversion.go expects
		crdSpec := sd.GetCRD().Spec
		jsonData[fmt.Sprintf("%s/%s", crdSpec.Group, crdSpec.Names.Kind)] = changeReport
	}

	// Marshal the complete change report map to JSON
	jsonContent, err := json.Marshal(jsonData)
	if err != nil {
		kingpin.FatalIfError(err, "cannot marshal change report")
	}

	// Write the JSON to the output file
	// 0600 permissions = owner read/write only (secure default)
	kingpin.FatalIfError(os.WriteFile(*out, jsonContent, 0600), "cannot write data to json file")
}

// listYAMLFiles returns paths to all YAML files in the specified directory.
// It only processes files (not subdirectories) with .yaml or .yml extensions.
func listYAMLFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(entries))

	for _, entry := range entries {
		// Skip subdirectories - only process files in the top-level directory
		if entry.IsDir() {
			continue
		}

		// Check file extension (case-insensitive)
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		// Add the full path to the result list
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}

	return paths, nil
}
