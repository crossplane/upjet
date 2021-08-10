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
