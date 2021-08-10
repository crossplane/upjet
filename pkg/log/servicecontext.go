package log

import (
	"go.uber.org/zap/zapcore"
)

const (
	// ServiceContextKey is a key for ServiceContext information in Stackdriver ErrorEvent,
	// refer to https://cloud.google.com/error-reporting/reference/rest/v1beta1/ErrorEvent
	ServiceContextKey = "serviceContext"
)

// ServiceContext see https://cloud.google.com/error-reporting/reference/rest/v1beta1/ServiceContext
type ServiceContext struct {
	Service string `json:"service"`
	Version string `json:"version"`
}

// MarshalLogObject implements zapcore.ObjectMarshaler
func (s *ServiceContext) MarshalLogObject(e zapcore.ObjectEncoder) error {
	e.AddString("service", s.Service)
	e.AddString("version", s.Version)
	return nil
}
