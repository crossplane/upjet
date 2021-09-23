package resource

// ConfigStore stores a global configuration
type ConfigStore struct {
	config map[string]*Configuration
}

// NewConfigStore builds and returns a ConfigStore
func NewConfigStore() ConfigStore {
	return ConfigStore{
		config: map[string]*Configuration{},
	}
}

// SetConfigForResource sets configuration for a give resource
func (cs *ConfigStore) SetConfigForResource(resource string, cfg *Configuration) {
	cs.config[resource] = cfg
}

// GetConfigForResource gets the configuration for a given resource
func (cs *ConfigStore) GetConfigForResource(resource string) *Configuration {
	return cs.config[resource]
}

// ConfigStoreBuilder collects functions that add things to ConfigStore.
type ConfigStoreBuilder []func(*ConfigStore) error

// AddToConfigStore applies all the setup functions in the store
func (cb *ConfigStoreBuilder) AddToConfigStore(c *ConfigStore) error {
	for _, f := range *cb {
		if err := f(c); err != nil {
			return err
		}
	}
	return nil
}

// Register adds a config store setup function to the list
func (cb *ConfigStoreBuilder) Register(funcs ...func(*ConfigStore) error) {
	for _, f := range funcs {
		*cb = append(*cb, f)
	}
}
