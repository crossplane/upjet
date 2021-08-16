package meta

import (
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	AnnotationKeyState = "tf.crossplane.io/state"
)

// GetState returns the Terraform state annotation value on the resource.
func GetState(o metav1.Object) string {
	return o.GetAnnotations()[AnnotationKeyState]
}

// SetState sets the Terraform state annotation of the resource.
func SetState(o metav1.Object, state string) {
	meta.AddAnnotations(o, map[string]string{AnnotationKeyState: state})
}
