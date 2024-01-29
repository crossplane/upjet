// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
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
