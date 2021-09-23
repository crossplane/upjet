package resource

type CustomConfiguration struct {
	// ExternalName allows you to specify a custom ExternalNameConfiguration.
	ExternalName map[string]*ExternalNameConfiguration

	Reference map[string]map[string]FieldReferenceConfiguration
	// TerraformIDFieldName is the name of the ID field in Terraform state of
	// the resource. Its default is "id" and in almost all cases, you don't need
	// to overwrite it.
	TerraformIDFieldName map[string]string
}

func NewCustomConfiguration() CustomConfiguration {
	return CustomConfiguration{
		ExternalName:         map[string]*ExternalNameConfiguration{},
		Reference:            map[string]map[string]FieldReferenceConfiguration{},
		TerraformIDFieldName: map[string]string{},
	}
}

type CustomConfigBuilder []func(*CustomConfiguration) error

func (cb *CustomConfigBuilder) AddToCustomConfig(c *CustomConfiguration) error {
	for _, f := range *cb {
		if err := f(c); err != nil {
			return err
		}
	}
	return nil
}

func (cb *CustomConfigBuilder) Register(funcs ...func(*CustomConfiguration) error) {
	for _, f := range funcs {
		*cb = append(*cb, f)
	}
}

func NewCustomConfigBuilder(funcs ...func(config *CustomConfiguration) error) CustomConfigBuilder {
	var cb CustomConfigBuilder
	cb.Register(funcs...)
	return cb
}
