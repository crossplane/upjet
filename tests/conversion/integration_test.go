// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package testconversion

import (
	"encoding/json"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/crossplane/upjet/v2/pkg/config"
	ujconversion "github.com/crossplane/upjet/v2/pkg/controller/conversion"
	"github.com/crossplane/upjet/v2/pkg/resource/fake"
)

// Level 2 Integration Tests
// These tests verify that registered conversions EXECUTE correctly and transform data properly.
// Unlike Level 1 tests which only verify registration, these tests call RoundTrip and verify
// the actual conversion logic works with real data transformation.

func TestConversionIntegration(t *testing.T) {
	// ============================================================================
	// SETUP: Create all resources and register with singleton registry ONCE
	// ============================================================================

	pc := config.NewTestProvider(t)

	// Resource 1: TestResource for field addition/deletion tests
	r1 := config.NewTestResource("TestResource", nil)
	r1.ShortGroup = "test"
	r1.Kind = "TestResource"
	pc.Resources["test_resource"] = r1

	// Resource 2: StringToNumberResource (count: string -> number)
	r2 := config.NewTestResourceWithIntField("StringToNumberResource", "count")
	r2.ShortGroup = "test"
	r2.Kind = "StringToNumberResource"
	pc.Resources["string_to_number_resource"] = r2

	// Resource 3: NumberToStringResource (value: number -> string)
	r3 := config.NewTestResourceWithStringField("NumberToStringResource", "value")
	r3.ShortGroup = "test"
	r3.Kind = "NumberToStringResource"
	pc.Resources["number_to_string_resource"] = r3

	// Resource 4: StringToBoolResource (enabled: string -> bool)
	r4 := config.NewTestResourceWithBoolField("StringToBoolResource", "enabled")
	r4.ShortGroup = "test"
	r4.Kind = "StringToBoolResource"
	pc.Resources["string_to_bool_resource"] = r4

	// Resource 5: BoolToStringResource (flag: bool -> string)
	r5 := config.NewTestResourceWithStringField("BoolToStringResource", "flag")
	r5.ShortGroup = "test"
	r5.Kind = "BoolToStringResource"
	pc.Resources["bool_to_string_resource"] = r5

	// Resource 6: MultiChangeResource - multiple changes in same version
	// Has: newField (added), count (string->int), enabled (string->bool)
	r6 := config.NewTestResource("MultiChangeResource", map[string]*schema.Schema{
		"count": {
			Type:     schema.TypeInt,
			Optional: true,
		},
		"enabled": {
			Type:     schema.TypeBool,
			Optional: true,
		},
	})
	r6.ShortGroup = "test"
	r6.Kind = "MultiChangeResource"
	pc.Resources["multi_change_resource"] = r6

	// Load and merge all fixtures
	fixture1 := config.LoadTestFixture(t, "valid/field-addition.json")
	fixture2 := config.LoadTestFixture(t, "string-to-number.json")
	fixture3 := config.LoadTestFixture(t, "number-to-string.json")
	fixture4 := config.LoadTestFixture(t, "string-to-bool.json")
	fixture5 := config.LoadTestFixture(t, "bool-to-string.json")
	fixture6 := config.LoadTestFixture(t, "multiple-changes.json")

	var data1, data2, data3, data4, data5, data6 map[string]interface{}
	json.Unmarshal(fixture1, &data1)
	json.Unmarshal(fixture2, &data2)
	json.Unmarshal(fixture3, &data3)
	json.Unmarshal(fixture4, &data4)
	json.Unmarshal(fixture5, &data5)
	json.Unmarshal(fixture6, &data6)

	merged := make(map[string]interface{})
	for k, v := range data1 {
		merged[k] = v
	}
	for k, v := range data2 {
		merged[k] = v
	}
	for k, v := range data3 {
		merged[k] = v
	}
	for k, v := range data4 {
		merged[k] = v
	}
	for k, v := range data5 {
		merged[k] = v
	}
	for k, v := range data6 {
		merged[k] = v
	}

	mergedJSON, _ := json.Marshal(merged)

	// Register conversions ONCE for all resources
	err := config.RegisterAutoConversions(pc, mergedJSON)
	if err != nil {
		t.Fatalf("Failed to register conversions: %v", err)
	}

	// Register with the global singleton registry ONCE
	scheme := runtime.NewScheme()
	_ = fake.AddToScheme(scheme)
	err = ujconversion.RegisterConversions(pc, nil, scheme, ujconversion.WithLogger(logging.NewNopLogger()))
	if err != nil {
		t.Fatalf("Failed to register conversions with registry: %v", err)
	}

	// ============================================================================
	// TEST SUITE 1: Field Addition/Deletion
	// ============================================================================

	t.Run("FieldAddition", func(t *testing.T) {
		t.Run("v1alpha1_to_v1beta1_field_from_annotation", func(t *testing.T) {
			// v1alpha1 object with annotation containing newField value
			src := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1alpha1",
					Kind:       "TestResource",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						"internal.upjet.crossplane.io/field-conversions": `{"spec.forProvider.newField":"restored-value"}`,
					},
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{
						// newField doesn't exist in v1alpha1 schema
					},
				},
			}

			// v1beta1 destination
			dst := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1beta1",
					Kind:       "TestResource",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{},
				},
			}

			// Execute conversion
			err := ujconversion.RoundTrip(dst, src)
			if err != nil {
				t.Fatalf("RoundTrip failed: %v", err)
			}

			// Verify: newField should be restored from annotation in dst
			params, err := dst.GetParameters()
			if err != nil {
				t.Fatalf("Failed to get parameters: %v", err)
			}

			newFieldValue, ok := params["newField"]
			if !ok {
				t.Fatalf("Expected newField to be present in parameters, got: %+v", params)
			}

			if newFieldValue != "restored-value" {
				t.Errorf("Expected newField='restored-value', got %v", newFieldValue)
			}
		})

		t.Run("v1beta1_to_v1alpha1_field_to_annotation", func(t *testing.T) {
			// v1beta1 object with newField value
			src := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1beta1",
					Kind:       "TestResource",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{
						"newField": "store-me",
					},
				},
			}

			// v1alpha1 destination
			dst := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1alpha1",
					Kind:       "TestResource",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{},
				},
			}

			// Execute conversion
			err := ujconversion.RoundTrip(dst, src)
			if err != nil {
				t.Fatalf("RoundTrip failed: %v", err)
			}

			// Verify: newField value should be stored in annotation
			annotations := dst.GetAnnotations()
			if annotations == nil {
				t.Fatal("Expected annotations to exist")
			}

			annotationValue, ok := annotations["internal.upjet.crossplane.io/field-conversions"]
			if !ok {
				t.Fatalf("Expected field-conversions annotation to exist, got annotations: %+v", annotations)
			}

			// Parse and verify
			expectedJSON := `{"spec.forProvider.newField":"store-me"}`
			if diff := cmp.Diff(expectedJSON, annotationValue, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Annotation value mismatch (-want +got):\n%s", diff)
			}

			// Verify: newField should NOT be in dst parameters (it doesn't exist in v1alpha1)
			params, err := dst.GetParameters()
			if err != nil {
				t.Fatalf("Failed to get parameters: %v", err)
			}
			if _, ok := params["newField"]; ok {
				t.Error("newField should not be present in v1alpha1 parameters")
			}
		})

		t.Run("roundtrip_preserves_data", func(t *testing.T) {
			// Start with v1beta1
			original := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1beta1",
					Kind:       "TestResource",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{
						"newField": "original-value",
					},
				},
			}

			// Convert to v1alpha1
			v1alpha1 := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1alpha1",
					Kind:       "TestResource",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{},
				},
			}

			err := ujconversion.RoundTrip(v1alpha1, original)
			if err != nil {
				t.Fatalf("First RoundTrip failed: %v", err)
			}

			// Verify annotation exists in v1alpha1
			annotations := v1alpha1.GetAnnotations()
			if annotations == nil {
				t.Fatal("Expected annotations to exist in v1alpha1")
			}
			if _, ok := annotations["internal.upjet.crossplane.io/field-conversions"]; !ok {
				t.Fatal("Expected field-conversions annotation in v1alpha1")
			}

			// Convert back to v1beta1
			v1beta1 := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1beta1",
					Kind:       "TestResource",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{},
				},
			}

			err = ujconversion.RoundTrip(v1beta1, v1alpha1)
			if err != nil {
				t.Fatalf("Second RoundTrip failed: %v", err)
			}

			// Verify: newField should be preserved
			params, err := v1beta1.GetParameters()
			if err != nil {
				t.Fatalf("Failed to get parameters: %v", err)
			}

			newFieldValue, ok := params["newField"]
			if !ok {
				t.Fatalf("Expected newField to be present after roundtrip, got: %+v", params)
			}

			if newFieldValue != "original-value" {
				t.Errorf("Expected newField='original-value' after roundtrip, got %v", newFieldValue)
			}
		})
	})

	// ============================================================================
	// TEST SUITE 2: Type Conversions
	// ============================================================================

	t.Run("TypeConversions", func(t *testing.T) {
		t.Run("StringToNumber", func(t *testing.T) {
			t.Run("v1alpha1_string_to_v1beta1_number", func(t *testing.T) {
				// v1alpha1 has count as string
				src := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1alpha1",
						Kind:       "StringToNumberResource",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{
							"count": "42", // string in v1alpha1
						},
					},
				}
				src.TerraformResourceType = "string_to_number_resource"

				// v1beta1 should have count as number
				dst := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1beta1",
						Kind:       "StringToNumberResource",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{},
					},
				}
				dst.TerraformResourceType = "string_to_number_resource"

				err := ujconversion.RoundTrip(dst, src)
				if err != nil {
					t.Fatalf("RoundTrip failed: %v", err)
				}

				params, err := dst.GetParameters()
				if err != nil {
					t.Fatalf("Failed to get parameters: %v", err)
				}

				countValue, ok := params["count"]
				if !ok {
					t.Fatalf("Expected count to be present, got: %+v", params)
				}

				// Should be converted to numeric type (int64 or float64)
				var numValue float64
				switch v := countValue.(type) {
				case float64:
					numValue = v
				case int64:
					numValue = float64(v)
				case int:
					numValue = float64(v)
				default:
					t.Errorf("Expected count to be numeric, got %T: %v", countValue, countValue)
				}
				if numValue != 42 {
					t.Errorf("Expected count=42, got %v", numValue)
				}
			})

			t.Run("v1beta1_number_to_v1alpha1_string", func(t *testing.T) {
				// v1beta1 has count as number
				src := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1beta1",
						Kind:       "StringToNumberResource",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{
							"count": 42, // number in v1beta1
						},
					},
				}
				src.TerraformResourceType = "string_to_number_resource"

				// v1alpha1 should have count as string
				dst := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1alpha1",
						Kind:       "StringToNumberResource",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{},
					},
				}
				dst.TerraformResourceType = "string_to_number_resource"

				err := ujconversion.RoundTrip(dst, src)
				if err != nil {
					t.Fatalf("RoundTrip failed: %v", err)
				}

				params, err := dst.GetParameters()
				if err != nil {
					t.Fatalf("Failed to get parameters: %v", err)
				}

				countValue, ok := params["count"]
				if !ok {
					t.Fatalf("Expected count to be present, got: %+v", params)
				}

				// Should be converted to string
				if countStr, ok := countValue.(string); !ok {
					t.Errorf("Expected count to be string, got %T: %v", countValue, countValue)
				} else if countStr != "42" {
					t.Errorf("Expected count=\"42\", got %q", countStr)
				}
			})

			t.Run("roundtrip_preserves_numeric_value", func(t *testing.T) {
				// Start with v1beta1 (number)
				original := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1beta1",
						Kind:       "StringToNumberResource",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{
							"count": 123,
						},
					},
				}
				original.TerraformResourceType = "string_to_number_resource"

				// Convert to v1alpha1 (string)
				v1alpha1 := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1alpha1",
						Kind:       "StringToNumberResource",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{},
					},
				}
				v1alpha1.TerraformResourceType = "string_to_number_resource"

				err := ujconversion.RoundTrip(v1alpha1, original)
				if err != nil {
					t.Fatalf("First RoundTrip failed: %v", err)
				}

				// Convert back to v1beta1 (number)
				v1beta1 := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1beta1",
						Kind:       "StringToNumberResource",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{},
					},
				}
				v1beta1.TerraformResourceType = "string_to_number_resource"

				err = ujconversion.RoundTrip(v1beta1, v1alpha1)
				if err != nil {
					t.Fatalf("Second RoundTrip failed: %v", err)
				}

				params, err := v1beta1.GetParameters()
				if err != nil {
					t.Fatalf("Failed to get parameters: %v", err)
				}

				countValue, ok := params["count"]
				if !ok {
					t.Fatalf("Expected count after roundtrip, got: %+v", params)
				}

				var numValue float64
				switch v := countValue.(type) {
				case float64:
					numValue = v
				case int64:
					numValue = float64(v)
				case int:
					numValue = float64(v)
				default:
					t.Errorf("Expected count to be numeric after roundtrip, got %T", countValue)
				}
				if numValue != 123 {
					t.Errorf("Expected count=123 after roundtrip, got %v", numValue)
				}
			})
		})

		t.Run("NumberToString", func(t *testing.T) {
			t.Run("v1alpha1_number_to_v1beta1_string", func(t *testing.T) {
				// v1alpha1 has value as number
				src := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1alpha1",
						Kind:       "NumberToStringResource",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{
							"value": 999,
						},
					},
				}
				src.TerraformResourceType = "number_to_string_resource"

				// v1beta1 should have value as string
				dst := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1beta1",
						Kind:       "NumberToStringResource",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{},
					},
				}
				dst.TerraformResourceType = "number_to_string_resource"

				err := ujconversion.RoundTrip(dst, src)
				if err != nil {
					t.Fatalf("RoundTrip failed: %v", err)
				}

				params, err := dst.GetParameters()
				if err != nil {
					t.Fatalf("Failed to get parameters: %v", err)
				}

				valueValue, ok := params["value"]
				if !ok {
					t.Fatalf("Expected value to be present, got: %+v", params)
				}

				// Should be converted to string
				if valueStr, ok := valueValue.(string); !ok {
					t.Errorf("Expected value to be string, got %T: %v", valueValue, valueValue)
				} else if valueStr != "999" {
					t.Errorf("Expected value=\"999\", got %q", valueStr)
				}
			})
		})

		t.Run("StringToBool", func(t *testing.T) {
			t.Run("v1alpha1_string_true_to_v1beta1_bool", func(t *testing.T) {
				src := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1alpha1",
						Kind:       "StringToBoolResource",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{
							"enabled": "true",
						},
					},
				}
				src.TerraformResourceType = "string_to_bool_resource"

				dst := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1beta1",
						Kind:       "StringToBoolResource",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{},
					},
				}
				dst.TerraformResourceType = "string_to_bool_resource"

				err := ujconversion.RoundTrip(dst, src)
				if err != nil {
					t.Fatalf("RoundTrip failed: %v", err)
				}

				params, err := dst.GetParameters()
				if err != nil {
					t.Fatalf("Failed to get parameters: %v", err)
				}

				enabledValue, ok := params["enabled"]
				if !ok {
					t.Fatalf("Expected enabled to be present, got: %+v", params)
				}

				if boolVal, ok := enabledValue.(bool); !ok {
					t.Errorf("Expected enabled to be bool, got %T: %v", enabledValue, enabledValue)
				} else if boolVal != true {
					t.Errorf("Expected enabled=true, got %v", boolVal)
				}
			})

			t.Run("v1alpha1_string_false_to_v1beta1_bool", func(t *testing.T) {
				src := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1alpha1",
						Kind:       "StringToBoolResource",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{
							"enabled": "false",
						},
					},
				}
				src.TerraformResourceType = "string_to_bool_resource"

				dst := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1beta1",
						Kind:       "StringToBoolResource",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{},
					},
				}
				dst.TerraformResourceType = "string_to_bool_resource"

				err := ujconversion.RoundTrip(dst, src)
				if err != nil {
					t.Fatalf("RoundTrip failed: %v", err)
				}

				params, err := dst.GetParameters()
				if err != nil {
					t.Fatalf("Failed to get parameters: %v", err)
				}

				enabledValue, ok := params["enabled"]
				if !ok {
					t.Fatalf("Expected enabled to be present, got: %+v", params)
				}

				if boolVal, ok := enabledValue.(bool); !ok {
					t.Errorf("Expected enabled to be bool, got %T: %v", enabledValue, enabledValue)
				} else if boolVal != false {
					t.Errorf("Expected enabled=false, got %v", boolVal)
				}
			})
		})

		t.Run("BoolToString", func(t *testing.T) {
			t.Run("v1alpha1_bool_true_to_v1beta1_string", func(t *testing.T) {
				src := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1alpha1",
						Kind:       "BoolToStringResource",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{
							"flag": true,
						},
					},
				}
				src.TerraformResourceType = "bool_to_string_resource"

				dst := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1beta1",
						Kind:       "BoolToStringResource",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{},
					},
				}
				dst.TerraformResourceType = "bool_to_string_resource"

				err := ujconversion.RoundTrip(dst, src)
				if err != nil {
					t.Fatalf("RoundTrip failed: %v", err)
				}

				params, err := dst.GetParameters()
				if err != nil {
					t.Fatalf("Failed to get parameters: %v", err)
				}

				flagValue, ok := params["flag"]
				if !ok {
					t.Fatalf("Expected flag to be present, got: %+v", params)
				}

				if flagStr, ok := flagValue.(string); !ok {
					t.Errorf("Expected flag to be string, got %T: %v", flagValue, flagValue)
				} else if flagStr != "true" {
					t.Errorf("Expected flag=\"true\", got %q", flagStr)
				}
			})

			t.Run("v1alpha1_bool_false_to_v1beta1_string", func(t *testing.T) {
				src := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1alpha1",
						Kind:       "BoolToStringResource",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{
							"flag": false,
						},
					},
				}
				src.TerraformResourceType = "bool_to_string_resource"

				dst := &TestResource{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "test.example.io/v1beta1",
						Kind:       "BoolToStringResource",
					},
					Spec: TestResourceSpec{
						ForProvider: TestResourceParameters{},
					},
				}
				dst.TerraformResourceType = "bool_to_string_resource"

				err := ujconversion.RoundTrip(dst, src)
				if err != nil {
					t.Fatalf("RoundTrip failed: %v", err)
				}

				params, err := dst.GetParameters()
				if err != nil {
					t.Fatalf("Failed to get parameters: %v", err)
				}

				flagValue, ok := params["flag"]
				if !ok {
					t.Fatalf("Expected flag to be present, got: %+v", params)
				}

				if flagStr, ok := flagValue.(string); !ok {
					t.Errorf("Expected flag to be string, got %T: %v", flagValue, flagValue)
				} else if flagStr != "false" {
					t.Errorf("Expected flag=\"false\", got %q", flagStr)
				}
			})
		})
	})

	// ============================================================================
	// TEST SUITE 3: Multiple Changes
	// ============================================================================

	t.Run("MultipleChanges", func(t *testing.T) {
		t.Run("multiple_changes_v1alpha1_to_v1beta1", func(t *testing.T) {
			// v1alpha1: count=string, enabled=string, newField doesn't exist
			src := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1alpha1",
					Kind:       "MultiChangeResource",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						// newField stored in annotation
						"internal.upjet.crossplane.io/field-conversions": `{"spec.forProvider.newField":"annotation-value"}`,
					},
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{
						"count":   "42",   // string -> will convert to int
						"enabled": "true", // string -> will convert to bool
					},
				},
			}
			src.TerraformResourceType = "multi_change_resource"

			// v1beta1: count=int, enabled=bool, newField=string
			dst := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1beta1",
					Kind:       "MultiChangeResource",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{},
				},
			}
			dst.TerraformResourceType = "multi_change_resource"

			err := ujconversion.RoundTrip(dst, src)
			if err != nil {
				t.Fatalf("RoundTrip failed: %v", err)
			}

			params, err := dst.GetParameters()
			if err != nil {
				t.Fatalf("Failed to get parameters: %v", err)
			}

			// Verify count converted to number
			countValue, ok := params["count"]
			if !ok {
				t.Errorf("Expected count to be present, got: %+v", params)
			} else {
				var numValue float64
				switch v := countValue.(type) {
				case float64:
					numValue = v
				case int64:
					numValue = float64(v)
				case int:
					numValue = float64(v)
				default:
					t.Errorf("Expected count to be numeric, got %T: %v", countValue, countValue)
				}
				if numValue != 42 {
					t.Errorf("Expected count=42, got %v", numValue)
				}
			}

			// Verify enabled converted to bool
			enabledValue, ok := params["enabled"]
			if !ok {
				t.Errorf("Expected enabled to be present, got: %+v", params)
			} else {
				if boolVal, ok := enabledValue.(bool); !ok {
					t.Errorf("Expected enabled to be bool, got %T: %v", enabledValue, enabledValue)
				} else if boolVal != true {
					t.Errorf("Expected enabled=true, got %v", boolVal)
				}
			}

			// Verify newField restored from annotation
			newFieldValue, ok := params["newField"]
			if !ok {
				t.Errorf("Expected newField to be restored from annotation, got: %+v", params)
			} else {
				if strVal, ok := newFieldValue.(string); !ok {
					t.Errorf("Expected newField to be string, got %T: %v", newFieldValue, newFieldValue)
				} else if strVal != "annotation-value" {
					t.Errorf("Expected newField='annotation-value', got %q", strVal)
				}
			}
		})

		t.Run("multiple_changes_v1beta1_to_v1alpha1", func(t *testing.T) {
			// v1beta1: count=int, enabled=bool, newField=string
			src := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1beta1",
					Kind:       "MultiChangeResource",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{
						"count":    99,
						"enabled":  false,
						"newField": "field-value",
					},
				},
			}
			src.TerraformResourceType = "multi_change_resource"

			// v1alpha1: count=string, enabled=string, newField doesn't exist
			dst := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1alpha1",
					Kind:       "MultiChangeResource",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{},
				},
			}
			dst.TerraformResourceType = "multi_change_resource"

			err := ujconversion.RoundTrip(dst, src)
			if err != nil {
				t.Fatalf("RoundTrip failed: %v", err)
			}

			params, err := dst.GetParameters()
			if err != nil {
				t.Fatalf("Failed to get parameters: %v", err)
			}

			// Verify count converted to string
			countValue, ok := params["count"]
			if !ok {
				t.Errorf("Expected count to be present, got: %+v", params)
			} else {
				if strVal, ok := countValue.(string); !ok {
					t.Errorf("Expected count to be string, got %T: %v", countValue, countValue)
				} else if strVal != "99" {
					t.Errorf("Expected count=\"99\", got %q", strVal)
				}
			}

			// Verify enabled converted to string
			enabledValue, ok := params["enabled"]
			if !ok {
				t.Errorf("Expected enabled to be present, got: %+v", params)
			} else {
				if strVal, ok := enabledValue.(string); !ok {
					t.Errorf("Expected enabled to be string, got %T: %v", enabledValue, enabledValue)
				} else if strVal != "false" {
					t.Errorf("Expected enabled=\"false\", got %q", strVal)
				}
			}

			// Verify newField NOT in parameters (stored in annotation)
			if _, ok := params["newField"]; ok {
				t.Error("newField should not be in v1alpha1 parameters, should be in annotation")
			}

			// Verify newField stored in annotation
			annotations := dst.GetAnnotations()
			if annotations == nil {
				t.Fatal("Expected annotations to exist")
			}

			annotationValue, ok := annotations["internal.upjet.crossplane.io/field-conversions"]
			if !ok {
				t.Errorf("Expected field-conversions annotation, got: %+v", annotations)
			} else {
				expectedJSON := `{"spec.forProvider.newField":"field-value"}`
				if diff := cmp.Diff(expectedJSON, annotationValue, cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("Annotation value mismatch (-want +got):\n%s", diff)
				}
			}
		})

		t.Run("roundtrip_with_multiple_changes", func(t *testing.T) {
			// Start with v1beta1 with all fields
			original := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1beta1",
					Kind:       "MultiChangeResource",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{
						"count":    123,
						"enabled":  true,
						"newField": "original-value",
					},
				},
			}
			original.TerraformResourceType = "multi_change_resource"

			// Convert to v1alpha1
			v1alpha1 := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1alpha1",
					Kind:       "MultiChangeResource",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{},
				},
			}
			v1alpha1.TerraformResourceType = "multi_change_resource"

			err := ujconversion.RoundTrip(v1alpha1, original)
			if err != nil {
				t.Fatalf("First RoundTrip failed: %v", err)
			}

			// Convert back to v1beta1
			v1beta1 := &TestResource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "test.example.io/v1beta1",
					Kind:       "MultiChangeResource",
				},
				Spec: TestResourceSpec{
					ForProvider: TestResourceParameters{},
				},
			}
			v1beta1.TerraformResourceType = "multi_change_resource"

			err = ujconversion.RoundTrip(v1beta1, v1alpha1)
			if err != nil {
				t.Fatalf("Second RoundTrip failed: %v", err)
			}

			// Verify all fields preserved with correct types
			params, err := v1beta1.GetParameters()
			if err != nil {
				t.Fatalf("Failed to get parameters: %v", err)
			}

			// Check count
			countValue, ok := params["count"]
			if !ok {
				t.Error("Expected count after roundtrip")
			} else {
				var numValue float64
				switch v := countValue.(type) {
				case float64:
					numValue = v
				case int64:
					numValue = float64(v)
				case int:
					numValue = float64(v)
				}
				if numValue != 123 {
					t.Errorf("Expected count=123 after roundtrip, got %v", numValue)
				}
			}

			// Check enabled
			enabledValue, ok := params["enabled"]
			if !ok {
				t.Error("Expected enabled after roundtrip")
			} else {
				if boolVal, ok := enabledValue.(bool); !ok || boolVal != true {
					t.Errorf("Expected enabled=true after roundtrip, got %v", enabledValue)
				}
			}

			// Check newField
			newFieldValue, ok := params["newField"]
			if !ok {
				t.Error("Expected newField after roundtrip")
			} else {
				if strVal, ok := newFieldValue.(string); !ok || strVal != "original-value" {
					t.Errorf("Expected newField='original-value' after roundtrip, got %v", newFieldValue)
				}
			}
		})
	})
}
