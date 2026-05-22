// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package testconversion

import (
	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/crossplane/upjet/v2/pkg/resource"
)

// TestResource is a minimal Terraformed resource for testing conversions.
// It mimics the structure of real generated resources with spec.forProvider.
type TestResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TestResourceSpec   `json:"spec"`
	Status TestResourceStatus `json:"status,omitempty"`

	// TerraformResourceType allows tests to set the resource type
	// Must be exported for runtime conversion to work
	TerraformResourceType string `json:"-"`
}

// TestResourceSpec defines the desired state of TestResource
type TestResourceSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       TestResourceParameters `json:"forProvider"`
}

// TestResourceParameters are the configurable fields of a TestResource.
type TestResourceParameters map[string]interface{}

// TestResourceObservation are the observable fields of a TestResource.
type TestResourceObservation map[string]interface{}

// TestResourceStatus defines the observed state of TestResource.
type TestResourceStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          TestResourceObservation `json:"atProvider,omitempty"`
}

// Ensure TestResource implements resource.Terraformed
var _ resource.Terraformed = &TestResource{}

// GetCondition of this TestResource.
func (tr *TestResource) GetCondition(ct xpv1.ConditionType) xpv1.Condition {
	return tr.Status.GetCondition(ct)
}

// SetConditions of this TestResource.
func (tr *TestResource) SetConditions(c ...xpv1.Condition) {
	tr.Status.SetConditions(c...)
}

// GetDeletionPolicy of this TestResource.
func (tr *TestResource) GetDeletionPolicy() xpv1.DeletionPolicy {
	return tr.Spec.DeletionPolicy
}

// SetDeletionPolicy of this TestResource.
func (tr *TestResource) SetDeletionPolicy(r xpv1.DeletionPolicy) {
	tr.Spec.DeletionPolicy = r
}

// GetManagementPolicies of this TestResource.
func (tr *TestResource) GetManagementPolicies() xpv1.ManagementPolicies {
	return tr.Spec.ManagementPolicies
}

// SetManagementPolicies of this TestResource.
func (tr *TestResource) SetManagementPolicies(r xpv1.ManagementPolicies) {
	tr.Spec.ManagementPolicies = r
}

// GetProviderConfigReference of this TestResource.
func (tr *TestResource) GetProviderConfigReference() *xpv1.Reference {
	return tr.Spec.ProviderConfigReference
}

// SetProviderConfigReference of this TestResource.
func (tr *TestResource) SetProviderConfigReference(r *xpv1.Reference) {
	tr.Spec.ProviderConfigReference = r
}

// GetWriteConnectionSecretToReference of this TestResource.
func (tr *TestResource) GetWriteConnectionSecretToReference() *xpv1.SecretReference {
	return tr.Spec.WriteConnectionSecretToReference
}

// SetWriteConnectionSecretToReference of this TestResource.
func (tr *TestResource) SetWriteConnectionSecretToReference(r *xpv1.SecretReference) {
	tr.Spec.WriteConnectionSecretToReference = r
}

// GetObservation of this TestResource
func (tr *TestResource) GetObservation() (map[string]any, error) {
	return map[string]any(tr.Status.AtProvider), nil
}

// SetObservation of this TestResource
func (tr *TestResource) SetObservation(data map[string]any) error {
	tr.Status.AtProvider = TestResourceObservation(data)
	return nil
}

// GetID returns the ID of this TestResource.
func (tr *TestResource) GetID() string {
	if id, ok := tr.Status.AtProvider["id"]; ok {
		if idStr, ok := id.(string); ok {
			return idStr
		}
	}
	return ""
}

// GetParameters of this TestResource
func (tr *TestResource) GetParameters() (map[string]any, error) {
	return map[string]any(tr.Spec.ForProvider), nil
}

// SetParameters of this TestResource
func (tr *TestResource) SetParameters(params map[string]any) error {
	tr.Spec.ForProvider = TestResourceParameters(params)
	return nil
}

// GetInitParameters of this TestResource
func (tr *TestResource) GetInitParameters() (map[string]any, error) {
	return nil, nil
}

// GetMergedParameters of this TestResource
func (tr *TestResource) GetMergedParameters(shouldMergeInitProvider bool) (map[string]any, error) {
	return map[string]any(tr.Spec.ForProvider), nil
}

// LateInitialize this TestResource
func (tr *TestResource) LateInitialize(attrs []byte) (bool, error) {
	return false, nil
}

// GetTerraformResourceType returns the Terraform resource type
func (tr *TestResource) GetTerraformResourceType() string {
	if tr.TerraformResourceType != "" {
		return tr.TerraformResourceType
	}
	return "test_resource"
}

// GetTerraformSchemaVersion returns the schema version
func (tr *TestResource) GetTerraformSchemaVersion() int {
	return 0
}

// GetConnectionDetailsMapping returns the connection details mapping
func (tr *TestResource) GetConnectionDetailsMapping() map[string]string {
	return nil
}

// GetObjectKind returns the ObjectKind schema
func (tr *TestResource) GetObjectKind() schema.ObjectKind {
	return &tr.TypeMeta
}

// DeepCopyObject returns a deep copy
func (tr *TestResource) DeepCopyObject() runtime.Object {
	out := &TestResource{}
	out.TypeMeta = tr.TypeMeta
	out.ObjectMeta = *tr.ObjectMeta.DeepCopy()
	out.Spec.ForProvider = make(TestResourceParameters)
	for k, v := range tr.Spec.ForProvider {
		out.Spec.ForProvider[k] = v
	}
	out.Status.AtProvider = make(TestResourceObservation)
	for k, v := range tr.Status.AtProvider {
		out.Status.AtProvider[k] = v
	}
	return out
}
