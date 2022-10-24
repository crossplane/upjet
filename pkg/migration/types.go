/*
Copyright 2022 Upbound Inc.
*/

package migration

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FinalizerPolicy string

const (
	FinalizerPolicyRemove FinalizerPolicy = "Remove" // Default
)

type Plan struct {
	Version string `json:"version"`
	Spec    Spec   `json:"spec,omitempty"`
}

type Spec struct {
	Steps []Step `json:"steps,omitempty"`
}

type Step struct {
	Name   string      `json:"name"`
	Type   string      `json:"type"`
	Apply  *ApplyStep  `json:"apply,omitempty"`
	Delete *DeleteStep `json:"delete,omitempty"`
}

type ApplyStep struct {
	Files []string `json:"files"`
}

type DeleteStep struct {
	Options   *DeleteOptions `json:"options,omitempty"`
	Resources []Resource     `json:"resources"`
}

type DeleteOptions struct {
	FinalizerPolicy *FinalizerPolicy `json:"finalizerPolicy,omitempty"`
}

type Resource struct {
	schema.GroupVersionKind `json:",inline"`
	Name                    string `json:"name"`
}

type Metadata struct {
	Path string
	// colon separated list of parent `Path`s for fan-ins and fan-outs
	// Example: resources/a.yaml:resources/b.yaml
	Parents string
}

type UnstructuredWithMetadata struct {
	Object   unstructured.Unstructured
	Metadata Metadata
}
