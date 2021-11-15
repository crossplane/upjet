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

package errors

import (
	"fmt"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
)

const (
	levelError = "error"
)

type tfError struct {
	logs    []byte
	tfLogs  []*TerraformLog
	cause   error
	message string
}

type applyFailed struct {
	*tfError
}

// TerraformLog represents relevant fields of a Terraform CLI JSON-formatted log line
type TerraformLog struct {
	Level      string        `json:"@level"`
	Message    string        `json:"@message"`
	Diagnostic LogDiagnostic `json:"diagnostic"`
}

// LogDiagnostic represents relevant fields of a Terraform CLI JSON-formatted
// log line diagnostic info
type LogDiagnostic struct {
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail"`
}

func (t *tfError) Error() string {
	if t == nil {
		return ""
	}

	messages := make([]string, 0, len(t.tfLogs))
	for _, l := range t.tfLogs {
		// only use error logs
		if l == nil || l.Level != levelError {
			continue
		}
		m := l.Message
		if l.Diagnostic.Severity == levelError && l.Diagnostic.Summary != "" {
			m = fmt.Sprintf("%s: %s", l.Diagnostic.Summary, l.Diagnostic.Detail)
		}
		messages = append(messages, m)
	}
	return fmt.Sprintf("%s: %s", t.message, strings.Join(messages, "\n"))
}

func (t *tfError) Unwrap() error {
	if t == nil {
		return nil
	}
	return t.cause
}

func newTFError(message string, cause error, logs []byte) (string, *tfError) {
	tfError := &tfError{
		logs:    logs,
		cause:   cause,
		message: message,
	}

	tfLogs, err := parseTerraformLogs(logs)
	if err != nil {
		return err.Error(), tfError
	}
	tfError.tfLogs = tfLogs
	return "", tfError
}

func parseTerraformLogs(logs []byte) ([]*TerraformLog, error) {
	logLines := strings.Split(string(logs), "\n")
	tfLogs := make([]*TerraformLog, 0, len(logLines))
	for _, l := range logLines {
		log := &TerraformLog{}
		l := strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if err := jsoniter.ConfigCompatibleWithStandardLibrary.UnmarshalFromString(l, log); err != nil {
			return nil, err
		}
		tfLogs = append(tfLogs, log)
	}
	return tfLogs, nil
}

// WrapTFError returns a new Terraform CLI failure error with given logs.
func WrapTFError(message string, cause error, logs []byte) error {
	if cause == nil {
		return nil
	}

	parseError, tfError := newTFError(message, cause, logs)
	if parseError == "" {
		return tfError
	}
	return errors.WithMessage(tfError, parseError)
}

// WrapApplyFailed returns a new apply failure error with given logs.
func WrapApplyFailed(cause error, logs []byte) error {
	if cause == nil {
		return nil
	}

	parseError, tfError := newTFError("apply failed", cause, logs)
	result := &applyFailed{tfError: tfError}
	if parseError == "" {
		return result
	}
	return errors.WithMessage(result, parseError)
}

// IsApplyFailed returns whether error is due to failure of an apply operation.
func IsApplyFailed(err error) bool {
	r := &applyFailed{}
	return errors.As(err, &r)
}

type destroyFailed struct {
	*tfError
}

// WrapDestroyFailed returns a new destroy failure error with given logs.
func WrapDestroyFailed(cause error, logs []byte) error {
	if cause == nil {
		return nil
	}

	parseError, tfError := newTFError("destroy failed", cause, logs)
	result := &destroyFailed{tfError: tfError}
	if parseError == "" {
		return result
	}
	return errors.WithMessage(result, parseError)
}

// IsDestroyFailed returns whether error is due to failure of a destroy operation.
func IsDestroyFailed(err error) bool {
	r := &destroyFailed{}
	return errors.As(err, &r)
}
