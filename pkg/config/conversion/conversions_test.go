// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"fmt"
	"slices"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	jsoniter "github.com/json-iterator/go"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

const (
	sourceVersion = "v1beta1"
	sourceField   = "testSourceField"
	targetVersion = "v1beta2"
	targetField   = "testTargetField"
)

func TestConvertPaved(t *testing.T) {
	type args struct {
		sourceVersion string
		sourceField   string
		targetVersion string
		targetField   string
		sourceObj     *fieldpath.Paved
		targetObj     *fieldpath.Paved
	}
	type want struct {
		converted bool
		err       error
		targetObj *fieldpath.Paved
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"SuccessfulConversion": {
			reason: "Source field in source version is successfully converted to the target field in target version.",
			args: args{
				sourceVersion: sourceVersion,
				sourceField:   sourceField,
				targetVersion: targetVersion,
				targetField:   targetField,
				sourceObj:     getPaved(sourceVersion, sourceField, ptr.To("testValue")),
				targetObj:     getPaved(targetVersion, targetField, nil),
			},
			want: want{
				converted: true,
				targetObj: getPaved(targetVersion, targetField, ptr.To("testValue")),
			},
		},
		"SuccessfulConversionAllVersions": {
			reason: "Source field in source version is successfully converted to the target field in target version when the conversion specifies wildcard version for both of the source and the target.",
			args: args{
				sourceVersion: AllVersions,
				sourceField:   sourceField,
				targetVersion: AllVersions,
				targetField:   targetField,
				sourceObj:     getPaved(sourceVersion, sourceField, ptr.To("testValue")),
				targetObj:     getPaved(targetVersion, targetField, nil),
			},
			want: want{
				converted: true,
				targetObj: getPaved(targetVersion, targetField, ptr.To("testValue")),
			},
		},
		"SourceVersionMismatch": {
			reason: "Conversion is not done if the source version of the object does not match the conversion's source version.",
			args: args{
				sourceVersion: "mismatch",
				sourceField:   sourceField,
				targetVersion: AllVersions,
				targetField:   targetField,
				sourceObj:     getPaved(sourceVersion, sourceField, ptr.To("testValue")),
				targetObj:     getPaved(targetVersion, targetField, nil),
			},
			want: want{
				converted: false,
				targetObj: getPaved(targetVersion, targetField, nil),
			},
		},
		"TargetVersionMismatch": {
			reason: "Conversion is not done if the target version of the object does not match the conversion's target version.",
			args: args{
				sourceVersion: AllVersions,
				sourceField:   sourceField,
				targetVersion: "mismatch",
				targetField:   targetField,
				sourceObj:     getPaved(sourceVersion, sourceField, ptr.To("testValue")),
				targetObj:     getPaved(targetVersion, targetField, nil),
			},
			want: want{
				converted: false,
				targetObj: getPaved(targetVersion, targetField, nil),
			},
		},
		"SourceFieldNotFound": {
			reason: "Conversion is not done if the source field is not found in the source object.",
			args: args{
				sourceVersion: sourceVersion,
				sourceField:   sourceField,
				targetVersion: targetVersion,
				targetField:   targetField,
				sourceObj:     getPaved(sourceVersion, sourceField, nil),
				targetObj:     getPaved(targetVersion, targetField, ptr.To("test")),
			},
			want: want{
				converted: false,
				targetObj: getPaved(targetVersion, targetField, ptr.To("test")),
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := NewFieldRenameConversion(tc.args.sourceVersion, tc.args.sourceField, tc.args.targetVersion, tc.args.targetField)
			converted, err := c.(*fieldCopy).ConvertPaved(tc.args.sourceObj, tc.args.targetObj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConvertPaved(sourceObj, targetObj): -wantErr, +gotErr:\n%s", tc.reason, diff)
			}
			if tc.want.err != nil {
				return
			}
			if diff := cmp.Diff(tc.want.converted, converted); diff != "" {
				t.Errorf("\n%s\nConvertPaved(sourceObj, targetObj): -wantConverted, +gotConverted:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.targetObj.UnstructuredContent(), tc.args.targetObj.UnstructuredContent()); diff != "" {
				t.Errorf("\n%s\nConvertPaved(sourceObj, targetObj): -wantTargetObj, +gotTargetObj:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestIdentityConversion(t *testing.T) {
	type args struct {
		sourceVersion string
		source        resource.Managed
		targetVersion string
		target        *mockManaged
		pathPrefixes  []string
		excludePaths  []string
	}
	type want struct {
		converted bool
		err       error
		target    *mockManaged
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"SuccessfulConversionNoExclusions": {
			reason: "Successfully copy identical fields from the source to the target with no exclusions.",
			args: args{
				sourceVersion: AllVersions,
				source: newMockManaged(map[string]any{
					"k1": "v1",
					"k2": "v2",
					"k3": map[string]any{
						"nk1": "nv1",
					},
				}),
				targetVersion: AllVersions,
				target:        newMockManaged(nil),
			},
			want: want{
				converted: true,
				target: newMockManaged(map[string]any{
					"k1": "v1",
					"k2": "v2",
					"k3": map[string]any{
						"nk1": "nv1",
					},
				}),
			},
		},
		"SuccessfulConversionExclusionsWithNoPrefixes": {
			reason: "Successfully copy identical fields from the source to the target with exclusions without prefixes.",
			args: args{
				sourceVersion: AllVersions,
				source: newMockManaged(map[string]any{
					"k1": "v1",
					"k2": "v2",
					"k3": map[string]any{
						"nk1": "nv1",
					},
				}),
				targetVersion: AllVersions,
				target:        newMockManaged(nil),
				excludePaths:  []string{"k2", "k3"},
			},
			want: want{
				converted: true,
				target: newMockManaged(map[string]any{
					"k1": "v1",
				}),
			},
		},
		"SuccessfulConversionNestedExclusionsWithNoPrefixes": {
			reason: "Successfully copy identical fields from the source to the target with nested exclusions without prefixes.",
			args: args{
				sourceVersion: AllVersions,
				source: newMockManaged(map[string]any{
					"k1": "v1",
					"k2": "v2",
					"k3": map[string]any{
						"nk1": "nv1",
					},
				}),
				targetVersion: AllVersions,
				target:        newMockManaged(nil),
				excludePaths:  []string{"k2", "k3.nk1"},
			},
			want: want{
				converted: true,
				target: newMockManaged(map[string]any{
					"k1": "v1",
					// key key3 is copied without its nested element (as an empty map)
					"k3": map[string]any{},
				}),
			},
		},
		"SuccessfulConversionWithListExclusion": {
			reason: "Successfully copy identical fields from the source to the target with an exclusion for a root-level list.",
			args: args{
				sourceVersion: AllVersions,
				source: newMockManaged(map[string]any{
					"k1": "v1",
					"k2": []map[string]any{
						{
							"nk3": "nv3",
						},
					},
				}),
				targetVersion: AllVersions,
				target:        newMockManaged(nil),
				excludePaths:  []string{"k2"},
			},
			want: want{
				converted: true,
				target: newMockManaged(map[string]any{
					"k1": "v1",
				}),
			},
		},
		"SuccessfulConversionWithNestedListExclusion": {
			reason: "Successfully copy identical fields from the source to the target with an exclusion for a nested list.",
			args: args{
				sourceVersion: AllVersions,
				source: newMockManaged(map[string]any{
					"k1": "v1",
					"k2": []map[string]any{
						{
							"nk3": []map[string]any{
								{
									"nk4": "nv4",
								},
							},
						},
					},
				}),
				targetVersion: AllVersions,
				target:        newMockManaged(nil),
				excludePaths:  []string{"k2[*].nk3"},
			},
			want: want{
				converted: true,
				target: newMockManaged(map[string]any{
					"k1": "v1",
					"k2": []any{map[string]any{}},
				}),
			},
		},
		"SuccessfulConversionWithDefaultExclusionPrefixes": {
			reason: "Successfully copy identical fields from the source to the target with an exclusion for a nested list.",
			args: args{
				sourceVersion: AllVersions,
				source: newMockManaged(map[string]any{
					"spec": map[string]any{
						"initProvider": map[string]any{
							"k1": "v1",
							"k2": "v2",
						},
						"forProvider": map[string]any{
							"k1": "v1",
							"k2": "v2",
						},
					},
					"status": map[string]any{
						"atProvider": map[string]any{
							"k1": "v1",
							"k2": "v2",
						},
					},
				}),
				targetVersion: AllVersions,
				target:        newMockManaged(nil),
				excludePaths:  []string{"k2"},
				pathPrefixes:  DefaultPathPrefixes(),
			},
			want: want{
				converted: true,
				target: newMockManaged(map[string]any{
					"spec": map[string]any{
						"initProvider": map[string]any{
							"k1": "v1",
						},
						"forProvider": map[string]any{
							"k1": "v1",
						},
					},
					"status": map[string]any{
						"atProvider": map[string]any{
							"k1": "v1",
						},
					},
				}),
			},
		},
	}
	for n, tc := range tests {
		t.Run(n, func(t *testing.T) {
			c := NewIdentityConversionExpandPaths(tc.args.sourceVersion, tc.args.targetVersion, tc.args.pathPrefixes, tc.args.excludePaths...)
			converted, err := c.(*identityConversion).ConvertManaged(tc.args.source, tc.args.target)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Fatalf("\n%s\nConvertManaged(source, target): -wantErr, +gotErr:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.converted, converted); diff != "" {
				t.Errorf("\n%s\nConvertManaged(source, target): -wantConverted, +gotConverted:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.target.UnstructuredContent(), tc.args.target.UnstructuredContent()); diff != "" {
				t.Errorf("\n%s\nConvertManaged(source, target): -wantTarget, +gotTarget:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestDefaultPathPrefixes(t *testing.T) {
	// no need for a table-driven test here as we assert all the parameter roots
	// in the MR schema are asserted.
	want := []string{"spec.forProvider", "spec.initProvider", "status.atProvider"}
	slices.Sort(want)
	got := DefaultPathPrefixes()
	slices.Sort(got)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("DefaultPathPrefixes(): -want, +got:\n%s", diff)
	}
}

func TestSingletonListConversion(t *testing.T) {
	type args struct {
		sourceVersion string
		sourceMap     map[string]any
		targetVersion string
		targetMap     map[string]any
		crdPaths      []string
		mode          ListConversionMode
		opts          []SingletonListConversionOption
	}
	type want struct {
		converted bool
		err       error
		targetMap map[string]any
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"SuccessfulToEmbeddedObjectConversion": {
			reason: "Successful conversion from a singleton list to an embedded object.",
			args: args{
				sourceVersion: AllVersions,
				sourceMap: map[string]any{
					"spec": map[string]any{
						"initProvider": map[string]any{
							"l": []map[string]any{
								{
									"k": "v",
								},
							},
						},
					},
				},
				targetVersion: AllVersions,
				targetMap:     map[string]any{},
				crdPaths:      []string{"l"},
				mode:          ToEmbeddedObject,
			},
			want: want{
				converted: true,
				targetMap: map[string]any{
					"spec": map[string]any{
						"initProvider": map[string]any{
							"l": map[string]any{
								"k": "v",
							},
						},
					},
				},
			},
		},
		"SuccessfulToSingletonListConversion": {
			reason: "Successful conversion from an embedded object to a singleton list.",
			args: args{
				sourceVersion: AllVersions,
				sourceMap: map[string]any{
					"spec": map[string]any{
						"initProvider": map[string]any{
							"o": map[string]any{
								"k": "v",
							},
						},
					},
				},
				targetVersion: AllVersions,
				targetMap:     map[string]any{},
				crdPaths:      []string{"o"},
				mode:          ToSingletonList,
			},
			want: want{
				converted: true,
				targetMap: map[string]any{
					"spec": map[string]any{
						"initProvider": map[string]any{
							"o": []map[string]any{
								{
									"k": "v",
								},
							},
						},
					},
				},
			},
		},
		"NoCRDPath": {
			reason: "No conversion when the specified CRD paths is empty.",
			args: args{
				sourceVersion: AllVersions,
				sourceMap: map[string]any{
					"spec": map[string]any{
						"initProvider": map[string]any{
							"o": map[string]any{
								"k": "v",
							},
						},
					},
				},
				targetVersion: AllVersions,
				targetMap:     map[string]any{},
				mode:          ToSingletonList,
			},
			want: want{
				converted: false,
				targetMap: map[string]any{},
			},
		},
		"SuccessfulToSingletonListConversionWithInjectedKey": {
			reason: "Successful conversion from an embedded object to a singleton list.",
			args: args{
				sourceVersion: AllVersions,
				sourceMap: map[string]any{
					"spec": map[string]any{
						"initProvider": map[string]any{
							"o": map[string]any{
								"k": "v",
							},
						},
					},
				},
				targetVersion: AllVersions,
				targetMap:     map[string]any{},
				crdPaths:      []string{"o"},
				mode:          ToSingletonList,
				opts: []SingletonListConversionOption{
					WithConvertOptions(&ConvertOptions{
						ListInjectKeys: map[string]SingletonListInjectKey{
							"o": {
								Key:   "index",
								Value: "0",
							},
						},
					}),
				},
			},
			want: want{
				converted: true,
				targetMap: map[string]any{
					"spec": map[string]any{
						"initProvider": map[string]any{
							"o": []map[string]any{
								{
									"k":     "v",
									"index": "0",
								},
							},
						},
					},
				},
			},
		},
	}
	for n, tc := range tests {
		t.Run(n, func(t *testing.T) {

			c := NewSingletonListConversion(tc.args.sourceVersion, tc.args.targetVersion, []string{pathInitProvider}, tc.args.crdPaths, tc.args.mode, tc.args.opts...)
			sourceMap, err := roundTrip(tc.args.sourceMap)
			if err != nil {
				t.Fatalf("Failed to preprocess tc.args.sourceMap: %v", err)
			}
			targetMap, err := roundTrip(tc.args.targetMap)
			if err != nil {
				t.Fatalf("Failed to preprocess tc.args.targetMap: %v", err)
			}
			converted, err := c.(*singletonListConverter).ConvertPaved(fieldpath.Pave(sourceMap), fieldpath.Pave(targetMap))
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Fatalf("\n%s\nConvertPaved(source, target): -wantErr, +gotErr:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.converted, converted); diff != "" {
				t.Errorf("\n%s\nConvertPaved(source, target): -wantConverted, +gotConverted:\n%s", tc.reason, diff)
			}
			m, err := roundTrip(tc.want.targetMap)
			if err != nil {
				t.Fatalf("Failed to preprocess tc.want.targetMap: %v", err)
			}
			if diff := cmp.Diff(m, targetMap); diff != "" {
				t.Errorf("\n%s\nConvertPaved(source, target): -wantTarget, +gotTarget:\n%s", tc.reason, diff)
			}
		})
	}
}

func getPaved(version, field string, value *string) *fieldpath.Paved {
	m := map[string]any{
		"apiVersion": fmt.Sprintf("mockgroup/%s", version),
		"kind":       "mockkind",
	}
	if value != nil {
		m[field] = *value
	}
	return fieldpath.Pave(m)
}

type mockManaged struct {
	*fake.Managed
	*fieldpath.Paved
}

func (m *mockManaged) DeepCopyObject() runtime.Object {
	buff, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(m.Paved.UnstructuredContent())
	if err != nil {
		panic(err)
	}
	var u map[string]any
	if err := jsoniter.Unmarshal(buff, &u); err != nil {
		panic(err)
	}
	return &mockManaged{
		Managed: m.Managed.DeepCopyObject().(*fake.Managed),
		Paved:   fieldpath.Pave(u),
	}
}

func newMockManaged(m map[string]any) *mockManaged {
	return &mockManaged{
		Managed: &fake.Managed{},
		Paved:   fieldpath.Pave(m),
	}
}

func TestOptionalFieldConversion(t *testing.T) {
	type args struct {
		sourceVersion string
		targetVersion string
		fieldPath     string
		mode          OptionalFieldConversionMode
		sourceObj     *fieldpath.Paved
		targetObj     *fieldpath.Paved
	}
	type want struct {
		converted bool
		err       error
		targetObj *fieldpath.Paved
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"SuccessfulToAnnotationStringField": {
			reason: "Successfully convert a string field to annotation when field exists in source but not in target schema.",
			args: args{
				sourceVersion: "v1beta2",
				targetVersion: "v1beta1",
				fieldPath:     "spec.forProvider.newField",
				mode:          ToAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"newField": "test-value",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"metadata": map[string]any{
						"annotations": map[string]any{
							"internal-upjet/spec.forProvider.newField": `"test-value"`,
						},
					},
				}),
			},
		},
		"SuccessfulToAnnotationComplexField": {
			reason: "Successfully convert a complex field (map) to annotation.",
			args: args{
				sourceVersion: "v1beta2",
				targetVersion: "v1beta1",
				fieldPath:     "spec.forProvider.complexField",
				mode:          ToAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"complexField": map[string]any{
								"nested":  "value",
								"another": 42,
							},
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"metadata": map[string]any{
						"annotations": map[string]any{
							"internal-upjet/spec.forProvider.complexField": `{"another":42,"nested":"value"}`,
						},
					},
				}),
			},
		},
		"SuccessfulFromAnnotationStringField": {
			reason: "Successfully convert annotation back to string field.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.newField",
				mode:          FromAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"metadata": map[string]any{
						"annotations": map[string]any{
							"internal-upjet/spec.forProvider.newField": `"restored-value"`,
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"newField": "restored-value",
						},
					},
				}),
			},
		},
		"SuccessfulFromAnnotationComplexField": {
			reason: "Successfully convert annotation back to complex field.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.complexField",
				mode:          FromAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"metadata": map[string]any{
						"annotations": map[string]any{
							"internal-upjet/spec.forProvider.complexField": `{"nested":"value","another":42}`,
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"complexField": map[string]any{
								"nested":  "value",
								"another": int64(42), // JSON unmarshal converts numbers to int64
							},
						},
					},
				}),
			},
		},
		"ToAnnotationFieldNotFound": {
			reason: "No conversion when source field is not found in source object.",
			args: args{
				sourceVersion: "v1beta2",
				targetVersion: "v1beta1",
				fieldPath:     "spec.forProvider.missingField",
				mode:          ToAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"existingField": "value",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
				}),
			},
			want: want{
				converted: false,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
				}),
			},
		},
		"FromAnnotationAnnotationNotFound": {
			reason: "No conversion when annotation is not found in source object.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.newField",
				mode:          FromAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"existingField": "value",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
				}),
			},
			want: want{
				converted: false,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
				}),
			},
		},
		"VersionMismatch": {
			reason: "No conversion when API versions don't match the conversion configuration.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.newField",
				mode:          ToAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1alpha1", // Different version
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"newField": "value",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
				}),
			},
			want: want{
				converted: false,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
				}),
			},
		},
		"ToAnnotationNilField": {
			reason: "Successfully handle nil field value by storing empty annotation.",
			args: args{
				sourceVersion: "v1beta2",
				targetVersion: "v1beta1",
				fieldPath:     "spec.forProvider.nullField",
				mode:          ToAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"nullField": nil,
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"metadata": map[string]any{
						"annotations": map[string]any{
							"internal-upjet/spec.forProvider.nullField": "",
						},
					},
				}),
			},
		},
		"SuccessfulToAnnotationNestedField": {
			reason: "Successfully convert a nested field to annotation.",
			args: args{
				sourceVersion: "v1beta2",
				targetVersion: "v1beta1",
				fieldPath:     "spec.forProvider.a.b",
				mode:          ToAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"a": map[string]any{
								"b": "nested-value",
							},
							"existingField": "existing",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"metadata": map[string]any{
						"annotations": map[string]any{
							"internal-upjet/spec.forProvider.a.b": `"nested-value"`,
						},
					},
				}),
			},
		},
		"SuccessfulFromAnnotationNestedField": {
			reason: "Successfully convert annotation back to nested field.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.a.b",
				mode:          FromAnnotation,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"metadata": map[string]any{
						"annotations": map[string]any{
							"internal-upjet/spec.forProvider.a.b": `"nested-restored-value"`,
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"existingField": "existing",
						},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"existingField": "existing",
							"a": map[string]any{
								"b": "nested-restored-value",
							},
						},
					},
				}),
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := NewOptionalFieldConversion(tc.args.sourceVersion, tc.args.targetVersion, tc.args.fieldPath, tc.args.mode)
			converted, err := c.(*optionalFieldConverter).ConvertPaved(tc.args.sourceObj, tc.args.targetObj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConvertPaved(sourceObj, targetObj): -wantErr, +gotErr:\n%s", tc.reason, diff)
			}
			if tc.want.err != nil {
				return
			}
			if diff := cmp.Diff(tc.want.converted, converted); diff != "" {
				t.Errorf("\n%s\nConvertPaved(sourceObj, targetObj): -wantConverted, +gotConverted:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.targetObj.UnstructuredContent(), tc.args.targetObj.UnstructuredContent()); diff != "" {
				t.Errorf("\n%s\nConvertPaved(sourceObj, targetObj): -wantTargetObj, +gotTargetObj:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestGenerateAnnotationKey(t *testing.T) {
	tests := map[string]struct {
		reason    string
		fieldPath string
		want      string
	}{
		"SimpleFieldPath": {
			reason:    "Extract field name from simple path.",
			fieldPath: "newField",
			want:      "internal-upjet/newField",
		},
		"NestedFieldPath": {
			reason:    "Extract field name from nested path.",
			fieldPath: "spec.forProvider.newField",
			want:      "internal-upjet/spec.forProvider.newField",
		},
		"DeeplyNestedFieldPath": {
			reason:    "Extract field name from deeply nested path.",
			fieldPath: "spec.forProvider.configuration.advanced.newField",
			want:      "internal-upjet/spec.forProvider.configuration.advanced.newField",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := generateAnnotationKey(tc.fieldPath)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\ngenerateAnnotationKey(%s): -want, +got:\n%s", tc.reason, tc.fieldPath, diff)
			}
		})
	}
}

func TestOptionalFieldConversionModeString(t *testing.T) {
	tests := map[string]struct {
		mode OptionalFieldConversionMode
		want string
	}{
		"ToAnnotation": {
			mode: ToAnnotation,
			want: "ToAnnotation",
		},
		"FromAnnotation": {
			mode: FromAnnotation,
			want: "FromAnnotation",
		},
		"UnknownMode": {
			mode: OptionalFieldConversionMode(999),
			want: "Unknown",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.mode.String()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("OptionalFieldConversionMode.String(): -want, +got:\n%s", diff)
			}
		})
	}
}

func TestFieldTypeConversion(t *testing.T) {
	type args struct {
		sourceVersion string
		targetVersion string
		fieldPath     string
		mode          TypeConversionMode
		sourceObj     *fieldpath.Paved
		targetObj     *fieldpath.Paved
	}
	type want struct {
		converted bool
		err       error
		targetObj *fieldpath.Paved
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"SuccessfulIntToStringConversion": {
			reason: "Successfully convert int64 field to string.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.port",
				mode:          IntToString,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"port": int64(8080),
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"port": "8080",
						},
					},
				}),
			},
		},
		"SuccessfulIntToStringFromFloat64": {
			reason: "Successfully convert float64 (representing integer) field to string.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.port",
				mode:          IntToString,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"port": float64(8080), // JSON unmarshaling produces float64
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"port": "8080",
						},
					},
				}),
			},
		},
		"SuccessfulStringToIntConversion": {
			reason: "Successfully convert string field to int64.",
			args: args{
				sourceVersion: "v1beta2",
				targetVersion: "v1beta1",
				fieldPath:     "spec.forProvider.port",
				mode:          StringToInt,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"port": "8080",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"port": int64(8080),
						},
					},
				}),
			},
		},
		"SuccessfulBoolToStringConversion": {
			reason: "Successfully convert bool field to string.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.enabled",
				mode:          BoolToString,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"enabled": true,
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"enabled": "true",
						},
					},
				}),
			},
		},
		"SuccessfulStringToBoolConversion": {
			reason: "Successfully convert string field to bool.",
			args: args{
				sourceVersion: "v1beta2",
				targetVersion: "v1beta1",
				fieldPath:     "spec.forProvider.enabled",
				mode:          StringToBool,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"enabled": "true",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"enabled": true,
						},
					},
				}),
			},
		},
		"SuccessfulStringToBoolVariations": {
			reason: "Successfully convert various string representations to bool.",
			args: args{
				sourceVersion: "v1beta2",
				targetVersion: "v1beta1",
				fieldPath:     "spec.forProvider.enabled",
				mode:          StringToBool,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"enabled": "1",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"enabled": true,
						},
					},
				}),
			},
		},
		"SuccessfulFloatToStringConversion": {
			reason: "Successfully convert float64 field to string.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.ratio",
				mode:          FloatToString,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"ratio": float64(3.14),
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"ratio": "3.14",
						},
					},
				}),
			},
		},
		"SuccessfulStringToFloatConversion": {
			reason: "Successfully convert string field to float64.",
			args: args{
				sourceVersion: "v1beta2",
				targetVersion: "v1beta1",
				fieldPath:     "spec.forProvider.ratio",
				mode:          StringToFloat,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"ratio": "3.14",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"ratio": float64(3.14),
						},
					},
				}),
			},
		},
		"FieldNotFound": {
			reason: "No conversion when source field is not found.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.missingField",
				mode:          IntToString,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"existingField": "value",
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: false,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
		},
		"VersionMismatch": {
			reason: "No conversion when API versions don't match the conversion configuration.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.port",
				mode:          IntToString,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1alpha1", // Different version
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"port": int64(8080),
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: false,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
		},
		"NilValueHandling": {
			reason: "Successfully handle nil field value.",
			args: args{
				sourceVersion: "v1beta1",
				targetVersion: "v1beta2",
				fieldPath:     "spec.forProvider.port",
				mode:          IntToString,
				sourceObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta1",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"port": nil,
						},
					},
				}),
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{},
					},
				}),
			},
			want: want{
				converted: true,
				targetObj: fieldpath.Pave(map[string]any{
					"apiVersion": "test.crossplane.io/v1beta2",
					"kind":       "TestResource",
					"spec": map[string]any{
						"forProvider": map[string]any{
							"port": nil,
						},
					},
				}),
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := NewFieldTypeConversion(tc.args.sourceVersion, tc.args.targetVersion, tc.args.fieldPath, tc.args.mode)
			converted, err := c.(*fieldTypeConverter).ConvertPaved(tc.args.sourceObj, tc.args.targetObj)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nConvertPaved(sourceObj, targetObj): -wantErr, +gotErr:\n%s", tc.reason, diff)
			}
			if tc.want.err != nil {
				return
			}
			if diff := cmp.Diff(tc.want.converted, converted); diff != "" {
				t.Errorf("\n%s\nConvertPaved(sourceObj, targetObj): -wantConverted, +gotConverted:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.targetObj.UnstructuredContent(), tc.args.targetObj.UnstructuredContent()); diff != "" {
				t.Errorf("\n%s\nConvertPaved(sourceObj, targetObj): -wantTargetObj, +gotTargetObj:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestTypeConversionModeString(t *testing.T) {
	tests := map[string]struct {
		mode TypeConversionMode
		want string
	}{
		"IntToString": {
			mode: IntToString,
			want: "IntToString",
		},
		"StringToInt": {
			mode: StringToInt,
			want: "StringToInt",
		},
		"BoolToString": {
			mode: BoolToString,
			want: "BoolToString",
		},
		"StringToBool": {
			mode: StringToBool,
			want: "StringToBool",
		},
		"FloatToString": {
			mode: FloatToString,
			want: "FloatToString",
		},
		"StringToFloat": {
			mode: StringToFloat,
			want: "StringToFloat",
		},
		"UnknownMode": {
			mode: TypeConversionMode(999),
			want: "Unknown",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.mode.String()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("TypeConversionMode.String(): -want, +got:\n%s", diff)
			}
		})
	}
}
