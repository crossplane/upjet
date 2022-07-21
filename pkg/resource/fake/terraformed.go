/*
Copyright 2021 Upbound Inc.
*/

package fake

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"

	"github.com/crossplane/crossplane-runtime/pkg/resource/fake"
)

// Observable is mock Observable.
type Observable struct {
	Observation                 map[string]any
	AdditionalConnectionDetails map[string][]byte
	ID                          string
}

// GetObservation is a mock.
func (o *Observable) GetObservation() (map[string]any, error) {
	return o.Observation, nil
}

// SetObservation is a mock.
func (o *Observable) SetObservation(data map[string]any) error {
	o.Observation = data
	return nil
}

// GetID is a mock.
func (o *Observable) GetID() string {
	return o.ID
}

// GetAdditionalConnectionDetails is a mock
func (o *Observable) GetAdditionalConnectionDetails(_ map[string]any) (map[string][]byte, error) {
	return o.AdditionalConnectionDetails, nil
}

// Parameterizable is mock Parameterizable.
type Parameterizable struct {
	Parameters map[string]any
}

// GetParameters is a mock.
func (p *Parameterizable) GetParameters() (map[string]any, error) {
	return p.Parameters, nil
}

// SetParameters is a mock.
func (p *Parameterizable) SetParameters(data map[string]any) error {
	p.Parameters = data
	return nil
}

// MetadataProvider is mock MetadataProvider.
type MetadataProvider struct {
	Type                     string
	SchemaVersion            int
	ConnectionDetailsMapping map[string]string
}

// GetTerraformResourceType is a mock.
func (mp *MetadataProvider) GetTerraformResourceType() string {
	return mp.Type
}

// GetTerraformSchemaVersion is a mock.
func (mp *MetadataProvider) GetTerraformSchemaVersion() int {
	return mp.SchemaVersion
}

// GetConnectionDetailsMapping is a mock.
func (mp *MetadataProvider) GetConnectionDetailsMapping() map[string]string {
	return mp.ConnectionDetailsMapping
}

// LateInitializer is mock LateInitializer.
type LateInitializer struct {
	Result bool
	Err    error
}

// LateInitialize is a mock.
func (li *LateInitializer) LateInitialize(_ []byte) (bool, error) {
	return li.Result, li.Err
}

// Terraformed is a mock that implements Terraformed interface.
type Terraformed struct {
	fake.Managed
	Observable
	Parameterizable
	MetadataProvider
	LateInitializer
}

// GetObjectKind returns schema.ObjectKind.
func (t *Terraformed) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

// DeepCopyObject returns a copy of the object as runtime.Object
func (t *Terraformed) DeepCopyObject() runtime.Object {
	out := &Terraformed{}
	j, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	_ = json.Unmarshal(j, out)
	return out
}
