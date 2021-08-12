/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package log

import (
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/go-logr/logr"
)

// NewLoggerWithServiceContext creates an instance of logr.Logger configured to write logs in Stackdriver acceptable
// format.
func NewLoggerWithServiceContext(service string, version string, debug bool) logr.Logger {
	zl := zap.New(
		zap.UseDevMode(debug),
		zap.Encoder(zapcore.NewJSONEncoder(newEncoderConfig())),
		zap.RawZapOpts(WrapCore()))

	return zl.WithValues(ServiceContextKey,
		ServiceContext{Service: service, Version: version})
}
