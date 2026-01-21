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

// newLifecycleMarkerUpdateHook returns a postGenerationHook that updates
// kubebuilder markers in previous API version files based on the
// served versions (via GetServedVersions) and DeprecatedVersions configuration.
//
// This hook is necessary because previous version files are not regenerated
// during the normal pipeline execution, but their markers need to be updated
// to reflect lifecycle changes (deprecation, removal from served versions).
func newLifecycleMarkerUpdateHook() postGenerationHook {
	return &lifecycleMarkerUpdater{}
}

type lifecycleMarkerUpdater struct {
	runner *PipelineRunner
}

// Run implements postGenerationHook for lifecycleMarkerUpdater.
func (mu *lifecycleMarkerUpdater) Run(runner *PipelineRunner, provider *config.Provider, resourcesGroups map[string]map[string]map[string]*config.Resource) error { //nolint:gocyclo // easier to follow as a unit
	mu.runner = runner
	for group, versions := range resourcesGroups {
		for _, resources := range versions {
			for _, resource := range resources {
				// Check if this resource has lifecycle configuration
				if !resource.HasExplicitServedVersions() && len(resource.GetDeprecatedVersions()) == 0 {
					continue
				}

				// Get served versions with proper defaulting
				servedVersions := resource.GetServedVersions()
				servedVersionsSet := make(map[string]bool)
				for _, v := range servedVersions {
					servedVersionsSet[v] = true
				}

				// Process each previous version
				for _, prevVersion := range resource.PreviousVersions {
					// Determine what markers this version needs
					isServed := servedVersionsSet[prevVersion]
					deprecation, isDeprecated := resource.IsVersionDeprecated(prevVersion)

					// Skip if no marker updates needed
					if isServed && !isDeprecated {
						continue
					}

					// Update the file for this previous version
					if err := mu.updateVersionFile(group, prevVersion, resource, isServed, isDeprecated, deprecation); err != nil {
						return errors.Wrapf(err, "cannot update markers for %s/%s in group %s", resource.Kind, prevVersion, group)
					}
				}
			}
		}
	}
	return nil
}

func (mu *lifecycleMarkerUpdater) updateVersionFile(group, version string, resource *config.Resource, isServed bool, isDeprecated bool, deprecation config.VersionDeprecation) error {
	// Construct file path: <apiDir>/<shortGroup>/<version>/zz_<kind>_types.go
	shortGroup := strings.ToLower(strings.Split(group, ".")[0])
	fileName := fmt.Sprintf("zz_%s_types.go", strings.ToLower(resource.Kind))
	filePath := filepath.Join(mu.runner.DirAPIs, shortGroup, version, fileName)

	// Validate the file path is within the expected directory
	if err := validateFilePath(filePath, mu.runner.DirAPIs); err != nil {
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

	// Skip if no marker updates needed
	if isServed && !isDeprecated {
		return nil
	}

	// Update markers using string manipulation
	// This is safe for generated code with predictable structure
	updatedContent, err := updateFileMarkers(string(content), resource.Kind, version, isServed, isDeprecated, deprecation)
	if err != nil {
		return errors.Wrapf(err, "cannot update markers in file %s", filePath)
	}

	// Write back to file
	if err := os.WriteFile(filePath, []byte(updatedContent), 0600); err != nil {
		return errors.Wrapf(err, "cannot write file %s", filePath)
	}

	fmt.Printf("  Updated markers for %s/%s\n", resource.Kind, version)
	return nil
}

// updateFileMarkers updates the kubebuilder markers and description deprecation notice in the file content using string manipulation.
// It follows a remove-then-conditionally-add pattern for idempotent updates:
// 1. Remove all existing lifecycle markers (clean slate)
// 2. Add back only the markers that should exist based on current configuration
// This ensures markers are always in sync with the configuration, regardless of previous state.
func updateFileMarkers(content string, typeName string, version string, isServed bool, isDeprecated bool, deprecation config.VersionDeprecation) (string, error) {
	// Step 1: Remove all existing lifecycle markers to start from a clean state
	content = removeLifecycleMarkers(content)

	// Now work with lines for adding new markers
	lines := strings.Split(content, "\n")

	// Find type declaration
	typeLineIndex := findTypeLineIndex(lines, typeName)
	if typeLineIndex == -1 {
		return "", errors.Errorf("cannot find type %s declaration", typeName)
	}

	// Find insertion point for new markers
	insertionIndex := findMarkerInsertionPoint(lines, typeLineIndex)
	if insertionIndex == -1 {
		return "", errors.Errorf("cannot find insertion point for markers before type %s", typeName)
	}

	// Step 2: Add back only the markers that match current configuration
	lines = insertLifecycleMarkers(lines, insertionIndex, isServed, isDeprecated, deprecation)

	// Handle description comment deprecation notice
	lines = updateDescriptionDeprecationNotice(lines, typeName, version, isDeprecated, deprecation)

	return strings.Join(lines, "\n"), nil
}

// findTypeLineIndex finds the line index of the type declaration
func findTypeLineIndex(lines []string, typeName string) int {
	for i, line := range lines {
		if strings.Contains(line, fmt.Sprintf("type %s struct", typeName)) {
			return i
		}
	}
	return -1
}

// removeLifecycleMarkers removes all lifecycle markers using simple string replacement.
// This is safe because if markers appear multiple times by accident, that's a bug
// and removing all occurrences is the correct behavior.
func removeLifecycleMarkers(content string) string {
	// Remove unservedversion marker
	content = strings.ReplaceAll(content, "// +kubebuilder:unservedversion\n", "")

	// Remove deprecatedversion markers (with any warning text)
	// Use a simple approach: find and remove lines starting with the marker
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "// +kubebuilder:deprecatedversion") {
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// findMarkerInsertionPoint finds where to insert new lifecycle markers
func findMarkerInsertionPoint(lines []string, typeLineIndex int) int {
	// Walk backwards to find where to insert
	for i := typeLineIndex - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "// +kubebuilder:storageversion") {
			// Insert after storageversion
			return i + 1
		}
		if strings.HasPrefix(line, "// +kubebuilder:subresource:status") {
			// If no storageversion, insert after subresource
			return i + 1
		}
	}
	return -1
}

// insertLifecycleMarkers inserts the lifecycle markers at the specified position.
// Markers are added conditionally based on current configuration:
// - If !isServed: adds +kubebuilder:unservedversion
// - If isDeprecated: adds +kubebuilder:deprecatedversion with warning
func insertLifecycleMarkers(lines []string, insertionIndex int, isServed bool, isDeprecated bool, deprecation config.VersionDeprecation) []string {
	var markersToInsert []string
	if !isServed {
		// Version is not served, add unservedversion marker
		markersToInsert = append(markersToInsert, "// +kubebuilder:unservedversion")
	}
	if isDeprecated {
		// Version is deprecated, add deprecatedversion marker with warning
		enhancedWarning := buildEnhancedDeprecationWarning(deprecation)
		markersToInsert = append(markersToInsert, fmt.Sprintf("// +kubebuilder:deprecatedversion:warning=\"%s\"", enhancedWarning))
	}

	result := make([]string, 0, len(lines)+len(markersToInsert))
	result = append(result, lines[:insertionIndex]...)
	result = append(result, markersToInsert...)
	result = append(result, lines[insertionIndex:]...)
	return result
}

// updateDescriptionDeprecationNotice updates the deprecation notice in the description comment
func updateDescriptionDeprecationNotice(lines []string, typeName string, version string, isDeprecated bool, deprecation config.VersionDeprecation) []string {
	typeLineIndex := findTypeLineIndex(lines, typeName)
	if typeLineIndex == -1 {
		return lines
	}

	descriptionStartIndex, descriptionEndIndex := findDescriptionBlock(lines, typeLineIndex)
	if descriptionStartIndex == -1 || descriptionEndIndex == -1 {
		return lines
	}

	// Remove existing deprecation notice
	lines = removeDeprecationNoticeFromDescription(lines, descriptionStartIndex, descriptionEndIndex)

	// Insert new deprecation notice if needed
	if isDeprecated {
		lines = insertDeprecationNotice(lines, typeName, version, deprecation)
	}

	return lines
}

// findDescriptionBlock finds the description comment block before the type
func findDescriptionBlock(lines []string, typeLineIndex int) (int, int) {
	descriptionStartIndex := -1
	descriptionEndIndex := -1

	for i := typeLineIndex - 1; i >= 0 && i >= typeLineIndex-10; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			if descriptionEndIndex != -1 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "// +kubebuilder:") {
			if descriptionEndIndex == -1 {
				descriptionEndIndex = i
			}
			descriptionStartIndex = i
		} else {
			if descriptionEndIndex != -1 {
				break
			}
			if strings.HasPrefix(line, "// +kubebuilder:") {
				continue
			}
			break
		}
	}

	return descriptionStartIndex, descriptionEndIndex
}

// removeDeprecationNoticeFromDescription removes existing deprecation notice from description
func removeDeprecationNoticeFromDescription(lines []string, descriptionStartIndex, descriptionEndIndex int) []string {
	newLines := make([]string, 0, len(lines))
	inDeprecationNotice := false
	for i, line := range lines {
		if i >= descriptionStartIndex && i <= descriptionEndIndex {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "Deprecated:") {
				inDeprecationNotice = true
				continue
			}
			if inDeprecationNotice {
				if trimmed == "//" || i == descriptionEndIndex {
					inDeprecationNotice = false
				}
				if trimmed != "//" {
					continue
				}
			}
		}
		newLines = append(newLines, line)
	}
	return newLines
}

// insertDeprecationNotice inserts the deprecation notice into the description
func insertDeprecationNotice(lines []string, typeName string, version string, deprecation config.VersionDeprecation) []string {
	typeLineIndex := findTypeLineIndex(lines, typeName)
	if typeLineIndex == -1 {
		return lines
	}

	deprecationNotice := buildDeprecationNotice(version, deprecation)
	noticeLines := strings.Split(deprecationNotice, "\n")

	// Find the main description line
	descriptionLineIndex := findMainDescriptionLine(lines, typeLineIndex)

	if descriptionLineIndex != -1 {
		result := make([]string, 0, len(lines)+len(noticeLines))
		result = append(result, lines[:descriptionLineIndex+1]...)
		result = append(result, noticeLines...)
		result = append(result, lines[descriptionLineIndex+1:]...)
		return result
	}

	// Fallback: insert before type
	result := make([]string, 0, len(lines)+len(noticeLines))
	result = append(result, lines[:typeLineIndex]...)
	result = append(result, noticeLines...)
	result = append(result, lines[typeLineIndex:]...)
	return result
}

// findMainDescriptionLine finds the main description comment line
func findMainDescriptionLine(lines []string, typeLineIndex int) int {
	for i := typeLineIndex - 1; i >= 0 && i >= typeLineIndex-20; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "// +kubebuilder:") && strings.Contains(line, "is the Schema for") {
			return i
		}
	}
	return -1
}
