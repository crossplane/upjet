// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

//go:generate go run github.com/golang/mock/mockgen -copyright_file ../../../hack/boilerplate.txt -destination=./mocks/mock.go -package mocks github.com/crossplane/crossplane-runtime/pkg/resource Managed

package fake

import (
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/crossplane/upjet/pkg/migration/fake/mocks"
)

const (
	MigrationSourceGroup   = "fakesourceapi"
	MigrationSourceVersion = "v1alpha1"
	MigrationSourceKind    = "VPC"

	MigrationTargetGroup   = "faketargetapi"
	MigrationTargetVersion = "v1alpha1"
	MigrationTargetKind    = "VPC"
)

var (
	MigrationSourceGVK = schema.GroupVersionKind{
		Group:   MigrationSourceGroup,
		Version: MigrationSourceVersion,
		Kind:    MigrationSourceKind,
	}

	MigrationTargetGVK = schema.GroupVersionKind{
		Group:   MigrationTargetGroup,
		Version: MigrationTargetVersion,
		Kind:    MigrationTargetKind,
	}
)

type MigrationSourceObject struct {
	mocks.MockManaged
	// cannot inline v1.TypeMeta here as mocks.MockManaged is also inlined
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	// cannot inline v1.ObjectMeta here as mocks.MockManaged is also inlined
	ObjectMeta ObjectMeta `json:"metadata,omitempty"`
	Spec       SourceSpec `json:"spec"`
	Status     Status     `json:"status,omitempty"`
}

type SourceSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       SourceSpecParameters `json:"forProvider"`
}

type EmbeddedParameter struct {
	Param *string `json:"param,omitempty"`
}

type SourceSpecParameters struct {
	Region    *string            `json:"region,omitempty"`
	CIDRBlock string             `json:"cidrBlock"`
	Tags      []Tag              `json:"tags,omitempty"`
	TestParam *EmbeddedParameter `json:",inline"`
}

type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Status struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          Observation `json:"atProvider,omitempty"`
}

type Observation struct{}

func (m *MigrationSourceObject) GetName() string {
	return m.ObjectMeta.Name
}

type MigrationTargetObject struct {
	mocks.MockManaged
	// cannot inline v1.TypeMeta here as mocks.MockManaged is also inlined
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	// cannot inline v1.ObjectMeta here as mocks.MockManaged is also inlined
	ObjectMeta ObjectMeta `json:"metadata,omitempty"`
	Spec       TargetSpec `json:"spec"`
	Status     Status     `json:"status,omitempty"`
}

type ObjectMeta struct {
	Name         string            `json:"name,omitempty"`
	GenerateName string            `json:"generateName,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

type TargetSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       TargetSpecParameters `json:"forProvider"`
}

type TargetSpecParameters struct {
	Region    *string           `json:"region,omitempty"`
	CIDRBlock string            `json:"cidrBlock"`
	Tags      map[string]string `json:"tags,omitempty"`
	TestParam EmbeddedParameter `json:",inline"`
}

type targetObjectKind struct{}

func (t *targetObjectKind) SetGroupVersionKind(_ schema.GroupVersionKind) {}

func (t *targetObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return MigrationTargetGVK
}

func (m *MigrationTargetObject) GetObjectKind() schema.ObjectKind {
	return &targetObjectKind{}
}

func (m *MigrationTargetObject) GetName() string {
	return m.ObjectMeta.Name
}

func (m *MigrationTargetObject) GetGenerateName() string {
	return m.ObjectMeta.GenerateName
}
