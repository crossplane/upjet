package markers

// Options represents marker options that Upjet need to parse or set.
type Options struct {
	UpjetOptions
	CrossplaneOptions
	KubebuilderOptions
}

// String returns a string representation of this Options object.
func (o Options) String() string {
	return o.UpjetOptions.String() +
		o.CrossplaneOptions.String() +
		o.KubebuilderOptions.String()
}
