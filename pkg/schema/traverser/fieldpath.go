// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package traverser

import "github.com/crossplane/crossplane-runtime/pkg/fieldpath"

const (
	// FieldPathWildcard is the wildcard expression in fieldpath indices.
	FieldPathWildcard = "*"
)

// FieldPathWithWildcard joins the given segment strings into a field path.
func FieldPathWithWildcard(parts []string) string {
	seg := make(fieldpath.Segments, len(parts))
	for i, p := range parts {
		seg[i] = fieldpath.Field(p)
	}
	return seg.String()
}

// FieldPath joins the given segment strings into a field path eliminating
// the wildcard index segments.
func FieldPath(parts []string) string {
	seg := make(fieldpath.Segments, len(parts))
	for i, p := range parts {
		if p == FieldPathWildcard {
			continue
		}
		seg[i] = fieldpath.Field(p)
	}
	return seg.String()
}
