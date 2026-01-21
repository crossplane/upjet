// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"path/filepath"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/config"
)

// postGenerationHook is invoked after the entire code generation pipeline completes.
// It allows custom post-processing across all generated resources and files.
// Hooks are useful for tasks like:
// - Updating kubebuilder markers in previous API versions
// - Custom code transformations
// - Documentation generation
// - Validation checks
type postGenerationHook interface {
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
