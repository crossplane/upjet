// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	encodingjson "encoding/json"
	"strings"

	"dario.cat/mergo"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"

	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/config/conversion"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/types/name"
)

// mergeAnnotationFieldsWithSpec merges field values stored in annotations back into
// the spec.forProvider and spec.initProvider parameter maps. This function is critical
// for handling API version compatibility when controllers run older API versions.
//
// Context: When a new field is added to a CRD API version, and a resource is created
// with this field in a newer version, controllers running older API versions will not
// have this field in their Go types. During API conversion, these field values are
// stored in a consolidated annotation to prevent data loss. This function restores
// those values before passing parameters to Terraform.
//
// The function:
// 1. Reads the consolidated annotation ("internal.upjet.crossplane.io/field-conversions")
// which contains a JSON map of all field conversions
// 2. For fields with "spec.forProvider." prefix, merges values into parameters map
// 3. For fields with "spec.initProvider." prefix, merges values into initParameters map
// 4. Only sets annotation values if the field doesn't already exist or has a different value
// 5. Handles both camelCase (field path in annotation) and snake_case (Terraform parameter key) conversions
// 6. Merges initProvider into forProvider if shouldMergeInitProvider is true
//
// Example:
// If annotation "internal.upjet.crossplane.io/field-conversions" contains:
// {"spec.forProvider.newField": "value"}
// and "new_field" doesn't exist in parameters, it will be set from the annotation.
//
// Parameters:
//   - tr: The Terraformed resource
//   - shouldMergeInitProvider: Whether to merge initProvider into forProvider
//   - annotations: The resource's annotations map
//
// Returns the merged parameters map or an error if field operations fail.
func mergeAnnotationFieldsWithSpec(tr resource.Terraformed, shouldMergeInitProvider bool, annotations map[string]string) (map[string]any, error) { //nolint:gocyclo
	parameters, err := tr.GetParameters()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get parameters")
	}
	initParameters, err := tr.GetInitParameters()
	if err != nil {
		return nil, errors.Wrap(err, "cannot get init parameters")
	}

	parametersPaved := fieldpath.Pave(parameters)
	initParametersPaved := fieldpath.Pave(initParameters)

	// Get the consolidated annotation containing all field conversions
	annotationValue, ok := annotations[conversion.AnnotationKey]
	if ok && annotationValue != "" {
		// Unmarshal the JSON map
		fieldMap := map[string]any{}
		if err := encodingjson.Unmarshal([]byte(annotationValue), &fieldMap); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal annotation %q from JSON", conversion.AnnotationKey)
		}

		// Iterate through each field in the JSON map
		for fieldPath, annotationFieldValue := range fieldMap {
			switch {
			case strings.HasPrefix(fieldPath, "spec.forProvider."):
				key := strings.TrimPrefix(fieldPath, "spec.forProvider.")
				snakeKey := name.NewFromCamel(key).Snake
				currentValue, err := parametersPaved.GetValue(snakeKey)
				if err != nil {
					if fieldpath.IsNotFound(err) {
						// Field doesn't exist, set it from annotation
						if err := parametersPaved.SetValue(snakeKey, annotationFieldValue); err != nil {
							return nil, errors.Wrapf(err, "cannot set value for %s", snakeKey)
						}
					} else {
						return nil, errors.Wrapf(err, "cannot get the current value for %s", snakeKey)
					}
				} else {
					// Field exists, compare values - only set if different
					if !areValuesEqual(currentValue, annotationFieldValue) {
						if err := parametersPaved.SetValue(snakeKey, annotationFieldValue); err != nil {
							return nil, errors.Wrapf(err, "cannot set value for %s", snakeKey)
						}
					}
				}
			case strings.HasPrefix(fieldPath, "spec.initProvider.") && shouldMergeInitProvider:
				key := strings.TrimPrefix(fieldPath, "spec.initProvider.")
				snakeKey := name.NewFromCamel(key).Snake
				currentValue, err := initParametersPaved.GetValue(snakeKey)
				if err != nil {
					if fieldpath.IsNotFound(err) {
						// Field doesn't exist, set it from annotation
						if err := initParametersPaved.SetValue(snakeKey, annotationFieldValue); err != nil {
							return nil, errors.Wrapf(err, "cannot set value for %s", snakeKey)
						}
					} else {
						return nil, errors.Wrapf(err, "cannot get the current value for %s", snakeKey)
					}
				} else {
					// Field exists, compare values - only set if different
					if !areValuesEqual(currentValue, annotationFieldValue) {
						if err := initParametersPaved.SetValue(snakeKey, annotationFieldValue); err != nil {
							return nil, errors.Wrapf(err, "cannot set value for %s", snakeKey)
						}
					}
				}
			}
		}
	}

	fp := parametersPaved.UnstructuredContent()
	ip := initParametersPaved.UnstructuredContent()

	if shouldMergeInitProvider {
		// Note(lsviben): mergo.WithSliceDeepCopy is needed to merge the
		// slices from the initProvider to forProvider. As it also sets
		// overwrite to true, we need to set it back to false, we don't
		// want to overwrite the forProvider fields with the initProvider
		// fields.
		if err := mergo.Merge(&fp, ip, mergo.WithSliceDeepCopy, func(c *mergo.Config) {
			c.Overwrite = false
		}); err != nil {
			return nil, errors.Wrapf(err, "cannot merge spec.initProvider and spec.forProvider parameters")
		}
	}
	return fp, nil
}

// moveTFStateValuesToAnnotation moves field values from Terraform state to annotations
// when those fields don't exist in the CRD's status.atProvider schema. This function
// prevents status data loss during API version transitions.
//
// Context: When Terraform returns state containing a field that was added in a newer
// API version, controllers running older API versions won't have this field in their
// status.atProvider Go types. Without this function, these values would be silently
// dropped during the conversion from TF state to CRD status.
//
// The function:
// 1. Early exits if ControllerReconcileVersion == Version (no conversion needed)
// 2. Reads the list of field paths from c.TfStatusConversionPaths
// 3. Reads existing field conversion map from the consolidated annotation
// 4. For each configured path, checks if the field exists in TF state (tfObservation)
// 5. Checks if the same field exists in the CRD's status.atProvider (atProvider)
// 6. If field exists in TF state but NOT in status.atProvider (old API version):
// - Adds the field to the conversion map with its value
// - Only updates if the value changed (uses areValuesEqual for comparison)
// 7. If field exists in both, no action is taken (normal case)
// 8. Stores the updated conversion map in the consolidated annotation
//
// This allows field values to be preserved in the consolidated annotation until the
// controller is upgraded to a version that includes the fields in the API schema.
//
// Example:
// If TF state has "new_status_field" but the old API version doesn't have this field
// in status.atProvider, the value is added to:
// annotation["internal.upjet.crossplane.io/field-conversions"] =
// {"status.atProvider.newStatusField": <value>, ...}
//
// Parameters:
// - tfObservation: The complete Terraform state as returned by TF operations
// - atProvider: The status.atProvider map that will be set on the CRD
// - annotations: The resource's annotations map (will be modified in-place)
// - c: The resource configuration containing ControllerReconcileVersion and TfStatusConversionPaths
//
// Returns a boolean indicating if annotations were updated, and an error if field
// operations or JSON marshaling fails.
func moveTFStateValuesToAnnotation(tfObservation map[string]any, atProvider map[string]any, annotations map[string]string, c *config.Resource) (bool, error) { //nolint:gocyclo // easier to follow as a unit
	if c.ControllerReconcileVersion == c.Version { //nolint:staticcheck // still handling deprecated field behavior
		return false, nil
	}

	paths := c.TfStatusConversionPaths

	if len(paths) == 0 {
		return false, nil
	}

	// Get existing conversion map from annotation (if any)
	fieldMap := map[string]any{}
	if existingAnnotation, ok := annotations[conversion.AnnotationKey]; ok && existingAnnotation != "" {
		if err := encodingjson.Unmarshal([]byte(existingAnnotation), &fieldMap); err != nil {
			return false, errors.Wrapf(err, "failed to unmarshal existing annotation %q from JSON", conversion.AnnotationKey)
		}
	}

	tfObservationPaved := fieldpath.Pave(tfObservation)
	atProviderPaved := fieldpath.Pave(atProvider)

	updated := false
	for _, path := range paths {
		p := strings.TrimPrefix(path, "status.atProvider.")
		snakeP := name.NewFromCamel(p).Snake
		tfFieldValue, err := tfObservationPaved.GetValue(snakeP)
		if err != nil {
			return false, errors.Wrapf(err, "cannot get value for %s", snakeP)
		}
		_, err = atProviderPaved.GetValue(snakeP)
		if err != nil {
			if fieldpath.IsNotFound(err) {
				// Field not in atProvider, check if we need to update annotation
				existingAnnotationValue, existsInAnnotation := fieldMap[path]
				if !existsInAnnotation || !areValuesEqual(existingAnnotationValue, tfFieldValue) {
					// Either doesn't exist in annotation or value is different - update it
					fieldMap[path] = tfFieldValue
					updated = true
				}
				// else: value already in annotation and is the same, no-op
			} else {
				return false, errors.Wrapf(err, "cannot get the current value for %s", snakeP)
			}
		}
		// else: field exists in atProvider, no need to store in annotation
	}

	// Only update annotation if we actually changed something
	if updated {
		jsonBytes, err := encodingjson.Marshal(fieldMap)
		if err != nil {
			return false, errors.Wrapf(err, "failed to marshal annotation %q to JSON", conversion.AnnotationKey)
		}
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[conversion.AnnotationKey] = string(jsonBytes)
	}

	return updated, nil
}

// mergeAnnotationFieldsWithStatus merges field values stored in the consolidated
// annotation back into the status.atProvider map. This function is used during
// Connect() to restore TF state from both the CRD status and annotation values.
//
// Context: When reconstructing Terraform state from a CRD (in Connect), we need
// the complete state including fields that don't exist in the current controller's
// API version. Those fields are stored in annotations and must be merged back into
// the state before passing it to Terraform's operation tracker.
//
// The function:
// 1. Early exits if ControllerReconcileVersion == Version (no conversion needed)
// 2. Reads the consolidated annotation ("internal.upjet.crossplane.io/field-conversions")
// 3. Extracts fields with "status.atProvider." prefix from the JSON map
// 4. Merges those values into the atProvider map (converting camelCase to snake_case)
// 5. Only updates fields if they don't exist or have different values
//
// This ensures Terraform sees the complete state and doesn't generate unnecessary
// diffs for fields that exist in annotations but not in the current schema.
//
// Parameters:
// - atProvider: The status.atProvider map to merge annotation values into
// - annotations: The resource's annotations map
// - c: The resource configuration, used to check ControllerReconcileVersion
//
// Returns an error if field operations or JSON unmarshaling fails.
func mergeAnnotationFieldsWithStatus(atProvider map[string]any, annotations map[string]string, c *config.Resource) error { //nolint:gocyclo // easier to follow as a unit
	if c.ControllerReconcileVersion == c.Version { //nolint:staticcheck // still handling deprecated field behavior
		return nil
	}

	// Get the consolidated annotation containing all field conversions
	annotationValue, ok := annotations[conversion.AnnotationKey]
	if !ok || annotationValue == "" {
		// No conversion annotations present
		return nil
	}

	// Unmarshal the JSON map
	fieldMap := map[string]any{}
	if err := encodingjson.Unmarshal([]byte(annotationValue), &fieldMap); err != nil {
		return errors.Wrapf(err, "failed to unmarshal annotation %q from JSON", conversion.AnnotationKey)
	}

	atProviderPaved := fieldpath.Pave(atProvider)

	// Iterate through each field in the JSON map
	for fieldPath, annotationFieldValue := range fieldMap {
		if strings.HasPrefix(fieldPath, "status.atProvider.") {
			key := strings.TrimPrefix(fieldPath, "status.atProvider.")
			snakeKey := name.NewFromCamel(key).Snake
			currentValue, err := atProviderPaved.GetValue(snakeKey)
			if err != nil {
				if fieldpath.IsNotFound(err) {
					// Field doesn't exist, set it from annotation
					if err := atProviderPaved.SetValue(snakeKey, annotationFieldValue); err != nil {
						return errors.Wrapf(err, "cannot set value for %s", snakeKey)
					}
				} else {
					return errors.Wrapf(err, "cannot get the current value for %s", snakeKey)
				}
			} else {
				// Field exists, compare values - only set if different
				if !areValuesEqual(currentValue, annotationFieldValue) {
					if err := atProviderPaved.SetValue(snakeKey, annotationFieldValue); err != nil {
						return errors.Wrapf(err, "cannot set value for %s", snakeKey)
					}
				}
			}
		}
	}
	return nil
}

// areValuesEqual performs deep equality comparison for two values.
// It handles the comparison by marshaling both values to JSON and comparing
// the JSON representations, which works for all types we store in annotations.
func areValuesEqual(v1, v2 any) bool {
	// Handle nil cases
	if v1 == nil && v2 == nil {
		return true
	}
	if v1 == nil || v2 == nil {
		return false
	}

	// Marshal both values to JSON for comparison
	j1, err1 := encodingjson.Marshal(v1)
	j2, err2 := encodingjson.Marshal(v2)

	// If either fails to marshal, consider them not equal
	if err1 != nil || err2 != nil {
		return false
	}

	return bytes.Equal(j1, j2)
}
