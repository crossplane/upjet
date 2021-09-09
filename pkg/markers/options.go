package markers

type Options struct {
	TerrajetOptions
	CrossplaneOptions
	KubebuilderOptions
}

func (o Options) String() string {
	return o.TerrajetOptions.String() +
		o.CrossplaneOptions.String() +
		o.KubebuilderOptions.String()
}
