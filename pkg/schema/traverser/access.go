// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package traverser

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"
)

// SchemaAccessor is a function that accesses and potentially modifies a
// Terraform schema.
type SchemaAccessor func(*schema.Schema) error

// AccessSchema accesses the schema element at the specified path and calls
// the given schema accessors in order. Reports any errors encountered by
// the accessors. The terminal node at the end of the specified path must be
// a *schema.Resource or an error will be reported. The specified path must
// have at least one component.
func AccessSchema(sch any, path []string, accessors ...SchemaAccessor) error { //nolint:gocyclo // easier to follow the flow
	if len(path) == 0 {
		return errors.New("empty path specified while accessing the Terraform resource schema")
	}
	k := path[0]
	path = path[1:]
	switch s := sch.(type) {
	case *schema.Schema:
		if len(path) == 0 {
			return errors.Errorf("terminal node at key %q is a *schema.Schema", k)
		}
		if k != wildcard {
			return errors.Errorf("expecting a wildcard key but encountered the key %q", k)
		}
		if err := AccessSchema(s.Elem, path, accessors...); err != nil {
			return err
		}
	case *schema.Resource:
		c := s.Schema[k]
		if c == nil {
			return errors.Errorf("schema element for key %q is nil", k)
		}
		if len(path) == 0 {
			for _, a := range accessors {
				if err := a(c); err != nil {
					return errors.Wrapf(err, "failed to call the accessor function on the schema element at key %q", k)
				}
			}
			return nil
		}
		if err := AccessSchema(c, path, accessors...); err != nil {
			return err
		}
	default:
		return errors.Errorf("unknown schema element type %T at key %q", s, k)
	}
	return nil
}
