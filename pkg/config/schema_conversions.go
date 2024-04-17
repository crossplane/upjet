// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/crossplane/upjet/pkg/schema/traverser"
)

var _ ResourceSetter = &SingletonListEmbedder{}

// ResourceSetter allows the context Resource to be set for a traverser.
type ResourceSetter interface {
	SetResource(r *Resource)
}

func traverseSchemas(tfName string, tfResource *schema.Resource, r *Resource, visitors ...traverser.SchemaTraverser) error {
	// set the upjet Resource configuration as context for the visitors that
	// satisfy the ResourceSetter interface.
	for _, v := range visitors {
		if rs, ok := v.(ResourceSetter); ok {
			rs.SetResource(r)
		}
	}
	return traverser.Traverse(tfName, tfResource, visitors...)
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
	l.r.AddSingletonListConversion(traverser.FieldPathWithWildcard(r.TFPath))
	return nil
}
