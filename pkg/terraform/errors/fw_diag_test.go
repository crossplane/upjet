// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"testing"

	xperrors "github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/google/go-cmp/cmp"
	fwdiag "github.com/hashicorp/terraform-plugin-framework/diag"
	fwpath "github.com/hashicorp/terraform-plugin-framework/path"
)

// fakeDiag is a minimal implementation of the framework diag.Diagnostic
// interface sufficient for testing frameworkDiagnosticString.
type fakeDiag struct {
	summary string
	detail  string
}

func (f fakeDiag) Severity() fwdiag.Severity { return fwdiag.SeverityError }
func (f fakeDiag) Summary() string           { return f.summary }
func (f fakeDiag) Detail() string            { return f.detail }
func (f fakeDiag) Equal(other fwdiag.Diagnostic) bool {
	o, ok := other.(fakeDiag)
	if !ok {
		return false
	}
	return f.summary == o.summary && f.detail == o.detail
}

// fakeWarnDiag implements a warning diagnostic.
type fakeWarnDiag struct {
	summary string
	detail  string
}

func (f fakeWarnDiag) Severity() fwdiag.Severity { return fwdiag.SeverityWarning }
func (f fakeWarnDiag) Summary() string           { return f.summary }
func (f fakeWarnDiag) Detail() string            { return f.detail }
func (f fakeWarnDiag) Equal(other fwdiag.Diagnostic) bool {
	o, ok := other.(fakeWarnDiag)
	if !ok {
		return false
	}
	return f.summary == o.summary && f.detail == o.detail
}

// fakeDiagWithPath implements DiagnosticWithPath.
type fakeDiagWithPath struct {
	summary string
	detail  string
	p       fwpath.Path
}

func (f fakeDiagWithPath) Severity() fwdiag.Severity { return fwdiag.SeverityError }
func (f fakeDiagWithPath) Summary() string           { return f.summary }
func (f fakeDiagWithPath) Detail() string            { return f.detail }
func (f fakeDiagWithPath) Path() fwpath.Path         { return f.p }
func (f fakeDiagWithPath) Equal(other fwdiag.Diagnostic) bool {
	o, ok := other.(fakeDiagWithPath)
	if !ok {
		return false
	}
	return f.summary == o.summary && f.detail == o.detail && f.p.String() == o.p.String()
}

func TestFrameworkDiagnosticString(t *testing.T) {
	type args struct {
		d fwdiag.Diagnostic
	}
	type want struct {
		out string
	}
	cases := map[string]struct {
		args
		want
	}{
		"SummaryOnly": {
			args: args{d: fakeDiag{summary: "read resource failed"}},
			want: want{out: "read resource failed"},
		},
		"SummaryAndDetail": {
			args: args{d: fakeDiag{summary: "apply failed", detail: "invalid input provided"}},
			want: want{out: "apply failed: invalid input provided"},
		},
		"TrimsSpaces": {
			args: args{d: fakeDiag{summary: "  parse  ", detail: "  bad format  "}},
			want: want{out: "parse: bad format"},
		},
		"MultilineDetailPreserved": {
			args: args{d: fakeDiag{summary: "plan failed", detail: "line1\nline2"}},
			want: want{out: "plan failed: line1\nline2"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := frameworkDiagnosticString(tc.args.d)
			if diff := cmp.Diff(tc.want.out, got); diff != "" {
				t.Errorf("\n%s\nframeworkDiagnosticString(...): -want out, +got out:\n%s", name, diff)
			}
		})
	}
}

func TestFrameworkDiagnosticsError(t *testing.T) {
	type args struct {
		ds fwdiag.Diagnostics
	}
	type want struct {
		err error
	}
	// Build a path for the path-bearing diag
	p := fwpath.Root("root").AtName("field")

	dErr1 := fakeDiag{summary: "op failed", detail: "reason one"}
	dErr2 := fakeDiagWithPath{summary: "apply failed", detail: "invalid value", p: p}
	dWarn := fakeWarnDiag{summary: "just a warning", detail: "ignore me"}

	cases := map[string]struct {
		args
		want
	}{
		"NoDiagnostics": {},
		"OnlyWarnings": {
			args: args{ds: fwdiag.Diagnostics{dWarn}},
			want: want{err: nil},
		},
		"SingleError": {
			args: args{ds: fwdiag.Diagnostics{dErr1}},
			want: want{err: xperrors.Join(
				xperrors.New("terraform diagnostic errors"),
				xperrors.New(frameworkDiagnosticString(dErr1)),
			)},
		},
		"MultipleErrorsWithPath": {
			args: args{ds: fwdiag.Diagnostics{dErr1, dErr2}},
			want: want{err: xperrors.Join(
				xperrors.New("terraform diagnostic errors"),
				xperrors.New(frameworkDiagnosticString(dErr1)),
				xperrors.New(frameworkDiagnosticString(dErr2)),
			)},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := FrameworkDiagnosticsError("terraform diagnostic errors", tc.args.ds)
			if diff := cmp.Diff(tc.want.err, got, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nFrameworkDiagnosticsError(...): -want err, +got err:\n%s", name, diff)
			}
		})
	}
}
