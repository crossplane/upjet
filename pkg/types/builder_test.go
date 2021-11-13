/*
 Copyright 2021 The Crossplane Authors.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package types

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

func TestBuilder_generateTypeName(t *testing.T) {
	type args struct {
		existing []string
		suffix   string
		names    []string
	}
	type want struct {
		out string
		err error
	}
	cases := map[string]struct {
		args
		want
	}{
		"NoExisting": {
			args: args{
				existing: []string{
					"SomeOtherType",
				},
				suffix: "Parameters",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkParameters",
				err: nil,
			},
		},
		"NoExistingMultipleIndexes": {
			args: args{
				existing: []string{
					"SomeOtherType",
				},
				suffix: "Parameters",
				names: []string{
					"RouterNat",
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkParameters",
				err: nil,
			},
		},
		"NoIndexExists": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
				},
				suffix: "Parameters",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkParameters_2",
				err: nil,
			},
		},
		"MultipleIndexesExist": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
					"SubnetworkParameters_2",
					"SubnetworkParameters_3",
					"SubnetworkParameters_4",
				},
				suffix: "Parameters",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkParameters_5",
				err: nil,
			},
		},
		"ErrIfAllIndexesExist": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
					"SubnetworkParameters_2",
					"SubnetworkParameters_3",
					"SubnetworkParameters_4",
					"SubnetworkParameters_5",
					"SubnetworkParameters_6",
					"SubnetworkParameters_7",
					"SubnetworkParameters_8",
					"SubnetworkParameters_9",
				},
				suffix: "Parameters",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				err: errors.Errorf("could not generate a unique name for %s", "SubnetworkParameters"),
			},
		},
		"MultipleNamesPrependsBeforeIndexing": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
				},
				suffix: "Parameters",
				names: []string{
					"RouterNat",
					"Subnetwork",
				},
			},
			want: want{
				out: "RouterNatSubnetworkParameters",
				err: nil,
			},
		},
		"MultipleNamesUsesIndexingIfNeeded": {
			args: args{
				existing: []string{
					"SubnetworkParameters",
					"RouterNatSubnetworkParameters",
				},
				suffix: "Parameters",
				names: []string{
					"RouterNat",
					"Subnetwork",
				},
			},
			want: want{
				out: "RouterNatSubnetworkParameters_2",
				err: nil,
			},
		},
		"AnySuffixWouldWorkSame": {
			args: args{
				existing: []string{
					"SubnetworkObservation",
					"SubnetworkObservation_2",
					"SubnetworkObservation_3",
					"SubnetworkObservation_4",
				},
				suffix: "Observation",
				names: []string{
					"Subnetwork",
				},
			},
			want: want{
				out: "SubnetworkObservation_5",
				err: nil,
			},
		},
	}
	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {
			p := types.NewPackage("path/to/test", "test")
			for _, s := range tc.existing {
				p.Scope().Insert(types.NewTypeName(token.NoPos, p, s, &types.Struct{}))
			}

			g := &Builder{
				Package: p,
			}
			got, gotErr := g.generateTypeName(tc.args.suffix, tc.args.names...)
			if diff := cmp.Diff(tc.want.err, gotErr, test.EquateErrors()); diff != "" {
				t.Fatalf("generateTypeName(...): -want error, +got error: %s", diff)
			}
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("generateTypeName(...) out = %v, want %v", got, tc.want.out)
			}
		})
	}
}
