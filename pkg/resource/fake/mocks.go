/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fake

import (
	"github.com/crossplane/crossplane-runtime/pkg/resource/fake"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
)

type Observable struct {
	Observation map[string]interface{}
}

func (o *Observable) GetObservation() (map[string]interface{}, error) {
	return o.Observation, nil
}

func (o *Observable) SetObservation(data map[string]interface{}) error {
	o.Observation = data
	return nil
}

type Parameterizable struct {
	Parameters map[string]interface{}
}

func (p *Parameterizable) GetParameters() (map[string]interface{}, error) {
	return p.Parameters, nil
}

func (p *Parameterizable) SetParameters(data map[string]interface{}) error {
	p.Parameters = data
	return nil
}

type MetadataProvider struct {
	Type    string
	IdField string
}

func (mp *MetadataProvider) GetTerraformResourceType() string {
	return mp.Type
}

func (mp *MetadataProvider) GetTerraformResourceIdField() string {
	return mp.IdField
}

// Terraformed is a mock that implements Terraformed interface.
type Terraformed struct {
	fake.Managed
	Observable
	Parameterizable
	MetadataProvider
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
