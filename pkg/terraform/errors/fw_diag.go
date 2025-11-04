// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// FrameworkDiagnosticsError returns an error representing
// the collected diagnostics at SeverityError level.
func FrameworkDiagnosticsError(parentMessage string, diags diag.Diagnostics) error {
	eds := diags.Errors()
	if len(eds) == 0 {
		return nil
	}

	errs := make([]error, 0, len(eds)+1)
	errs = append(errs, errors.New(parentMessage))
	for _, d := range eds {
		errs = append(errs, errors.New(frameworkDiagnosticString(d)))
	}
	return errors.Join(errs...)
}

// frameworkDiagnosticString formats the given framework Diagnostic
// as a string <summary>[: <detail>][: <path>].
func frameworkDiagnosticString(d diag.Diagnostic) string {
	var msg strings.Builder
	msg.WriteString(strings.TrimSpace(d.Summary()))

	detail := strings.TrimSpace(d.Detail())
	if detail != "" {
		msg.WriteString(": ")
		msg.WriteString(detail)
	}

	if p, ok := d.(diag.DiagnosticWithPath); ok {
		wp := strings.TrimSpace(p.Path().String())
		if wp != "" {
			msg.WriteString(": ")
			msg.WriteString(wp)
		}
	}

	return msg.String()
}
