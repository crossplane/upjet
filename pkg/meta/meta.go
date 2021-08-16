package meta

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/crossplane/crossplane-runtime/pkg/meta"
)

const (
	// AnnotationKeyState is the annotation key to store base64 encoded Terraform state
	// as an annotation for the Terraform managed resource
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
