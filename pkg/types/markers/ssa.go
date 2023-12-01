// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package markers

import (
	"fmt"

	"github.com/crossplane/upjet/pkg/config"
)

// ServerSideApplyOptions represents the server-side apply merge options that
// upjet needs to control.
// https://kubernetes.io/docs/reference/using-api/server-side-apply/#merge-strategy
type ServerSideApplyOptions struct {
	ListType   *config.ListType
	ListMapKey []string
	MapType    *config.MapType
	StructType *config.StructType
}

func (o ServerSideApplyOptions) String() string {
	m := ""

	if o.ListType != nil {
		m += fmt.Sprintf("+listType=%s\n", *o.ListType)
	}

	for _, k := range o.ListMapKey {
		m += fmt.Sprintf("+listMapKey=%s\n", k)
	}

	if o.MapType != nil {
		m += fmt.Sprintf("+mapType=%s\n", *o.MapType)
	}

	if o.StructType != nil {
		m += fmt.Sprintf("+structType=%s\n", *o.StructType)
	}

	return m
}
