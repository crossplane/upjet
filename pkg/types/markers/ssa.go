// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package markers

import "fmt"

// A ListType is a type of list.
type ListType string

// Types of lists.
const (
	// ListTypeAtomic means the entire list is replaced during merge. At any
	// point in time, a single manager owns the list.
	ListTypeAtomic ListType = "atomic"

	// ListTypeSet can be granularly merged, and different managers can own
	// different elements in the list. The list can include only scalar
	// elements.
	ListTypeSet ListType = "set"

	// ListTypeSet can be granularly merged, and different managers can own
	// different elements in the list. The list can include only nested types
	// (i.e. objects).
	ListTypeMap ListType = "map"
)

// A MapType is a type of map.
type MapType string

// Types of maps.
const (
	// MapTypeAtomic means that the map can only be entirely replaced by a
	// single manager.
	MapTypeAtomic MapType = "atomic"

	// MapTypeGranular means that the map supports separate managers updating
	// individual fields.
	MapTypeGranular MapType = "granular"
)

// A StructType is a type of struct.
type StructType string

// Struct types.
const (
	// StructTypeAtomic means that the struct can only be entirely replaced by a
	// single manager.
	StructTypeAtomic StructType = "atomic"

	// StructTypeGranular means that the struct supports separate managers
	// updating individual fields.
	StructTypeGranular StructType = "granular"
)

// ServerSideApplyOptions represents the server-side apply merge options that
// upjet needs to control.
// https://kubernetes.io/docs/reference/using-api/server-side-apply/#merge-strategy
type ServerSideApplyOptions struct {
	ListType   *ListType
	ListMapKey []string
	MapType    *MapType
	StructType *StructType
}

func (o ServerSideApplyOptions) String() string {
	m := ""

	if o.ListType != nil {
		m += fmt.Sprintf("+listType:%s\n", *o.ListType)
	}

	for _, k := range o.ListMapKey {
		m += fmt.Sprintf("+listMapKey:%s\n", k)
	}

	if o.MapType != nil {
		m += fmt.Sprintf("+mapType:%s\n", *o.MapType)
	}

	if o.StructType != nil {
		m += fmt.Sprintf("+structType:%s\n", *o.StructType)
	}

	return m
}
