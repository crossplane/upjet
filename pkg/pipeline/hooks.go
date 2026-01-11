// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/config"
)

// PostGenerationHook is invoked after the entire code generation pipeline completes.
// It allows custom post-processing across all generated resources and files.
// Hooks are useful for tasks like:
// - Updating kubebuilder markers in previous API versions
// - Custom code transformations
// - Documentation generation
// - Validation checks
type PostGenerationHook interface {
	// Run executes the post-generation hook.
	// Parameters:
	// - ctx: Context for cancellation/timeouts
	// - runner: The PipelineRunner that just completed generation
	// - provider: The provider configuration used for generation
	// - resourcesGroups: All generated resources organized by group -> version -> resource name
	//
	// Returns an error if the hook fails. Pipeline execution will be aborted on error.
	Run(runner *PipelineRunner, provider *config.Provider, resourcesGroups map[string]map[string]map[string]*config.Resource) error
}

// PostGenerationHookFn is a function adapter that implements PostGenerationHook.
// This allows using plain functions as hooks without defining new types.
type PostGenerationHookFn func(runner *PipelineRunner, provider *config.Provider, resourcesGroups map[string]map[string]map[string]*config.Resource) error

// Run implements PostGenerationHook for PostGenerationHookFn.
func (fn PostGenerationHookFn) Run(runner *PipelineRunner, provider *config.Provider, resourcesGroups map[string]map[string]map[string]*config.Resource) error {
	return fn(runner, provider, resourcesGroups)
}

// NewVersionMarkerUpdateHook returns a PostGenerationHook that updates
// kubebuilder markers in previous API version files based on the
// ServedVersions and DeprecatedVersions configuration.
//
// This hook is necessary because previous version files are not regenerated
// during the normal pipeline execution, but their markers need to be updated
// to reflect lifecycle changes (deprecation, removal from served versions).
func NewVersionMarkerUpdateHook() PostGenerationHook {
	return PostGenerationHookFn(func(runner *PipelineRunner, provider *config.Provider, resourcesGroups map[string]map[string]map[string]*config.Resource) error {
		updater := &markerUpdater{
			runner: runner,
		}
		return updater.updateMarkers(resourcesGroups, provider)
	})
}

type markerUpdater struct {
	runner *PipelineRunner
}

func (mu *markerUpdater) updateMarkers(resourcesGroups map[string]map[string]map[string]*config.Resource, _ *config.Provider) error { //nolint:gocyclo // easier to follow as a unit
	for group, versions := range resourcesGroups {
		for _, resources := range versions {
			for _, resource := range resources {
				// Check if this resource has lifecycle configuration
				if len(resource.ServedVersions) == 0 && len(resource.DeprecatedVersions) == 0 {
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
					deprecation, isDeprecated := resource.DeprecatedVersions[prevVersion]

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

func (mu *markerUpdater) updateVersionFile(group, version string, resource *config.Resource, isServed bool, isDeprecated bool, deprecation config.VersionDeprecation) error {
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

	// Parse the Go file
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return errors.Wrapf(err, "cannot parse file %s", filePath)
	}

	// Find the main type declaration (e.g., type Bucket struct)
	typeSpec := findMainTypeSpec(file, resource.Kind)
	if typeSpec == nil {
		return errors.Errorf("cannot find type %s in file %s", resource.Kind, filePath)
	}

	// Update the markers in the doc comments
	updated := updateMarkers(isServed, isDeprecated)
	if !updated {
		// No changes needed
		return nil
	}

	// Write the file back
	// Read the original file content
	content, err := os.ReadFile(filePath) //nolint:gosec // filePath is validated above
	if err != nil {
		return errors.Wrapf(err, "cannot read file %s", filePath)
	}

	// We need to reconstruct the file with updated comments
	// This is complex with go/ast, so we'll use a simpler string-based approach
	// for now by manipulating the comment lines directly
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

// findMainTypeSpec finds the main type declaration in the AST (e.g., "type Bucket struct")
func findMainTypeSpec(file *ast.File, typeName string) *ast.TypeSpec {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if typeSpec.Name.Name == typeName {
				return typeSpec
			}
		}
	}
	return nil
}

// updateMarkers checks if markers need to be updated (returns true if changes needed)
func updateMarkers(isServed bool, isDeprecated bool) bool {
	// For now, always return true if either marker is needed
	// A more sophisticated implementation could check existing markers
	return !isServed || isDeprecated
}

// updateFileMarkers updates the kubebuilder markers and description deprecation notice in the file content using string manipulation
func updateFileMarkers(content string, typeName string, version string, isServed bool, isDeprecated bool, deprecation config.VersionDeprecation) (string, error) {
	lines := strings.Split(content, "\n")

	// Find type declaration
	typeLineIndex := findTypeLineIndex(lines, typeName)
	if typeLineIndex == -1 {
		return "", errors.Errorf("cannot find type %s declaration", typeName)
	}

	// Remove existing lifecycle markers
	lines = removeExistingLifecycleMarkers(lines, typeName)

	// Find insertion point for new markers
	typeLineIndex = findTypeLineIndex(lines, typeName)
	insertionIndex := findMarkerInsertionPoint(lines, typeLineIndex)
	if insertionIndex == -1 {
		return "", errors.Errorf("cannot find insertion point for markers before type %s", typeName)
	}

	// Build and insert new markers
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

// removeExistingLifecycleMarkers removes existing unservedversion and deprecatedversion markers
func removeExistingLifecycleMarkers(lines []string, typeName string) []string {
	typeLineIndex := findTypeLineIndex(lines, typeName)
	if typeLineIndex == -1 {
		return lines
	}

	markerStartIndex := findMarkerBlockStart(lines, typeLineIndex)
	return filterLifecycleMarkers(lines, markerStartIndex, typeLineIndex)
}

// findMarkerBlockStart finds the start of the comment block before the type
func findMarkerBlockStart(lines []string, typeLineIndex int) int {
	markerStartIndex := typeLineIndex - 1
	for markerStartIndex >= 0 && (strings.HasPrefix(strings.TrimSpace(lines[markerStartIndex]), "//") || strings.TrimSpace(lines[markerStartIndex]) == "") {
		markerStartIndex--
	}
	markerStartIndex++ // Move to the first comment line
	return markerStartIndex
}

// filterLifecycleMarkers filters out existing lifecycle markers from the lines
func filterLifecycleMarkers(lines []string, markerStartIndex, typeLineIndex int) []string {
	newLines := make([]string, 0, len(lines))
	inMarkerBlock := false
	for i, line := range lines {
		if i == markerStartIndex {
			inMarkerBlock = true
		}
		if i == typeLineIndex {
			inMarkerBlock = false
		}

		// Skip existing unservedversion and deprecatedversion markers
		if inMarkerBlock && isLifecycleMarker(line) {
			continue
		}

		newLines = append(newLines, line)
	}
	return newLines
}

// isLifecycleMarker checks if a line is a lifecycle marker
func isLifecycleMarker(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "// +kubebuilder:unservedversion") ||
		strings.HasPrefix(trimmed, "// +kubebuilder:deprecatedversion")
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

// insertLifecycleMarkers inserts the lifecycle markers at the specified position
func insertLifecycleMarkers(lines []string, insertionIndex int, isServed bool, isDeprecated bool, deprecation config.VersionDeprecation) []string {
	var markersToInsert []string
	if !isServed {
		markersToInsert = append(markersToInsert, "// +kubebuilder:unservedversion")
	}
	if isDeprecated {
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

// NewStorageVersionMarkerUpdateHook returns a PostGenerationHook that updates
// the +kubebuilder:storageversion marker in previous API version files based on
// the CRDStorageVersion configuration.
//
// This hook is necessary because when the storage version changes (e.g., during
// the Bridge phase from v1beta1 to v1beta2), the old version files aren't
// regenerated, so their storage version markers become stale. This hook ensures
// that only the configured storage version has the marker.
func NewStorageVersionMarkerUpdateHook() PostGenerationHook {
	return PostGenerationHookFn(func(runner *PipelineRunner, provider *config.Provider, resourcesGroups map[string]map[string]map[string]*config.Resource) error {
		updater := &storageVersionMarkerUpdater{
			runner: runner,
		}
		return updater.updateMarkers(resourcesGroups, provider)
	})
}

type storageVersionMarkerUpdater struct {
	runner *PipelineRunner
}

func (svu *storageVersionMarkerUpdater) updateMarkers(resourcesGroups map[string]map[string]map[string]*config.Resource, _ *config.Provider) error {
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

func (svu *storageVersionMarkerUpdater) updateVersionFile(group, version string, resource *config.Resource, shouldHaveMarker bool) error { //nolint:gocyclo // sequential file validation and update operations
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

	// Parse the Go file to check if marker exists
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return errors.Wrapf(err, "cannot parse file %s", filePath)
	}

	// Find the main type declaration
	typeSpec := findMainTypeSpec(file, resource.Kind)
	if typeSpec == nil {
		return errors.Errorf("cannot find type %s in file %s", resource.Kind, filePath)
	}

	// Read the file content
	content, err := os.ReadFile(filePath) //nolint:gosec // filePath is validated above
	if err != nil {
		return errors.Wrapf(err, "cannot read file %s", filePath)
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

	// Find and remove the storage version marker in the block before the type
	// Walk backwards from type declaration up to ~20 lines
	result := make([]string, 0, len(lines))
	markerStart := typeLineIndex - 20
	if markerStart < 0 {
		markerStart = 0
	}

	for i, line := range lines {
		// Check if we're in the range before the type and this is the marker line
		if i >= markerStart && i < typeLineIndex {
			trimmed := strings.TrimSpace(line)
			if trimmed == "// +kubebuilder:storageversion" {
				// Skip this line (remove it)
				continue
			}
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n"), nil
}

// validateFilePath ensures the file path is safe and within the expected base directory
func validateFilePath(filePath, baseDir string) error {
	// Clean the paths to resolve any .. or . components
	cleanPath := filepath.Clean(filePath)
	cleanBase := filepath.Clean(baseDir)

	// Convert to absolute paths for comparison
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return errors.Wrapf(err, "cannot get absolute path for %s", cleanPath)
	}

	absBase, err := filepath.Abs(cleanBase)
	if err != nil {
		return errors.Wrapf(err, "cannot get absolute path for %s", cleanBase)
	}

	// Check if the file path is within the base directory
	relPath, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return errors.Wrapf(err, "cannot get relative path from %s to %s", absBase, absPath)
	}

	// If the relative path starts with "..", it's outside the base directory
	if strings.HasPrefix(relPath, "..") {
		return errors.Errorf("path %s is outside base directory %s", filePath, baseDir)
	}

	return nil
}
