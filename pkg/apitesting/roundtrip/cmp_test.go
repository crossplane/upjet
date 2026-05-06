// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0
package roundtrip

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestNormalizeMeta(t *testing.T) {
	ts := metav1.NewTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	obj := &metav1.ObjectMeta{
		Name:              "keep-me",
		ResourceVersion:   "12345",
		Generation:        42,
		UID:               types.UID("some-uid"),
		ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
		CreationTimestamp: ts,
		Labels:            map[string]string{"key": "val"},
		Annotations:       map[string]string{"note": "1"},
	}

	normalizeMeta(obj)

	if obj.ResourceVersion != "" {
		t.Errorf("ResourceVersion not cleared: %q", obj.ResourceVersion)
	}
	if obj.Generation != 0 {
		t.Errorf("Generation not cleared: %d", obj.Generation)
	}
	if obj.UID != "" {
		t.Errorf("UID not cleared: %q", obj.UID)
	}
	if obj.ManagedFields != nil {
		t.Errorf("ManagedFields not cleared")
	}
	if !obj.CreationTimestamp.IsZero() {
		t.Errorf("CreationTimestamp not cleared")
	}
	// untouched fields survive
	if obj.Name != "keep-me" {
		t.Errorf("Name modified: %q", obj.Name)
	}
	if obj.Labels["key"] != "val" {
		t.Errorf("Labels modified")
	}
	if obj.Annotations["note"] != "1" {
		t.Errorf("Annotations modified")
	}
}

// withSlice is a helper struct used to test EquateEmptyAndSingleZeroSlice with
// nested slices inside structs.
type withSlice struct {
	Items []string
}

func TestEquateEmptyAndSingleZeroSlice(t *testing.T) {
	opt := EquateEmptyAndSingleZeroSlice()
	// Include EquateEmpty to cover the both-Len==0 case that our option deliberately
	// leaves to the upstream comparer.
	allOpts := cmp.Options{opt, cmpopts.EquateEmpty()}

	zeroStr := ""

	cases := []struct {
		name  string
		equal bool
		diff  func() string
	}{
		{
			name:  "empty int slice equals single-zero int slice",
			equal: true,
			diff:  func() string { return cmp.Diff([]int{}, []int{0}, allOpts) },
		},
		{
			name:  "nil int slice equals single-zero int slice",
			equal: true,
			diff:  func() string { return cmp.Diff([]int(nil), []int{0}, allOpts) },
		},
		{
			name:  "single-zero equals single-zero",
			equal: true,
			diff:  func() string { return cmp.Diff([]int{0}, []int{0}, allOpts) },
		},
		{
			name:  "both empty int slices equal via EquateEmpty",
			equal: true,
			diff:  func() string { return cmp.Diff([]int{}, []int{}, allOpts) },
		},
		{
			name:  "nonzero element not equated with empty",
			equal: false,
			diff:  func() string { return cmp.Diff([]int{1}, []int{}, allOpts) },
		},
		{
			name:  "multi-element slice not equated with empty",
			equal: false,
			diff:  func() string { return cmp.Diff([]int{0, 0}, []int{}, allOpts) },
		},
		{
			name:  "nil pointer slice equals single-nil-pointer slice",
			equal: true,
			diff:  func() string { return cmp.Diff([]*string{}, []*string{nil}, allOpts) },
		},
		{
			name:  "single nil pointer equals pointer to zero string",
			equal: true,
			diff:  func() string { return cmp.Diff([]*string{nil}, []*string{&zeroStr}, allOpts) },
		},
		{
			name:  "nested nil slice equals slice containing nil slice",
			equal: true,
			diff:  func() string { return cmp.Diff([][]int{}, [][]int{nil}, allOpts) },
		},
		{
			name:  "nested empty slice equals slice containing empty slice",
			equal: true,
			diff:  func() string { return cmp.Diff([][]int{}, [][]int{{}}, allOpts) },
		},
		{
			name:  "struct slice: empty equals slice with zero-value struct",
			equal: true,
			diff:  func() string { return cmp.Diff([]withSlice{}, []withSlice{{}}, allOpts) },
		},
		{
			name:  "struct slice: empty equals slice with struct containing nil Items",
			equal: true,
			diff: func() string {
				return cmp.Diff([]withSlice{}, []withSlice{{Items: nil}}, allOpts)
			},
		},
		{
			name:  "struct slice: empty not equal to slice with non-zero struct",
			equal: false,
			diff: func() string {
				return cmp.Diff([]withSlice{}, []withSlice{{Items: []string{"nonempty"}}}, allOpts)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diff := tc.diff()
			if tc.equal && diff != "" {
				t.Errorf("expected equal, got diff:\n%s", diff)
			}
			if !tc.equal && diff == "" {
				t.Errorf("expected diff, but objects compared as equal")
			}
		})
	}
}
