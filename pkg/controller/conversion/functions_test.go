// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/config/conversion"
	"github.com/crossplane/upjet/pkg/resource"
	"github.com/crossplane/upjet/pkg/resource/fake"
)

const (
	key1      = "key1"
	val1      = "val1"
	key2      = "key2"
	val2      = "val2"
	commonKey = "commonKey"
	commonVal = "commonVal"
	errTest   = "test error"
)

func TestRoundTrip(t *testing.T) {
	type args struct {
		dst         resource.Terraformed
		src         resource.Terraformed
		conversions []conversion.Conversion
	}
	type want struct {
		err error
		dst resource.Terraformed
	}
	tests := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"SuccessfulRoundTrip": {
			reason: "Source object is successfully copied into the target object.",
			args: args{
				dst:         fake.NewTerraformed(),
				src:         fake.NewTerraformed(fake.WithParameters(fake.NewMap(key1, val1))),
				conversions: []conversion.Conversion{conversion.NewIdentityConversionExpandPaths(conversion.AllVersions, conversion.AllVersions, nil)},
			},
			want: want{
				dst: fake.NewTerraformed(fake.WithParameters(fake.NewMap(key1, val1))),
			},
		},
		"SuccessfulRoundTripWithConversions": {
			reason: "Source object is successfully converted into the target object with a set of conversions.",
			args: args{
				dst: fake.NewTerraformed(),
				src: fake.NewTerraformed(fake.WithParameters(fake.NewMap(commonKey, commonVal, key1, val1))),
				conversions: []conversion.Conversion{
					conversion.NewIdentityConversionExpandPaths(conversion.AllVersions, conversion.AllVersions, nil),
					// Because the parameters of the fake.Terraformed is an unstructured
					// map, all the fields of source (including key1) are successfully
					// copied into dst by registry.RoundTrip.
					// This conversion deletes the copied key "key1".
					conversion.NewCustomConverter(conversion.AllVersions, conversion.AllVersions, func(_, target xpresource.Managed) error {
						tr := target.(*fake.Terraformed)
						delete(tr.Parameters, key1)
						return nil
					}),
					conversion.NewFieldRenameConversion(conversion.AllVersions, fmt.Sprintf("parameterizable.parameters.%s", key1), conversion.AllVersions, fmt.Sprintf("parameterizable.parameters.%s", key2)),
				},
			},
			want: want{
				dst: fake.NewTerraformed(fake.WithParameters(fake.NewMap(commonKey, commonVal, key2, val1))),
			},
		},
		"SuccessfulRoundTripWithNonWildcardConversions": {
			reason: "Source object is successfully converted into the target object with a set of non-wildcard conversions.",
			args: args{
				dst: fake.NewTerraformed(fake.WithTypeMeta(metav1.TypeMeta{})),
				src: fake.NewTerraformed(fake.WithParameters(fake.NewMap(commonKey, commonVal, key1, val1)), fake.WithTypeMeta(metav1.TypeMeta{})),
				conversions: []conversion.Conversion{
					conversion.NewIdentityConversionExpandPaths(fake.Version, fake.Version, nil),
					// Because the parameters of the fake.Terraformed is an unstructured
					// map, all the fields of source (including key1) are successfully
					// copied into dst by registry.RoundTrip.
					// This conversion deletes the copied key "key1".
					conversion.NewCustomConverter(fake.Version, fake.Version, func(_, target xpresource.Managed) error {
						tr := target.(*fake.Terraformed)
						delete(tr.Parameters, key1)
						return nil
					}),
					conversion.NewFieldRenameConversion(fake.Version, fmt.Sprintf("parameterizable.parameters.%s", key1), fake.Version, fmt.Sprintf("parameterizable.parameters.%s", key2)),
				},
			},
			want: want{
				dst: fake.NewTerraformed(fake.WithParameters(fake.NewMap(commonKey, commonVal, key2, val1)), fake.WithTypeMeta(metav1.TypeMeta{
					Kind:       fake.Kind,
					APIVersion: fake.GroupVersion.String(),
				})),
			},
		},
		"RoundTripFailedPrioritizedConversion": {
			reason: "Should return an error if a PrioritizedConversion fails.",
			args: args{
				dst:         fake.NewTerraformed(),
				src:         fake.NewTerraformed(),
				conversions: []conversion.Conversion{failedPrioritizedConversion{}},
			},
			want: want{
				err: errors.Wrapf(errors.New(errTest), errFmtPrioritizedManagedConversion, ""),
			},
		},
		"RoundTripFailedPavedConversion": {
			reason: "Should return an error if a PavedConversion fails.",
			args: args{
				dst:         fake.NewTerraformed(),
				src:         fake.NewTerraformed(),
				conversions: []conversion.Conversion{failedPavedConversion{}},
			},
			want: want{
				err: errors.Wrapf(errors.New(errTest), errFmtPavedConversion, ""),
			},
		},
		"RoundTripFailedManagedConversion": {
			reason: "Should return an error if a ManagedConversion fails.",
			args: args{
				dst:         fake.NewTerraformed(),
				src:         fake.NewTerraformed(),
				conversions: []conversion.Conversion{failedManagedConversion{}},
			},
			want: want{
				err: errors.Wrapf(errors.New(errTest), errFmtManagedConversion, ""),
			},
		},
		"RoundTripWithExcludedFields": {
			reason: "Source object is successfully copied into the target object with certain fields excluded.",
			args: args{
				dst:         fake.NewTerraformed(),
				src:         fake.NewTerraformed(fake.WithParameters(fake.NewMap(key1, val1, key2, val2))),
				conversions: []conversion.Conversion{conversion.NewIdentityConversionExpandPaths(conversion.AllVersions, conversion.AllVersions, []string{"parameterizable.parameters"}, key2)},
			},
			want: want{
				dst: fake.NewTerraformed(fake.WithParameters(fake.NewMap(key1, val1))),
			},
		},
	}

	s := runtime.NewScheme()
	if err := fake.AddToScheme(s); err != nil {
		t.Fatalf("Failed to register the fake.Terraformed object with the runtime scheme")
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			p := &config.Provider{
				Resources: map[string]*config.Resource{
					tc.args.dst.GetTerraformResourceType(): {
						Conversions: tc.args.conversions,
					},
				},
			}
			r := &registry{
				scheme: s,
			}
			if err := r.RegisterConversions(p, nil); err != nil {
				t.Fatalf("\n%s\nRegisterConversions(p): Failed to register the conversions with the registry.\n", tc.reason)
			}
			err := r.RoundTrip(tc.args.dst, tc.args.src)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nRoundTrip(dst, src): -wantErr, +gotErr:\n%s", tc.reason, diff)
			}
			if tc.want.err != nil {
				return
			}
			if diff := cmp.Diff(tc.want.dst, tc.args.dst); diff != "" {
				t.Errorf("\n%s\nRoundTrip(dst, src): -wantDst, +gotDst:\n%s", tc.reason, diff)
			}
		})
	}
}

type failedPrioritizedConversion struct{}

func (failedPrioritizedConversion) Applicable(_, _ runtime.Object) bool {
	return true
}

func (failedPrioritizedConversion) ConvertManaged(_, _ xpresource.Managed) (bool, error) {
	return false, errors.New(errTest)
}

func (failedPrioritizedConversion) Prioritized() {}

type failedPavedConversion struct{}

func (failedPavedConversion) Applicable(_, _ runtime.Object) bool {
	return true
}

func (failedPavedConversion) ConvertPaved(_, _ *fieldpath.Paved) (bool, error) {
	return false, errors.New(errTest)
}

type failedManagedConversion struct{}

func (failedManagedConversion) Applicable(_, _ runtime.Object) bool {
	return true
}

func (failedManagedConversion) ConvertManaged(_, _ xpresource.Managed) (bool, error) {
	return false, errors.New(errTest)
}
