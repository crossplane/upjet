package markers

// Options represents marker options that Terrajet need to parse or set.
type Options struct {
	TerrajetOptions
	CrossplaneOptions
	KubebuilderOptions
}

// String returns a string representation of this Options object.
func (o Options) String() string {
	return o.TerrajetOptions.String() +
		o.CrossplaneOptions.String() +
		o.KubebuilderOptions.String()
}
