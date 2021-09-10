package markers

import "fmt"

// CrossplaneOptions represents the Crossplane marker options that terrajet
// would need to interact
type CrossplaneOptions struct {
	ReferenceToType string
}

func (o CrossplaneOptions) String() string {
	m := ""

	if o.ReferenceToType != "" {
		m += fmt.Sprintf("+crossplane:generate:reference:type=%s\n", o.ReferenceToType)
	}

	return m
}
