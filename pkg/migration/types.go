// Copyright 2022 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package migration

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// FinalizerPolicy denotes the policy regarding the managed reconciler's
// finalizer while deleting a managed resource.
type FinalizerPolicy string

const (
	// FinalizerPolicyRemove is the FinalizerPolicy for removing
	// the managed reconciler's finalizer from a managed resource.
	FinalizerPolicyRemove FinalizerPolicy = "Remove" // Default
)

// Resource categories
const (
	categoryUnknown Category = ""
	// CategoryClaim category for composite claim resources
	CategoryClaim Category = "claim"
	// CategoryComposite category for composite resources
	CategoryComposite Category = "composite"
	// CategoryComposition category for compositions
	CategoryComposition Category = "composition"
	// CategoryManaged category for managed resources
	CategoryManaged Category = "managed"
	// CategoryCrossplanePackage category for provider packages
	CategoryCrossplanePackage Category = "package"
)

// Plan represents a migration plan for migrating managed resources,
// and associated composites and claims from a migration source provider
// to a migration target provider.
type Plan struct {
	Version string `json:"version"`
	Spec    Spec   `json:"spec,omitempty"`
}

// Spec represents the specification of a migration plan
type Spec struct {
	// Steps are the migration plan's steps that are expected
	// to complete a migration when executed in order.
	Steps   []Step `json:"steps,omitempty"`
	stepMap map[string]*Step
}

// StepType is the type used to name a migration step
type StepType string

const (
	// StepTypeApply denotes an apply step
	StepTypeApply StepType = "Apply"
	// StepTypePatch denotes a patch step, where a resource is patched
	// using the given JSON patch document.
	StepTypePatch StepType = "Patch"
	// StepTypeDelete denotes a delete step
	StepTypeDelete StepType = "Delete"
	// StepTypeExec executes the command with provided args
	StepTypeExec StepType = "Exec"
)

// Step represents a step in the generated migration plan
type Step struct {
	// Name is the name of this Step
	Name string `json:"name"`
	// Description is description of the Step.
	Description string
	// Type is the type of this Step.
	// Can be one of Apply, Delete, etc.
	Type StepType `json:"type"`
	// Apply contains the information needed to run an StepTypeApply step.
	// Must be set when the Step.Type is StepTypeApply.
	Apply *ApplyStep `json:"apply,omitempty" yaml:"apply,omitempty"`
	// Patch contains the information needed to run a StepTypePatch step.
	Patch *PatchStep `json:"patch,omitempty" yaml:"patch,omitempty"`
	// Delete contains the information needed to run an StepTypeDelete step.
	// Must be set when the Step.Type is StepTypeDelete.
	Delete *DeleteStep `json:"delete,omitempty" yaml:"delete,omitempty"`
	// Exec contains the information needed to run a StepTypeExec step.
	Exec *ExecStep `json:"exec,omitempty" yaml:"exec,omitempty"`
	// ManualExecution string is to make copy/pasting easier for folks.
	ManualExecution []string `json:"manualExecution,omitempty" yaml:"manualExecution,omitempty"`
}

// ApplyStep represents an apply step in which an array of manifests
// is applied from the filesystem.
type ApplyStep struct {
	// Files denotes the paths of the manifest files to be applied.
	// The paths can either be relative or absolute.
	Files []string `json:"files,omitempty"`
}

// PatchType represents the patch type used in a patch operation
type PatchType string

const (
	// PatchTypeStrategic represents the strategic merge patch
	PatchTypeStrategic PatchType = "strategic"
	// PatchTypeMerge represents the RFC 7386 JSON merge patch
	PatchTypeMerge PatchType = "merge"
	// PatchTypeJSON represents the RFC 6902 JSON patch
	PatchTypeJSON PatchType = "json"
)

// PatchStep represents a patch step in which an array of manifests
// is used to patch resources.
type PatchStep struct {
	// Files denotes the paths of the manifest files
	// to be used as patch documents.
	// The paths can either be relative or absolute.
	Files []string `json:"files,omitempty"`
	// Type is the PatchType to be used in this step
	Type PatchType `json:"type,omitempty"`
}

// DeleteStep represents a deletion step with options
type DeleteStep struct {
	// Options represents the options to be used while deleting the resources
	// specified in Resources.
	Options *DeleteOptions `json:"options,omitempty"`
	// Resources is the array of resources to be deleted in this step
	Resources []Resource `json:"resources,omitempty"`
}

// DeleteOptions represent options to be used during deletion of
// a managed resource.
type DeleteOptions struct {
	// FinalizerPolicy denotes the policy to be used regarding
	// the managed reconciler's finalizer
	FinalizerPolicy *FinalizerPolicy `json:"finalizerPolicy,omitempty"`
}

// ExecStep represents an exec command with its arguments
type ExecStep struct {
	// Command is the command to run
	Command string `json:"command"`
	// Args is the arguments of the command
	Args []string `json:"args"`
}

// GroupVersionKind represents the GVK for an object's kind.
// schema.GroupVersionKind does not contain json the serialization tags
// for its fields, but we would like to serialize these as part of the
// migration plan.
type GroupVersionKind struct {
	// Group is the API group for the resource
	Group string `json:"group"`
	// Version is the API version for the resource
	Version string `json:"version"`
	// Kind is the kind name for the resource
	Kind string `json:"kind"`
}

type Resource struct {
	// GroupVersionKind holds the GVK for the resource's type
	// schema.GroupVersionKind is not embedded for consistent serialized names
	GroupVersionKind `json:",inline"`
	// Name is the name of the resource
	Name string `json:"name"`
}

// Category specifies if a resource is a Claim, Composite or a Managed resource
type Category string

// String returns a string representing the receiver Category.
func (c Category) String() string {
	return string(c)
}

// Metadata holds metadata for an object read from a Source
type Metadata struct {
	// Path uniquely identifies the path for this object on its Source
	Path string
	// colon separated list of parent `Path`s for fan-ins and fan-outs
	// Example: resources/a.yaml:resources/b.yaml
	Parents string
	// Category specifies if the associated resource is a Claim, Composite or a
	// Managed resource
	Category Category
}

// UnstructuredWithMetadata represents an unstructured.Unstructured
// together with the associated Metadata.
type UnstructuredWithMetadata struct {
	Object   unstructured.Unstructured
	Metadata Metadata
}
