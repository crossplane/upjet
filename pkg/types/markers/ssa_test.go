// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package markers

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/utils/ptr"

	"github.com/crossplane/upjet/v2/pkg/config"
)

func TestServerSideApplyOptions(t *testing.T) {
	cases := map[string]struct {
		o    ServerSideApplyOptions
		want string
	}{
		"MapType": {
			o: ServerSideApplyOptions{
				MapType: ptr.To[config.MapType](config.MapTypeAtomic),
			},
			want: "+mapType=atomic\n",
		},
		"StructType": {
			o: ServerSideApplyOptions{
				StructType: ptr.To[config.StructType](config.StructTypeAtomic),
			},
			want: "+structType=atomic\n",
		},
		"ListType": {
			o: ServerSideApplyOptions{
				ListType:   ptr.To[config.ListType](config.ListTypeMap),
				ListMapKey: []string{"name", "coolness"},
			},
			want: "+listType=map\n+listMapKey=name\n+listMapKey=coolness\n",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.o.String()

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("o.String(): -want, +got: %s", diff)
			}
		})
	}
}
