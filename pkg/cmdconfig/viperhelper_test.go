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
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/cobra"
)

const (
	envAppConfig                  = "APP_CONFIG"
	flagDebug                     = "debug"
	flagHeartbeatUpdateServiceURL = "hb-service-url"
	flagNatsMonitoringServiceURL  = "nats-mon-url"
	flagParallelism               = "parallelism"
	flagPageSize                  = "page-size"
	flagPollInterval              = "poll-interval"
	flagHTTPRequestTimeout        = "http-req-timeout"
	valHeartbeatUpdateServiceURL  = ""
	valNatsMonitoringServiceURL   = "https://nats.nats-system:8222"
	valParallelism                = 1
	valPageSize                   = 1000
	valPollInterval               = 10 * time.Second
	valHTTPRequestTimeout         = 1 * time.Second
)

func TestInitializeConfig(t *testing.T) {
	type args struct {
		configFile string
		envMap     map[string]string
	}
	tests := map[string]struct {
		args       args
		wantError  bool
		wantConfig config
	}{
		"NoConfigFromConfigFile": {
			wantConfig: config{
				DebugMode:                 false,
				HeartbeatUpdateServiceURL: valHeartbeatUpdateServiceURL,
				NATSMonitoringServiceURL:  valNatsMonitoringServiceURL,
				Parallelism:               valParallelism,
				PageSize:                  valPageSize,
				PollInterval:              valPollInterval,
				HTTPRequestTimeout:        valHTTPRequestTimeout,
			},
		},
		"AllConfigFromConfigFile": {
			args: args{
				configFile: "testdata/config.yaml",
			},
			wantConfig: config{
				DebugMode:                 true,
				HeartbeatUpdateServiceURL: "test-hb-service-url",
				NATSMonitoringServiceURL:  "test-nats-mon-url",
				Parallelism:               -1,
				PageSize:                  -111,
				PollInterval:              9 * time.Minute,
				HTTPRequestTimeout:        999 * time.Second,
			},
		},
		"EnvOverridesConfigFile": {
			args: args{
				configFile: "testdata/config.yaml",
				envMap: map[string]string{
					"HB_SERVICE_URL": "a.b.c",
					"PARALLELISM":    "5",
					"POLL_INTERVAL":  "2m",
					"DEBUG":          "false",
				},
			},
			wantConfig: config{
				DebugMode:                 false,
				HeartbeatUpdateServiceURL: "a.b.c",
				NATSMonitoringServiceURL:  "test-nats-mon-url",
				Parallelism:               5,
				PageSize:                  -111,
				PollInterval:              2 * time.Minute,
				HTTPRequestTimeout:        999 * time.Second,
			},
		},
		"ErrInvalidConfigFile": {
			args: args{
				configFile: "testdata/invalid_config.txt",
			},
			wantError: true,
		},
		"ErrIncorrectConfigFilePath": {
			args: args{
				configFile: "testdata/non_existent",
			},
			wantError: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.args.configFile != "" {
				defer setEnvFunc(envAppConfig, tt.args.configFile, t)()
			}

			for k, v := range tt.args.envMap {
				defer setEnvFunc(k, v, t)()
			}

			cmd, conf := getTestCmd()
			_, err := InitializeConfig(cmd, envAppConfig)

			if (err != nil) != tt.wantError {
				t.Errorf("InitializeConfig(...) error = %v, wantError %v", err, tt.wantError)
				return
			}
			if err != nil {
				return
			}

			if diff := cmp.Diff(tt.wantConfig, *conf); diff != "" {
				t.Fatalf("InitializeConfig(...): -want config, +got config: %s", diff)
			}
		})
	}
}

func setEnvFunc(env, val string, t *testing.T) func() {
	testName := t.Name()
	oldVal, ok := os.LookupEnv(env)

	if err := os.Setenv(env, val); err != nil {
		t.Errorf("Could not set env variable %q to %q in test %q",
			env, val, testName)
	}

	return func() {
		if ok {
			if err := os.Setenv(env, oldVal); err != nil {
				t.Fatalf("could not reset env variable %q to %q in test %q",
					env, oldVal, testName)
			}

			return
		}

		if err := os.Unsetenv(env); err != nil {
			t.Fatalf("could not unset previously env variable %q in test %q",
				env, testName)
		}
	}
}

type config struct {
	// DebugMode enables debug level logging
	DebugMode bool `mapstructure:"debug" yaml:"debug"`
	// HeartbeatUpdateServiceURL service URL of the hearbeat batch-update private API
	// e.g., http://api-private.upbound-system:8081/v1/environments/heartbeats
	HeartbeatUpdateServiceURL string `mapstructure:"hb-service-url" yaml:"hb-service-url"`
	// NATSMonitoringServiceURL URL of the NATS-server monitoring service
	// e.g., http://nats.nats-system:8222
	NATSMonitoringServiceURL string `mapstructure:"nats-mon-url" yaml:"nats-mon-url"`
	// Parallelism number of parallel HTTP clients to run to fetch NATS-connection info
	// and to batch-update heartbeats via the private API
	Parallelism int `mapstructure:"parallelism" yaml:"parallelism"`
	// PageSize max number of NATS-connection info to retrieve (and update)
	// in a single request of a parallel client. Parallelism * PageSize are retrieved
	// in parallel.
	PageSize int `mapstructure:"page-size" yaml:"page-size"`
	// PollInterval time.Duration string that specifies polling interval of the
	// NATS monitoring service
	PollInterval time.Duration `mapstructure:"poll-interval" yaml:"poll-interval"`
	// HTTPRequestTimeout timeout for HTTP requests
	HTTPRequestTimeout time.Duration `mapstructure:"http-req-timeout" yaml:"http-req-timeout"`
}

func getTestCmd() (*cobra.Command, *config) {
	conf := &config{}
	cmd := &cobra.Command{
		Use:   "nats-monitoring",
		Short: "NATS-based health-checks",
		Long:  "Runs NATS-based health-check monitoring server",
	}

	cmd.Flags().BoolVarP(&conf.DebugMode, flagDebug, "d", false, "Run with debug logging")
	cmd.Flags().StringVar(&conf.HeartbeatUpdateServiceURL, flagHeartbeatUpdateServiceURL, valHeartbeatUpdateServiceURL,
		"Upbound API heartbeat batch-update service URL")
	cmd.Flags().StringVar(&conf.NATSMonitoringServiceURL, flagNatsMonitoringServiceURL, valNatsMonitoringServiceURL,
		"NATS-monitoring service URL")
	cmd.Flags().IntVarP(&conf.Parallelism, flagParallelism, "p", valParallelism,
		"Number of concurrent HTTP clients to fetch & update heartbeat info")
	cmd.Flags().IntVarP(&conf.PageSize, flagPageSize, "s", valPageSize,
		"`limit` query parameter to set on requests to NATS monitoring service")
	cmd.Flags().DurationVar(&conf.PollInterval, flagPollInterval, valPollInterval,
		"Poll interval for the NATS monitoring service")
	cmd.Flags().DurationVar(&conf.HTTPRequestTimeout, flagHTTPRequestTimeout, valHTTPRequestTimeout,
		"HTTP request timeout")

	return cmd, conf
}

func TestLoadStructFromYAMLFileIfSet(t *testing.T) {
	type args struct {
		configObj *config
	}
	// other cases are tested via TestInitializeConfig
	tests := map[string]struct {
		args       args
		wantConfig *config
	}{
		"NilConfigObject": {},
		"NonNilConfigObject": {
			args: args{
				configObj: &config{},
			},
			wantConfig: &config{
				DebugMode:                 true,
				HeartbeatUpdateServiceURL: "test-hb-service-url",
				NATSMonitoringServiceURL:  "test-nats-mon-url",
				Parallelism:               -1,
				PageSize:                  -111,
				PollInterval:              9 * time.Minute,
				HTTPRequestTimeout:        999 * time.Second,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			defer setEnvFunc(envAppConfig, "testdata/config.yaml", t)()

			_, err := LoadStructFromYAMLFileIfSet(envAppConfig, tt.args.configObj)
			if err != nil {
				t.Errorf("Unexpected error in LoadStructFromYAMLFileIfSet(...): %v", err)
				return
			}

			if diff := cmp.Diff(tt.wantConfig, tt.args.configObj); diff != "" {
				t.Fatalf("InitializeConfig(...): -want config, +got config: %s", diff)
			}
		})
	}
}
