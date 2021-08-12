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
	contextKey = "context"

	// UserKey is a key for User information in Stackdriver ErrorContext,
	// refer to https://cloud.google.com/error-reporting/reference/rest/v1beta1/ErrorContext
	UserKey = "user"
)

// ErrorContext see https://cloud.google.com/error-reporting/reference/rest/v1beta1/ErrorContext
type ErrorContext struct {
	User        string              `json:"user"`
	HTTPRequest *HTTPRequestContext `json:"httpRequest"`
}

// MarshalLogObject implements zapcore.ObjectMarshaler
func (c *ErrorContext) MarshalLogObject(e zapcore.ObjectEncoder) (err error) {
	if c.User != "" {
		e.AddString(UserKey, c.User)
	}

	if c.HTTPRequest != nil {
		if err = e.AddObject(HTTPRequestKey, c.HTTPRequest); err != nil {
			return
		}
	}
	return
}
