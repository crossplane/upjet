// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"fmt"
	"slices"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"github.com/crossplane/crossplane-runtime/pkg/test"
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
	}
	for n, tc := range tests {
		t.Run(n, func(t *testing.T) {
			c := NewSingletonListConversion(tc.args.sourceVersion, tc.args.targetVersion, []string{pathInitProvider}, tc.args.crdPaths, tc.args.mode)
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
