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

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane-contrib/terrajet/pkg/cmdconfig"
	"github.com/crossplane-contrib/terrajet/pkg/log"
	"github.com/crossplane-contrib/terrajet/pkg/process"
	"github.com/crossplane-contrib/terrajet/pkg/version"
)

const (
	// command-line flags for nats-monitoring executable
	flagDebug      = "debug"
	flagTimeout    = "timeout"
	flagPort       = "port"
	flagPluginPath = "plugin-path"
	// default values for command-line arguments
	valTimeout    = 30 * time.Second
	valPort       = 9000
	valPluginPath = "/usr/local/bin/provider"
	// config file path env. variable
	appConfigFileEnvVarName = "APP_CONFIG_PATH"
	// provider plugin binary path
)

// ProviderConfig maintains the configurations for the
// provider runner
type ProviderConfig struct {
	// Debug enables debug level logging
	Debug bool `mapstructure:"debug" yaml:"debug"`
	// Timeout for reading the address info from provider's stdout
	Timeout time.Duration `mapstructure:"timeout" yaml:"timeout"`
	// Port the provider will listen on
	Port int `mapstructure:"port" yaml:"port"`
	// PluginPath path of the provider executable
	PluginPath string `mapstructure:"plugin-path" yaml:"plugin-path"`
}

func (c ProviderConfig) String() (string, error) {
	buff, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(buff), nil
}

var logger logging.Logger

func main() {
	conf := &ProviderConfig{}
	// initialize a cobra command for nats-monitoring
	cmd := &cobra.Command{
		Use:   "provider-wrapper",
		Short: "Wrapper for provider",
		Long:  "Wraps and manages provider plugin and socat processes",
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if _, err := cmdconfig.InitializeConfig(cmd, appConfigFileEnvVarName); err != nil {
				panic(err)
			}
		},
		Run: func(_ *cobra.Command, _ []string) {
			startProvider(conf)
		},
	}

	cmd.Flags().BoolVarP(&conf.Debug, flagDebug, "d", false, "Run with debug logging")
	cmd.Flags().IntVarP(&conf.Port, flagPort, "p", valPort, "TCP port the provider will listen")
	cmd.Flags().StringVar(&conf.PluginPath, flagPluginPath, valPluginPath,
		"Path to the provider's plugin binary")
	cmd.Flags().DurationVarP(&conf.Timeout, flagTimeout, "t", valTimeout,
		"Timeout for reading provider's address")

	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}

type providerAddress struct {
	// Address the provider listens on
	Address string `json:"address"`
	// Network the provider listens on
	Network string `json:"network"`
}

func startProvider(conf *ProviderConfig) {
	logWithServiceContext := log.NewLoggerWithServiceContext("provider", version.Version,
		conf.Debug)
	logger = logging.NewLogrLogger(logWithServiceContext)

	confString, err := conf.String()
	if err != nil {
		panic(err)
	}
	logger.Info("Starting provider...", "configuration", confString)

	chAddrInfo := make(chan *providerAddress)
	providerProcess, err := process.New(conf.PluginPath, nil, "",
		true, false, true, logger)
	if err != nil {
		panic(err)
	}
	providerProcess.StartStderr(func(line string, logger logging.Logger) {
		addr := &providerAddress{}
		if err := json.Unmarshal([]byte(line), addr); err != nil {
			logger.Info("Failed to unmarshall line", "error", err)
		}
		if addr.Address != "" && addr.Network != "" {
			chAddrInfo <- addr
		}
	})
	providerProcess.LogStdout()

	var socatProcess *process.Info
	timeout := time.After(conf.Timeout)
	select {
	case <-timeout:
		panic(errors.New("timed out while waiting for provider to report its address"))

	case addrInfo := <-chAddrInfo:
		if addrInfo.Network != "unix" {
			panic(errors.Errorf("unknown provider network: %s", addrInfo.Network))
		}
		args := []string{
			fmt.Sprintf("TCP-LISTEN:%d,fork", conf.Port),
			fmt.Sprintf("UNIX-CONNECT:%s", addrInfo.Address),
		}
		if conf.Debug {
			args = append([]string{"-d", "-d"}, args...)
		}
		socatProcess, err = process.New("socat", args, "",
			true, false, true, logger)
		if err != nil {
			panic(err)
		}
		socatProcess.Log()
	}

	go providerProcess.Wait()
	go socatProcess.Wait()

	select {
	case <-providerProcess.Stopped():
	case <-socatProcess.Stopped():
		os.Exit(1)
	}
}
