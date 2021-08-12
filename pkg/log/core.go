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
	"fmt"
	"math"
	"runtime/debug"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// core wraps zapcore.core to override Write method to generate Stackdriver Error Report
type core struct {
	zapcore.Core
}

// With wraps zapcore.With
func (c *core) With(fields []zapcore.Field) zapcore.Core {
	return &core{
		Core: c.Core.With(fields),
	}
}

// Check determines whether the supplied Entry should be logged (using the embedded LevelEnabler).
// If the entry should be logged, the core adds itself to the CheckedEntry and returns the result.
// Callers must use Check before calling Write.
func (c *core) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}

	return ce
}

func isError(entry zapcore.Entry, fields []zapcore.Field) bool {
	if entry.Level >= zapcore.ErrorLevel {
		return true
	}

	for _, f := range fields {
		if f.Type == zapcore.ErrorType {
			return true
		}
	}
	return false
}

// Write creates Stackdriver Error report if log entry's log level is at least error,
// or if fields contain a field of type Error.
// Reformatting of a non-error log entry to the Stackdriver's acceptable format is done via EncoderConfig.
func (c *core) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	if isError(entry, fields) {
		entry, fields = c.createErrorReport(entry, fields)
	}

	return c.Core.Write(entry, fields)
}

// Sync flushes buffered logs (if any).
func (c *core) Sync() error {
	return c.Core.Sync()
}

// WrapCore returns a `zap.Option` that wraps the default core with our core.
func WrapCore(options ...func(c *core)) zap.Option {
	return zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		newCore := &core{
			Core: c,
		}
		for _, option := range options {
			option(newCore)
		}
		return newCore
	})
}

func formatField(f zapcore.Field) string {
	value := ""

	switch f.Type {
	case zapcore.StringType:
		value = f.String
	case zapcore.Int8Type, zapcore.Int16Type, zapcore.Int32Type, zapcore.Int64Type, zapcore.DurationType,
		zapcore.Uint8Type, zapcore.Uint16Type, zapcore.Uint32Type, zapcore.Uint64Type, zapcore.UintptrType:
		value = fmt.Sprintf("%d", f.Integer)
	case zapcore.Float64Type:
		value = fmt.Sprintf("%f", math.Float64frombits(uint64(f.Integer)))
	case zapcore.Float32Type:
		value = fmt.Sprintf("%f", math.Float32frombits(uint32(f.Integer)))
	case zapcore.BoolType:
		value = fmt.Sprintf("%t", f.Integer == 1)
	// Errors are processed separately
	case zapcore.SkipType, zapcore.ErrorType:
	default:
		value = fmt.Sprintf("%v", f.Interface)
	}

	if value == "" {
		return ""
	}

	return fmt.Sprintf(" %s: %s;", f.Key, value)
}

// Creates Stackdriver Error report. For log entries to be recognizable by Error Reporting use the format described
// here: https://cloud.google.com/error-reporting/reference/rest/v1beta1/projects.events/report#ReportedErrorEvent,
// and here: https://cloud.google.com/error-reporting/docs/formatting-error-messages#json_representation
// If your log entry contains an exception stack trace, the exception stack trace should be set in entry.message field.
func (c *core) createErrorReport(ent zapcore.Entry, fields []zapcore.Field) (zapcore.Entry, []zapcore.Field) {
	// There is no place to add custom fields into ErrorReport other than error message itself.
	// All fields which are not ReportedErrorEvent fields (and therefore not recognized by Stackdriver) will be ignored.
	extraFields := ""
	userID := ""
	var req *HTTPRequestContext = nil
	for _, f := range fields {
		if f.Type == zapcore.ErrorType {
			ent.Message = f.Interface.(error).Error() + " " + ent.Message
		}

		switch f.Key {
		case HTTPRequestKey:
			req = f.Interface.(*HTTPRequestContext)
		case UserKey:
			userID = f.String
		default:
			extraFields += formatField(f)
		}
	}
	if extraFields != "" {
		ent.Message += ";" + extraFields
	}

	// Must be set to this! Otherwise Stackdriver does not generate Error Report.
	ent.Message += "\n" + string(debug.Stack())

	newFields := []zapcore.Field{
		zap.Time("eventTime", ent.Time),
		zap.Object(contextKey, &ErrorContext{
			User:        userID,
			HTTPRequest: req,
		}),
	}
	return ent, newFields
}
