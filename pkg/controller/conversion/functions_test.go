// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"fmt"
	"testing"

	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"

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
				dst: fake.NewTerraformed(),
				src: fake.NewTerraformed(fake.WithParameters(fake.NewMap(key1, val1))),
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
			r := &registry{}
			if err := r.RegisterConversions(p); err != nil {
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
