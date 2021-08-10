// Copyright 2021 Upbound Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmdconfig

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// LoadStructFromYAMLFileIfSet reads the given environment variable, and if the
// variable is set, loads the corresponding file into a given struct if not nil.
func LoadStructFromYAMLFileIfSet(filePathEnvVar string, obj interface{}) (*viper.Viper, error) {
	v := viper.New()
	filePath, isSet := os.LookupEnv(filePathEnvVar)
	if !isSet {
		return v, nil
	}

	v.SetConfigFile(filePath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return nil, errors.WithMessagef(err, "viper cannot read the file located at '%v'", filePath)
	}

	if obj != nil && !reflect.ValueOf(obj).IsNil() {
		if err := v.Unmarshal(obj); err != nil {
			return nil, errors.WithMessagef(err, "viper cannot unmarshal the contents of file '%v' into the given go struct", filePath)
		}
	}

	return v, nil
}

// InitializeConfig binds all flags from `cmd` to a new `Viper` instance and
// loads configuration parameters from the file pointed by specified env. variable if
// the env. variable is set.
func InitializeConfig(cmd *cobra.Command, configFileEnvVarName string) (*viper.Viper, error) {
	v, err := LoadStructFromYAMLFileIfSet(configFileEnvVarName, nil) // Load AppConfig if config path env var is set
	if err != nil {
		return nil, err
	}

	if err := bindFlags(cmd, v); err != nil {
		return nil, err
	}

	return v, nil
}

func bindFlags(cmd *cobra.Command, v *viper.Viper) error {
	var resultErr error
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		// if an error has been recorded cancel traversal
		if resultErr != nil {
			return
		}
		envVarName := strings.ToUpper(strings.ReplaceAll(flag.Name, "-", "_"))
		if err := v.BindEnv(flag.Name, envVarName); err != nil {
			resultErr = err
			return
		}
		if err := v.BindPFlag(flag.Name, flag); err != nil {
			resultErr = err
			return
		}

		// if option is not set from command-line, try to set it from from (viper) env. variable or (viper) config file
		if !flag.Changed && v.IsSet(flag.Name) {
			if err := cmd.Flags().Set(flag.Name, fmt.Sprintf("%v", v.Get(flag.Name))); err != nil {
				resultErr = err
				return
			}
		}
	})
	return resultErr
}
