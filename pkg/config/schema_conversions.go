// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/v2/pkg/schema/traverser"
)

var _ ResourceSetter = &SingletonListEmbedder{}

// ResourceSetter allows the context Resource to be set for a traverser.
type ResourceSetter interface {
	SetResource(r *Resource)
}

// ResourceSchema represents a provider's resource schema.
type ResourceSchema map[string]*Resource

// TraverseTFSchemas traverses the Terraform schemas of all the resources of
// the Provider `p` using the specified visitors. Reports any errors
// encountered.
func (s ResourceSchema) TraverseTFSchemas(visitors ...traverser.SchemaTraverser) error {
	for name, cfg := range s {
		if err := TraverseSchemas(name, cfg, visitors...); err != nil {
			return errors.Wrapf(err, "failed to traverse the schema of the Terraform resource with name %q", name)
		}
	}
	return nil
}

// TraverseSchemas visits the specified schema belonging to the Terraform
// resource with the given name and given upjet resource configuration using
// the specified visitors. If any visitors report an error, traversal is
// stopped and the error is reported to the caller.
func TraverseSchemas(tfName string, r *Resource, visitors ...traverser.SchemaTraverser) error {
	// set the upjet Resource configuration as context for the visitors that
	// satisfy the ResourceSetter interface.
	for _, v := range visitors {
		if rs, ok := v.(ResourceSetter); ok {
			rs.SetResource(r)
		}
	}
	return traverser.Traverse(tfName, r.TerraformResource, visitors...)
}

type resourceContext struct {
	r *Resource
}

func (rc *resourceContext) SetResource(r *Resource) {
	rc.r = r
}

// SingletonListEmbedder is a schema traverser for embedding singleton lists
// in the Terraform schema as objects.
type SingletonListEmbedder struct {
	resourceContext
	traverser.NoopTraverser
}

func (l *SingletonListEmbedder) VisitResource(r *traverser.ResourceNode) error {
	// this visitor only works on sets and lists with the MaxItems constraint
	// of 1.
	if r.Schema.Type != schema.TypeList && r.Schema.Type != schema.TypeSet {
		return nil
	}
	if r.Schema.MaxItems != 1 {
		return nil
	}
	l.r.AddSingletonListConversion(traverser.FieldPathWithWildcard(r.TFPath), traverser.FieldPathWithWildcard(r.CRDPath))
	return nil
}
