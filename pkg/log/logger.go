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
