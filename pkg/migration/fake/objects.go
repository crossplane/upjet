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

//go:generate go run github.com/golang/mock/mockgen -copyright_file ../../../hack/boilerplate.txt -destination=./mocks/mock.go -package mocks github.com/crossplane/crossplane-runtime/pkg/resource Managed

package fake

import (
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/upbound/upjet/pkg/migration/fake/mocks"
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

type SourceSpecParameters struct {
	Region    *string `json:"region,omitempty"`
	CIDRBlock string  `json:"cidrBlock"`
	Tags      []Tag   `json:"tags,omitempty"`
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
	Name string `json:"name,omitempty"`
}

type TargetSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       TargetSpecParameters `json:"forProvider"`
}

type TargetSpecParameters struct {
	Region    *string           `json:"region,omitempty"`
	CIDRBlock string            `json:"cidrBlock"`
	Tags      map[string]string `json:"tags,omitempty"`
}
