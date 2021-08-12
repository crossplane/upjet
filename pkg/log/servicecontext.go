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
