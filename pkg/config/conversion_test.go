// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestRegisterAutoConversions(t *testing.T) {
	type args struct {
		crdSchemaChanges []byte
		setupResource    func() *Resource
	}
	type want struct {
		conversionCount int
		err             bool
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"FieldAddition": {
			reason: "Field addition should register 2 bidirectional conversions",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/field-addition.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				conversionCount: 2,
				err:             false,
			},
		},
		"FieldDeletion": {
			reason: "Field deletion should register 2 bidirectional conversions (reversed)",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/field-deletion.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				conversionCount: 2,
				err:             false,
			},
		},
		"TypeChangeStringToInt": {
			reason: "String to number type change should register int conversions when TF schema type is int",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-number.json"),
				setupResource: func() *Resource {
					return newTestResourceWithIntField("TestResource", "count")
				},
			},
			want: want{
				conversionCount: 2,
				err:             false,
			},
		},
		"TypeChangeStringToFloat": {
			reason: "String to number type change should register float conversions when TF schema type is float",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-number.json"),
				setupResource: func() *Resource {
					return newTestResourceWithFloatField("TestResource", "count")
				},
			},
			want: want{
				conversionCount: 2,
				err:             false,
			},
		},
		"TypeChangeStringToBoolean": {
			reason: "String to boolean type change should register boolean conversions",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-bool.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				conversionCount: 2,
				err:             false,
			},
		},
		"MultipleChanges": {
			reason: "Multiple changes (addition + deletion + type change) should register 6 conversions total",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/multiple-changes.json"),
				setupResource: func() *Resource {
					return newTestResourceWithIntField("TestResource", "count")
				},
			},
			want: want{
				conversionCount: 6,
				err:             false,
			},
		},
		"SkipAutoRegistration": {
			reason: "Resources with SkipAutoRegistration=true should not register any conversions",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/field-addition.json"),
				setupResource: func() *Resource {
					r := newTestResource("TestResource")
					r.AutoConversionRegistrationOptions.SkipAutoRegistration = true
					return r
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"ExcludeSpecificPath": {
			reason: "Paths in AutoRegisterExcludePaths should not register conversions",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/field-addition.json"),
				setupResource: func() *Resource {
					r := newTestResource("TestResource")
					r.AutoConversionRegistrationOptions.AutoRegisterExcludePaths = []string{
						"spec.forProvider.newField",
					}
					return r
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"ResourceNotInJSON": {
			reason: "Resources not present in JSON should not register conversions",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/field-addition.json"),
				setupResource: func() *Resource {
					return newTestResource("DifferentResource")
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"MalformedJSON": {
			reason: "Malformed JSON should return an error",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "invalid/malformed.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				err: true,
			},
		},
		"EmptyJSON": {
			reason: "Empty JSON object should not register any conversions and not error",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/empty.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"NoChanges": {
			reason: "Resource with empty changes array should not register conversions",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/no-changes.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"NilTerraformResource": {
			reason: "Type change with nil TerraformResource should skip conversion gracefully",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-number.json"),
				setupResource: func() *Resource {
					r := newTestResource("TestResource")
					r.TerraformResource = nil
					return r
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"NilTerraformResourceSchema": {
			reason: "Type change with nil TerraformResource Schema should skip conversion gracefully",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-number.json"),
				setupResource: func() *Resource {
					r := newTestResource("TestResource")
					r.TerraformResource.Schema = nil
					return r
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"UnknownChangeType": {
			reason: "Unknown changeType values should be silently skipped",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "invalid/unknown-change-type.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"MissingChangeType": {
			reason: "Changes with missing changeType field should be silently skipped",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "invalid/missing-change-type.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"TypeChangeMissingValues": {
			reason: "Type changes without oldValue/newValue should be silently skipped",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "invalid/type-change-missing-values.json"),
				setupResource: func() *Resource {
					return newTestResourceWithIntField("TestResource", "count")
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"TypeChangeSameValues": {
			reason: "Type changes with same oldValue and newValue should be silently skipped",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "invalid/type-change-same-values.json"),
				setupResource: func() *Resource {
					return newTestResourceWithStringField("TestResource", "count")
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"TypeChangeUnsupported": {
			reason: "Unsupported type conversions (e.g., array->string) should be silently skipped",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "invalid/type-change-unsupported.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"ThreeVersions": {
			reason: "Multiple version transitions should register all conversions correctly",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/three-versions.json"),
				setupResource: func() *Resource {
					return newTestResourceWithIntField("TestResource", "count")
				},
			},
			want: want{
				conversionCount: 8, // v1beta1: 2 (field_added) + 2 (type_changed) + v1beta2: 2 (field_deleted) + 2 (field_added)
				err:             false,
			},
		},
		"EvolvingField": {
			reason: "Field that changes type multiple times should register conversions for each transition",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/evolving-field.json"),
				setupResource: func() *Resource {
					return newTestResourceWithIntField("TestResource", "value")
				},
			},
			want: want{
				conversionCount: 2, // v1beta1: 2 (string->number) + v1beta2: skipped (number->boolean not supported with int schema)
				err:             false,
			},
		},
		"DeeplyNested": {
			reason: "Deeply nested paths should be handled correctly",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/deeply-nested.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				conversionCount: 6, // 2 field_added + 2 field_deleted + 2 field_added (type_changed skipped due to no schema)
				err:             false,
			},
		},
		"TypeChangeNumberToString": {
			reason: "Number to string type change should register conversions (defaults to int)",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-number-to-string.json"),
				setupResource: func() *Resource {
					return newTestResourceWithStringField("TestResource", "count")
				},
			},
			want: want{
				conversionCount: 2, // IntToString and StringToInt
				err:             false,
			},
		},
		"TypeChangeBooleanToString": {
			reason: "Boolean to string type change should register boolean conversions",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-bool-to-string.json"),
				setupResource: func() *Resource {
					return newTestResourceWithStringField("TestResource", "enabled")
				},
			},
			want: want{
				conversionCount: 2, // BoolToString and StringToBool
				err:             false,
			},
		},
		"TypeChangeSchemaMismatch": {
			reason: "Type change with mismatched TF schema type should return an error",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "invalid/type-change-bool-schema-mismatch.json"),
				setupResource: func() *Resource {
					return newTestResourceWithBoolField("TestResource", "count")
				},
			},
			want: want{
				conversionCount: 0,
				err:             true, // Should error on schema type mismatch
			},
		},
		"TypeChangeSchemaNotFound": {
			reason: "Type change when field is not in TF schema should skip conversion",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-number.json"),
				setupResource: func() *Resource {
					return newTestResourceWithIntField("TestResource", "differentField")
				},
			},
			want: want{
				conversionCount: 0, // Field "count" not in schema, skip
				err:             false,
			},
		},
		"ExcludeMultiplePaths": {
			reason: "Multiple paths in AutoRegisterExcludePaths should all be excluded",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/exclude-multiple-paths.json"),
				setupResource: func() *Resource {
					r := newTestResource("TestResource")
					r.AutoConversionRegistrationOptions.AutoRegisterExcludePaths = []string{
						"spec.forProvider.field1",
						"spec.forProvider.field2",
					}
					return r
				},
			},
			want: want{
				conversionCount: 2, // Only field3 registered (field1 and field2 excluded)
				err:             false,
			},
		},
		"ExcludeAllPaths": {
			reason: "Excluding all paths should result in no conversions",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/exclude-multiple-paths.json"),
				setupResource: func() *Resource {
					r := newTestResource("TestResource")
					r.AutoConversionRegistrationOptions.AutoRegisterExcludePaths = []string{
						"spec.forProvider.field1",
						"spec.forProvider.field2",
						"spec.forProvider.field3",
					}
					return r
				},
			},
			want: want{
				conversionCount: 0,
				err:             false,
			},
		},
		"ExcludeNonExistentPath": {
			reason: "Excluding non-existent paths should not affect actual changes",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/field-addition.json"),
				setupResource: func() *Resource {
					r := newTestResource("TestResource")
					r.AutoConversionRegistrationOptions.AutoRegisterExcludePaths = []string{
						"spec.forProvider.nonExistentField",
					}
					return r
				},
			},
			want: want{
				conversionCount: 2, // newField is still registered
				err:             false,
			},
		},
		"ExcludePathsWithSkipAutoRegistration": {
			reason: "SkipAutoRegistration should take precedence over AutoRegisterExcludePaths",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/field-addition.json"),
				setupResource: func() *Resource {
					r := newTestResource("TestResource")
					r.AutoConversionRegistrationOptions.SkipAutoRegistration = true
					r.AutoConversionRegistrationOptions.AutoRegisterExcludePaths = []string{
						"spec.forProvider.newField",
					}
					return r
				},
			},
			want: want{
				conversionCount: 0, // SkipAutoRegistration means nothing is registered
				err:             false,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Setup provider with test resource
			pc := newTestProvider(t)
			testResource := tc.args.setupResource()
			pc.Resources["TestResource"] = testResource

			// Run auto-registration
			err := RegisterAutoConversions(pc, tc.args.crdSchemaChanges)

			// Check error expectation
			if tc.want.err {
				if err == nil {
					t.Errorf("\n%s\nRegisterAutoConversions(...): expected error, got nil", tc.reason)
				}
				return
			}

			if err != nil {
				t.Errorf("\n%s\nRegisterAutoConversions(...): unexpected error: %v", tc.reason, err)
				return
			}

			// Check conversion count
			gotCount := len(testResource.Conversions)
			if diff := cmp.Diff(tc.want.conversionCount, gotCount); diff != "" {
				t.Errorf("\n%s\nRegisterAutoConversions(...): conversion count -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestRegisterAutoConversionsMultipleResources(t *testing.T) {
	crdSchemaChanges := loadTestFixture(t, "valid/multiple-resources.json")
	pc := newTestProvider(t)

	// Setup multiple resources with different short groups
	pc.Resources["ResourceOne"] = newTestResource("ResourceOne")
	pc.Resources["ResourceTwo"] = newTestResourceWithIntField("ResourceTwo", "count")
	pc.Resources["ResourceThree"] = newTestResource("ResourceThree")

	// ResourceFour has a different short group
	r4 := newTestResource("ResourceFour")
	r4.ShortGroup = "other"
	pc.Resources["ResourceFour"] = r4

	// ResourceFive has no changes
	pc.Resources["ResourceFive"] = newTestResource("ResourceFive")

	// ResourceSix not in JSON at all
	pc.Resources["ResourceSix"] = newTestResource("ResourceSix")

	err := RegisterAutoConversions(pc, crdSchemaChanges)
	if err != nil {
		t.Fatalf("RegisterAutoConversions(...): unexpected error: %v", err)
	}

	// Verify each resource has correct number of conversions
	cases := map[string]struct {
		resourceName  string
		expectedCount int
		reason        string
	}{
		"ResourceOne": {
			resourceName:  "ResourceOne",
			expectedCount: 2,
			reason:        "ResourceOne has 1 field addition = 2 conversions",
		},
		"ResourceTwo": {
			resourceName:  "ResourceTwo",
			expectedCount: 2,
			reason:        "ResourceTwo has 1 type change = 2 conversions",
		},
		"ResourceThree": {
			resourceName:  "ResourceThree",
			expectedCount: 2,
			reason:        "ResourceThree has 1 field deletion = 2 conversions",
		},
		"ResourceFour": {
			resourceName:  "ResourceFour",
			expectedCount: 4,
			reason:        "ResourceFour has 1 field addition + 1 type change = 4 conversions",
		},
		"ResourceFive": {
			resourceName:  "ResourceFive",
			expectedCount: 0,
			reason:        "ResourceFive has no changes = 0 conversions",
		},
		"ResourceSix": {
			resourceName:  "ResourceSix",
			expectedCount: 0,
			reason:        "ResourceSix not in JSON = 0 conversions",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := pc.Resources[tc.resourceName]
			gotCount := len(r.Conversions)
			if diff := cmp.Diff(tc.expectedCount, gotCount); diff != "" {
				t.Errorf("\n%s\nconversion count -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestExcludeTypeChangesFromIdentity(t *testing.T) {
	type args struct {
		crdSchemaChanges []byte
		setupResource    func() *Resource
	}
	type want struct {
		identityExcludePaths []string
		err                  bool
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"TypeChangeAdded": {
			reason: "Type changed fields should be added to IdentityConversionExcludePaths",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-number.json"),
				setupResource: func() *Resource {
					return newTestResourceWithIntField("TestResource", "count")
				},
			},
			want: want{
				identityExcludePaths: []string{"count"},
				err:                  false,
			},
		},
		"OnlyTypeChanges": {
			reason: "Only type changes should be excluded, not field additions or deletions",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/multiple-changes.json"),
				setupResource: func() *Resource {
					return newTestResourceWithIntField("TestResource", "count")
				},
			},
			want: want{
				identityExcludePaths: []string{"count"}, // Only the type-changed field
				err:                  false,
			},
		},
		"EmptyWhenNoTypeChanges": {
			reason: "Resources with only field additions should not add exclude paths",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/field-addition.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				identityExcludePaths: []string{},
				err:                  false,
			},
		},
		"ExcludedPathNotAdded": {
			reason: "Paths in AutoRegisterExcludePaths should not be added to identity exclude paths",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-number.json"),
				setupResource: func() *Resource {
					r := newTestResourceWithIntField("TestResource", "count")
					r.AutoConversionRegistrationOptions.AutoRegisterExcludePaths = []string{
						"spec.forProvider.count",
					}
					return r
				},
			},
			want: want{
				identityExcludePaths: []string{},
				err:                  false,
			},
		},
		"EmptyJSON": {
			reason: "Empty JSON object should not add any exclude paths and not error",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/empty.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				identityExcludePaths: []string{},
				err:                  false,
			},
		},
		"NoChanges": {
			reason: "Resource with empty changes array should not add exclude paths",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/no-changes.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				identityExcludePaths: []string{},
				err:                  false,
			},
		},
		"MalformedJSON": {
			reason: "Malformed JSON should return an error",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "invalid/malformed.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				err: true,
			},
		},
		"MultipleTypeChanges": {
			reason: "Multiple type changes in same resource should add all to exclude paths",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/multiple-type-changes.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				identityExcludePaths: []string{"field1", "field2", "field3"},
				err:                  false,
			},
		},
		"TypeChangesAcrossVersions": {
			reason: "Type changes across different versions should be deduplicated",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/evolving-field.json"),
				setupResource: func() *Resource {
					return newTestResource("TestResource")
				},
			},
			want: want{
				identityExcludePaths: []string{"value"}, // Only one entry even though field changes in multiple versions
				err:                  false,
			},
		},
		"InteractionWithAutoRegisterExcludePaths": {
			reason: "AutoRegisterExcludePaths should prevent paths from being added to identity exclude list",
			args: args{
				crdSchemaChanges: loadTestFixture(t, "valid/multiple-type-changes.json"),
				setupResource: func() *Resource {
					r := newTestResource("TestResource")
					r.AutoConversionRegistrationOptions.AutoRegisterExcludePaths = []string{
						"spec.forProvider.field1",
						"spec.initProvider.field3",
					}
					return r
				},
			},
			want: want{
				identityExcludePaths: []string{"field2"}, // field1 and field3 excluded by AutoRegisterExcludePaths
				err:                  false,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Setup provider with test resource
			pc := newTestProvider(t)
			testResource := tc.args.setupResource()
			pc.Resources["TestResource"] = testResource

			// Run exclusion
			err := ExcludeTypeChangesFromIdentity(pc, tc.args.crdSchemaChanges)

			// Check error expectation
			if tc.want.err {
				if err == nil {
					t.Errorf("\n%s\nExcludeTypeChangesFromIdentity(...): expected error, got nil", tc.reason)
				}
				return
			}

			if err != nil {
				t.Errorf("\n%s\nExcludeTypeChangesFromIdentity(...): unexpected error: %v", tc.reason, err)
				return
			}

			// Check identity exclude paths
			got := testResource.AutoConversionRegistrationOptions.IdentityConversionExcludePaths
			if diff := cmp.Diff(tc.want.identityExcludePaths, got, cmpopts.EquateEmpty(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("\n%s\nExcludeTypeChangesFromIdentity(...): IdentityConversionExcludePaths -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestIsExcludedPath(t *testing.T) {
	type args struct {
		path         string
		excludePaths []string
	}
	type want struct {
		excluded bool
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"ExactMatch": {
			reason: "Path that exactly matches an exclude path should be excluded",
			args: args{
				path:         "spec.forProvider.field",
				excludePaths: []string{"spec.forProvider.field"},
			},
			want: want{
				excluded: true,
			},
		},
		"NoMatch": {
			reason: "Path that doesn't match any exclude path should not be excluded",
			args: args{
				path:         "spec.forProvider.field",
				excludePaths: []string{"spec.forProvider.other"},
			},
			want: want{
				excluded: false,
			},
		},
		"EmptyExcludeList": {
			reason: "Empty exclude list should not exclude any path",
			args: args{
				path:         "spec.forProvider.field",
				excludePaths: []string{},
			},
			want: want{
				excluded: false,
			},
		},
		"NilExcludeList": {
			reason: "Nil exclude list should not exclude any path",
			args: args{
				path:         "spec.forProvider.field",
				excludePaths: nil,
			},
			want: want{
				excluded: false,
			},
		},
		"EmptyPath": {
			reason: "Empty path should not match anything",
			args: args{
				path:         "",
				excludePaths: []string{"spec.forProvider.field"},
			},
			want: want{
				excluded: false,
			},
		},
		"PartialMatch": {
			reason: "Partial path match should not be excluded (must be exact match)",
			args: args{
				path:         "spec.forProvider.fieldName",
				excludePaths: []string{"spec.forProvider.field"},
			},
			want: want{
				excluded: false,
			},
		},
		"MultiplePathsFirstMatch": {
			reason: "Path matching first item in list should be excluded",
			args: args{
				path: "spec.forProvider.field1",
				excludePaths: []string{
					"spec.forProvider.field1",
					"spec.forProvider.field2",
					"spec.forProvider.field3",
				},
			},
			want: want{
				excluded: true,
			},
		},
		"MultiplePathsMiddleMatch": {
			reason: "Path matching middle item in list should be excluded",
			args: args{
				path: "spec.forProvider.field2",
				excludePaths: []string{
					"spec.forProvider.field1",
					"spec.forProvider.field2",
					"spec.forProvider.field3",
				},
			},
			want: want{
				excluded: true,
			},
		},
		"MultiplePathsLastMatch": {
			reason: "Path matching last item in list should be excluded",
			args: args{
				path: "spec.forProvider.field3",
				excludePaths: []string{
					"spec.forProvider.field1",
					"spec.forProvider.field2",
					"spec.forProvider.field3",
				},
			},
			want: want{
				excluded: true,
			},
		},
		"CaseSensitive": {
			reason: "Path matching should be case sensitive",
			args: args{
				path:         "spec.forProvider.Field",
				excludePaths: []string{"spec.forProvider.field"},
			},
			want: want{
				excluded: false,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := isExcludedPath(tc.args.path, tc.args.excludePaths)
			if diff := cmp.Diff(tc.want.excluded, got); diff != "" {
				t.Errorf("\n%s\nisExcludedPath(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestSetIdentityConversionExcludePath(t *testing.T) {
	type args struct {
		fullPath string
		existing []string
	}
	type want struct {
		excludePaths []string
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"AddSinglePath": {
			reason: "Adding a single path should work correctly",
			args: args{
				fullPath: "spec.forProvider.field",
				existing: []string{},
			},
			want: want{
				excludePaths: []string{"field"},
			},
		},
		"AddDuplicatePath": {
			reason: "Adding duplicate path should not create duplicates",
			args: args{
				fullPath: "spec.forProvider.field",
				existing: []string{"field"},
			},
			want: want{
				excludePaths: []string{"field"},
			},
		},
		"AddMultipleUniquePaths": {
			reason: "Adding multiple unique paths should add all of them",
			args: args{
				fullPath: "spec.forProvider.field2",
				existing: []string{"field1"},
			},
			want: want{
				excludePaths: []string{"field1", "field2"},
			},
		},
		"EmptyPath": {
			reason: "Empty path after trimming should not be added",
			args: args{
				fullPath: "metadata.annotations.key",
				existing: []string{},
			},
			want: want{
				excludePaths: []string{},
			},
		},
		"InitProviderPath": {
			reason: "spec.initProvider paths should be trimmed correctly",
			args: args{
				fullPath: "spec.initProvider.field",
				existing: []string{},
			},
			want: want{
				excludePaths: []string{"field"},
			},
		},
		"AtProviderPath": {
			reason: "status.atProvider paths should be trimmed correctly",
			args: args{
				fullPath: "status.atProvider.field",
				existing: []string{},
			},
			want: want{
				excludePaths: []string{"field"},
			},
		},
		"NestedPath": {
			reason: "Nested paths should preserve structure after trimming prefix",
			args: args{
				fullPath: "spec.forProvider.nested.field.name",
				existing: []string{},
			},
			want: want{
				excludePaths: []string{"nested.field.name"},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newTestResource("TestResource")
			r.AutoConversionRegistrationOptions.IdentityConversionExcludePaths = tc.args.existing
			m := map[string]bool{}

			// Pre-populate map with existing paths
			for _, p := range tc.args.existing {
				m[p] = true
			}

			setIdentityConversionExcludePath(r, tc.args.fullPath, m)

			got := r.AutoConversionRegistrationOptions.IdentityConversionExcludePaths
			if diff := cmp.Diff(tc.want.excludePaths, got, cmpopts.EquateEmpty(), cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Errorf("\n%s\nsetIdentityConversionExcludePath(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestIntegrationBothFunctions(t *testing.T) {
	type want struct {
		conversionCount      int
		identityExcludeCount int
		err                  bool
	}

	cases := map[string]struct {
		reason           string
		crdSchemaChanges []byte
		setupResource    func() *Resource
		callOrder        string // "correct", "reversed"
		want             want
	}{
		"CorrectOrder": {
			reason:           "Calling ExcludeTypeChanges then RegisterAutoConversions should work correctly",
			crdSchemaChanges: loadTestFixture(t, "valid/multiple-changes.json"),
			setupResource: func() *Resource {
				return newTestResourceWithIntField("TestResource", "count")
			},
			callOrder: "correct",
			want: want{
				conversionCount:      6, // field_added + field_deleted + type_changed
				identityExcludeCount: 1, // Only count (type_changed)
				err:                  false,
			},
		},
		"ReversedOrder": {
			reason:           "Calling RegisterAutoConversions then ExcludeTypeChanges still works (order doesn't break functionality)",
			crdSchemaChanges: loadTestFixture(t, "valid/multiple-changes.json"),
			setupResource: func() *Resource {
				return newTestResourceWithIntField("TestResource", "count")
			},
			callOrder: "reversed",
			want: want{
				conversionCount:      6, // Still registers all conversions
				identityExcludeCount: 1, // Still excludes type-changed field
				err:                  false,
			},
		},
		"TypeChangeExcludedBeforeRegistration": {
			reason:           "Type change fields should be excluded from identity conversion",
			crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-number.json"),
			setupResource: func() *Resource {
				return newTestResourceWithIntField("TestResource", "count")
			},
			callOrder: "correct",
			want: want{
				conversionCount:      2, // Type change conversions registered
				identityExcludeCount: 1, // count excluded from identity conversion
				err:                  false,
			},
		},
		"ResourceWithBothFunctionsApplied": {
			reason:           "Resource with both functions applied should have conversions and exclude paths",
			crdSchemaChanges: loadTestFixture(t, "valid/multiple-type-changes.json"),
			setupResource: func() *Resource {
				return newTestResource("TestResource")
			},
			callOrder: "correct",
			want: want{
				conversionCount:      4, // string->bool (field2: 2) + number->string (field3: 2), string->number skipped (no schema for field1)
				identityExcludeCount: 3, // All 3 type-changed fields excluded
				err:                  false,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pc := newTestProvider(t)
			testResource := tc.setupResource()
			pc.Resources["TestResource"] = testResource

			var err error
			if tc.callOrder == "correct" {
				// Correct order: ExcludeTypeChanges first, then RegisterAutoConversions
				err = ExcludeTypeChangesFromIdentity(pc, tc.crdSchemaChanges)
				if err == nil {
					err = RegisterAutoConversions(pc, tc.crdSchemaChanges)
				}
			} else {
				// Reversed order: RegisterAutoConversions first, then ExcludeTypeChanges
				err = RegisterAutoConversions(pc, tc.crdSchemaChanges)
				if err == nil {
					err = ExcludeTypeChangesFromIdentity(pc, tc.crdSchemaChanges)
				}
			}

			if tc.want.err {
				if err == nil {
					t.Errorf("\n%s\nExpected error, got nil", tc.reason)
				}
				return
			}

			if err != nil {
				t.Errorf("\n%s\nUnexpected error: %v", tc.reason, err)
				return
			}

			// Verify conversion count
			gotConversionCount := len(testResource.Conversions)
			if diff := cmp.Diff(tc.want.conversionCount, gotConversionCount); diff != "" {
				t.Errorf("\n%s\nconversion count -want, +got:\n%s", tc.reason, diff)
			}

			// Verify identity exclude paths count
			gotIdentityExcludeCount := len(testResource.AutoConversionRegistrationOptions.IdentityConversionExcludePaths)
			if diff := cmp.Diff(tc.want.identityExcludeCount, gotIdentityExcludeCount); diff != "" {
				t.Errorf("\n%s\nidentity exclude count -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestConversionFunctionValidation(t *testing.T) {
	cases := map[string]struct {
		reason           string
		crdSchemaChanges []byte
		setupResource    func() *Resource
		expectedCount    int
	}{
		"MultipleChanges": {
			reason:           "Multiple changes should register correct number of conversions",
			crdSchemaChanges: loadTestFixture(t, "valid/multiple-changes.json"),
			setupResource: func() *Resource {
				return newTestResourceWithIntField("TestResource", "count")
			},
			expectedCount: 6, // 2 (field_added) + 2 (field_deleted) + 2 (type_changed)
		},
		"FieldAdditionOnly": {
			reason:           "Field addition should register 2 bidirectional conversions",
			crdSchemaChanges: loadTestFixture(t, "valid/field-addition.json"),
			setupResource: func() *Resource {
				return newTestResource("TestResource")
			},
			expectedCount: 2,
		},
		"TypeChangeOnly": {
			reason:           "Type change should register 2 bidirectional conversions",
			crdSchemaChanges: loadTestFixture(t, "valid/type-change-string-to-number.json"),
			setupResource: func() *Resource {
				return newTestResourceWithIntField("TestResource", "count")
			},
			expectedCount: 2,
		},
		"ThreeVersionsMultipleChanges": {
			reason:           "Three versions with multiple changes should register all conversions",
			crdSchemaChanges: loadTestFixture(t, "valid/three-versions.json"),
			setupResource: func() *Resource {
				return newTestResourceWithIntField("TestResource", "count")
			},
			expectedCount: 8, // v1beta1: 2+2 + v1beta2: 2+2
		},
		"MultipleTypeChanges": {
			reason:           "Multiple type changes should register conversions for supported types",
			crdSchemaChanges: loadTestFixture(t, "valid/multiple-type-changes.json"),
			setupResource: func() *Resource {
				return newTestResource("TestResource")
			},
			expectedCount: 4, // string->bool (field2: 2) + number->string (field3: 2), string->number skipped (no schema)
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pc := newTestProvider(t)
			testResource := tc.setupResource()
			pc.Resources["TestResource"] = testResource

			// Register conversions
			err := RegisterAutoConversions(pc, tc.crdSchemaChanges)
			if err != nil {
				t.Fatalf("RegisterAutoConversions failed: %v", err)
			}

			// Verify we have the expected number of conversions
			gotCount := len(testResource.Conversions)
			if diff := cmp.Diff(tc.expectedCount, gotCount); diff != "" {
				t.Errorf("\n%s\nconversion count -want, +got:\n%s", tc.reason, diff)
			}

			// Verify all conversions are non-nil
			for i, conv := range testResource.Conversions {
				if conv == nil {
					t.Errorf("Conversion at index %d is nil", i)
				}
			}
		})
	}
}

func TestTrimPathPrefix(t *testing.T) {
	type args struct {
		path string
	}
	type want struct {
		trimmed string
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"ForProviderPrefix": {
			reason: "Should trim spec.forProvider prefix",
			args: args{
				path: "spec.forProvider.fieldName",
			},
			want: want{
				trimmed: "fieldName",
			},
		},
		"InitProviderPrefix": {
			reason: "Should trim spec.initProvider prefix",
			args: args{
				path: "spec.initProvider.fieldName",
			},
			want: want{
				trimmed: "fieldName",
			},
		},
		"AtProviderPrefix": {
			reason: "Should trim status.atProvider prefix",
			args: args{
				path: "status.atProvider.fieldName",
			},
			want: want{
				trimmed: "fieldName",
			},
		},
		"NestedField": {
			reason: "Should trim prefix and preserve nested path",
			args: args{
				path: "spec.forProvider.nested.field.name",
			},
			want: want{
				trimmed: "nested.field.name",
			},
		},
		"NoMatchingPrefix": {
			reason: "Should return empty string for paths without expected prefixes",
			args: args{
				path: "metadata.annotations.key",
			},
			want: want{
				trimmed: "",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := trimPathPrefix(tc.args.path)
			if diff := cmp.Diff(tc.want.trimmed, got); diff != "" {
				t.Errorf("\n%s\ntrimPathPrefix(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
