// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package traverser

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pkg/errors"

	"github.com/crossplane/upjet/pkg/types/name"
)

const (
	// wildcard index for field path expressions
	wildcard = "*"
)

var _ Element = &SchemaNode{}
var _ Element = &ResourceNode{}

// Traverse traverses the Terraform schema of the given Terraform resource
// with the given Terraform resource name and visits each of the specified
// visitors on the traversed schema's nodes.
// If any of the visitors in the chain reports an error,
// it stops the traversal.
func Traverse(tfName string, tfResource *schema.Resource, visitors ...SchemaTraverser) error {
	if len(visitors) == 0 {
		return nil
	}
	return traverse(tfResource, Node{TFName: tfName}, visitors...)
}

// SchemaTraverser represents a visitor on the schema.Schema and
// schema.Resource nodes of a Terraform resource schema.
type SchemaTraverser interface {
	// VisitSchema is called on a Terraform schema.Schema node.
	VisitSchema(s *SchemaNode) error
	// VisitResource is called on a Terraform schema.Resource node.
	VisitResource(r *ResourceNode) error
}

// Element represents a schema element being visited and should Accept
// a visitor.
type Element interface {
	Accept(v SchemaTraverser) error
}

// Node represents a schema node that's being traversed.
type Node struct {
	// TFName is the Terraform resource name
	TFName string
	// Schema is the Terraform schema associated with the visited node during a
	// traversal.
	Schema *schema.Schema
	// CRDPath is the canonical CRD field path for the node being visited.
	CRDPath []string
	// TFPath is the canonical Terraform field path for the node being visited.
	TFPath []string
}

// SchemaNode represents a Terraform schema.Schema node.
type SchemaNode struct {
	Node
	// ElemSchema is the schema for the Terraform element of the node being
	// visited.
	ElemSchema *schema.Schema
}

func (s *SchemaNode) Accept(v SchemaTraverser) error {
	return v.VisitSchema(s)
}

// ResourceNode represents a Terraform schema.Resource node.
type ResourceNode struct {
	Node
	// ElemResource is the resource schema for the Terraform element
	// of the node being visited.
	ElemResource *schema.Resource
}

func (r *ResourceNode) Accept(v SchemaTraverser) error {
	return v.VisitResource(r)
}

func traverse(tfResource *schema.Resource, pNode Node, visitors ...SchemaTraverser) error { //nolint:gocyclo // traverse logic is easier to follow in a unit
	m := tfResource.Schema
	if m == nil && tfResource.SchemaFunc != nil {
		m = tfResource.SchemaFunc()
	}
	if m == nil {
		return nil
	}
	node := Node{TFName: pNode.TFName}
	for k, s := range m {
		node.CRDPath = append(pNode.CRDPath, name.NewFromSnake(k).LowerCamelComputed) //nolint:gocritic // the parent node's path must not be modified
		node.TFPath = append(pNode.TFPath, k)                                         //nolint:gocritic // the parent node's path must not be modified
		node.Schema = s
		switch e := s.Elem.(type) {
		case *schema.Schema:
			for _, v := range visitors {
				n := SchemaNode{
					Node:       node,
					ElemSchema: e,
				}
				if err := n.Accept(v); err != nil {
					return errors.Wrapf(err, "failed to visit the *schema.Schema node at path %s", FieldPathWithWildcard(node.TFPath))
				}
			}
		case *schema.Resource:
			n := ResourceNode{
				Node:         node,
				ElemResource: e,
			}
			for _, v := range visitors {
				if err := n.Accept(v); err != nil {
					return errors.Wrapf(err, "failed to visit the *schema.Resource node at path %s", FieldPathWithWildcard(node.TFPath))
				}
			}
			// only list and set types support an elem type of resource.
			node.CRDPath = append(node.CRDPath, wildcard)
			node.TFPath = append(node.TFPath, wildcard)
			if err := traverse(e, node, visitors...); err != nil {
				return err
			}
		}
	}
	return nil
}

// NoopTraverser is a traverser that doesn't do anything when visiting a node.
// Meant to make the implementation of visitors easy for the cases they are not
// interested in a specific node type.
type NoopTraverser struct{}

func (n NoopTraverser) VisitSchema(*SchemaNode) error {
	return nil
}

func (n NoopTraverser) VisitResource(*ResourceNode) error {
	return nil
}

// TFResourceSchema represents a provider's Terraform resource schema.
type TFResourceSchema map[string]*schema.Resource

// Traverse traverses the receiver schema using the specified
// visitors. Reports any errors encountered by the visitors.
func (s TFResourceSchema) Traverse(visitors ...SchemaTraverser) error {
	for n, r := range s {
		if err := Traverse(n, r, visitors...); err != nil {
			return errors.Wrapf(err, "failed to traverse the schema of the Terraform resource with name %q", n)
		}
	}
	return nil
}
