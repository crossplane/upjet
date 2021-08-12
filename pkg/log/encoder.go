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

// Stackdriver log entry severity (see https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogSeverity):
// DEFAULT		(0) The log entry has no assigned severity level.
// DEBUG		(100) Debug or trace information.
// INFO			(200) Routine information, such as ongoing status or performance.
// NOTICE		(300) Normal but significant events, such as start up, shut down, or a configuration change.
// WARNING		(400) Warning events might cause problems.
// ERROR		(500) Error events are likely to cause problems.
// CRITICAL		(600) Critical events cause more severe problems or outages.
// ALERT		(700) A person must take an action immediately.
// EMERGENCY	(800) One or more systems are unusable.
func zapLevelToStackdriverSeverity(l zapcore.Level) string {
	switch l {
	case zapcore.WarnLevel:
		return "WARNING"
	case zapcore.DPanicLevel:
		return "CRITICAL"
	case zapcore.PanicLevel:
		return "ALERT"
	case zapcore.FatalLevel:
		return "EMERGENCY"
	default:
		return l.CapitalString()
	}
}

func encodeLevel(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(zapLevelToStackdriverSeverity(l))
}

// newEncoderConfig creates an instance of zapcore.EncoderConfig in accordance with Stackdriver's log format.
// Refer to Stackdriver's special fields in structured payloads
// https://cloud.google.com/logging/docs/agent/configuration#special-fields
// If, after stripping special-purpose fields, there is only a message field left, that message is saved as textPayload.
// Otherwise any remaining structured record fields become part of jsonPayload.
func newEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		TimeKey:        "timestamp", // Stackdriver's special field
		LevelKey:       "severity",  // Stackdriver's special field
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "message", // Stackdriver's special field
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    encodeLevel,
		EncodeTime:     zapcore.RFC3339TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
}
