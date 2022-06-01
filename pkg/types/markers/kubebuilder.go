package markers

import "fmt"

// KubebuilderOptions represents the kubebuilder options that upjet would
// need to control
type KubebuilderOptions struct {
	Required *bool
	Minimum  *int
	Maximum  *int
}

func (o KubebuilderOptions) String() string {
	m := ""

	if o.Required != nil {
		if *o.Required {
			m += "+kubebuilder:validation:Required\n"
		} else {
			m += "+kubebuilder:validation:Optional\n"
		}
	}
	if o.Minimum != nil {
		m += fmt.Sprintf("+kubebuilder:validation:Minimum=%d\n", *o.Minimum)
	}
	if o.Maximum != nil {
		m += fmt.Sprintf("+kubebuilder:validation:Maximum=%d\n", *o.Maximum)
	}

	return m
}
