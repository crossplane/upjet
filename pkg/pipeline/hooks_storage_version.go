// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/config"
)

// newStorageVersionMarkerUpdateHook returns a postGenerationHook that updates
// the +kubebuilder:storageversion marker in previous API version files based on
// the CRDStorageVersion configuration.
//
// This hook is necessary because when the storage version changes (e.g., during
// the Bridge phase from v1beta1 to v1beta2), the old version files aren't
// regenerated, so their storage version markers become stale. This hook ensures
// that only the configured storage version has the marker.
func newStorageVersionMarkerUpdateHook() postGenerationHook {
	return &storageVersionMarkerUpdater{}
}

type storageVersionMarkerUpdater struct {
	runner *PipelineRunner
}

// Run implements postGenerationHook for storageVersionMarkerUpdater.
func (svu *storageVersionMarkerUpdater) Run(runner *PipelineRunner, provider *config.Provider, resourcesGroups map[string]map[string]map[string]*config.Resource) error {
	svu.runner = runner
	for group, versions := range resourcesGroups {
		for _, resources := range versions {
			for _, resource := range resources {
				// Only process resources with previous versions
				if len(resource.PreviousVersions) == 0 {
					continue
				}

				// Get the configured storage version
				storageVersion := resource.CRDStorageVersion()

				// Process each previous version
				for _, prevVersion := range resource.PreviousVersions {
					// Determine if this version should have the storage marker
					shouldHaveMarker := prevVersion == storageVersion

					// Update the file for this previous version
					if err := svu.updateVersionFile(group, prevVersion, resource, shouldHaveMarker); err != nil {
						return errors.Wrapf(err, "cannot update storage version marker for %s/%s in group %s", resource.Kind, prevVersion, group)
					}
				}
			}
		}
	}
	return nil
}

func (svu *storageVersionMarkerUpdater) updateVersionFile(group, version string, resource *config.Resource, shouldHaveMarker bool) error {
	// Construct file path: <apiDir>/<shortGroup>/<version>/zz_<kind>_types.go
	shortGroup := strings.ToLower(strings.Split(group, ".")[0])
	fileName := fmt.Sprintf("zz_%s_types.go", strings.ToLower(resource.Kind))
	filePath := filepath.Join(svu.runner.DirAPIs, shortGroup, version, fileName)

	// Validate the file path is within the expected directory
	if err := validateFilePath(filePath, svu.runner.DirAPIs); err != nil {
		return errors.Wrapf(err, "invalid file path %s", filePath)
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// File doesn't exist, skip (this is not necessarily an error)
		return nil
	}

	// Read the file content
	content, err := os.ReadFile(filePath) //nolint:gosec // filePath is validated above
	if err != nil {
		return errors.Wrapf(err, "cannot read file %s", filePath)
	}

	// Validate that the type exists in the file (simple string check)
	// This is safe because we're working with generated code with predictable structure
	typeDeclaration := fmt.Sprintf("type %s struct", resource.Kind)
	if !strings.Contains(string(content), typeDeclaration) {
		return errors.Errorf("cannot find type %s in file %s", resource.Kind, filePath)
	}

	// Check if the file currently has the marker
	hasMarker := strings.Contains(string(content), "// +kubebuilder:storageversion")

	// Determine if we need to make changes
	if hasMarker == shouldHaveMarker {
		// File is already in the correct state
		return nil
	}

	// Update the file
	var updatedContent string
	if shouldHaveMarker {
		// Add the marker
		updatedContent, err = addStorageVersionMarker(string(content), resource.Kind)
	} else {
		// Remove the marker
		updatedContent, err = removeStorageVersionMarker(string(content), resource.Kind)
	}
	if err != nil {
		return errors.Wrapf(err, "cannot update storage version marker in file %s", filePath)
	}

	// Write back to file
	if err := os.WriteFile(filePath, []byte(updatedContent), 0600); err != nil {
		return errors.Wrapf(err, "cannot write file %s", filePath)
	}

	action := "Removed"
	if shouldHaveMarker {
		action = "Added"
	}
	fmt.Printf("  %s storage version marker for %s/%s\n", action, resource.Kind, version)
	return nil
}

// addStorageVersionMarker adds the +kubebuilder:storageversion marker to the file
func addStorageVersionMarker(content string, typeName string) (string, error) {
	lines := strings.Split(content, "\n")

	// Find the line with "type <TypeName> struct"
	typeLineIndex := -1
	for i, line := range lines {
		if strings.Contains(line, fmt.Sprintf("type %s struct", typeName)) {
			typeLineIndex = i
			break
		}
	}

	if typeLineIndex == -1 {
		return "", errors.Errorf("cannot find type %s declaration", typeName)
	}

	// Find the marker block before the type (walk backwards)
	// Look for +kubebuilder:subresource:status or +kubebuilder:object:root=true
	insertionIndex := -1
	for i := typeLineIndex - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "// +kubebuilder:subresource:status") {
			// Insert after subresource:status
			insertionIndex = i + 1
			break
		}
		if strings.HasPrefix(line, "// +kubebuilder:object:root=true") && insertionIndex == -1 {
			// Fallback: insert after object:root if subresource not found
			insertionIndex = i + 1
		}
	}

	if insertionIndex == -1 {
		return "", errors.Errorf("cannot find insertion point for storage version marker before type %s", typeName)
	}

	// Insert the marker
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:insertionIndex]...)
	result = append(result, "// +kubebuilder:storageversion")
	result = append(result, lines[insertionIndex:]...)

	return strings.Join(result, "\n"), nil
}

// removeStorageVersionMarker removes the +kubebuilder:storageversion marker from the file
func removeStorageVersionMarker(content string, typeName string) (string, error) {
	marker := "// +kubebuilder:storageversion\n"
	if !strings.Contains(content, marker) {
		return "", errors.Errorf("storage version marker not found for type %s", typeName)
	}
	return strings.ReplaceAll(content, marker, ""), nil
}
