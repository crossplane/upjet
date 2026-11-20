// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package terraform

import (
	"encoding/json"
	"strings"
	"testing"
)

const testPEM = "-----BEGIN PRIVATE KEY-----\nMIIBOgIBAAJBAKj34G\nhx8nQ3rvz2Sudba8\n-----END PRIVATE KEY-----\n"

// mainTFJSON marshals the value the way WriteMainTF does, so the escaped form
// under test is the one that really lands in main.tf.json.
func mainTFJSON(t *testing.T, secret string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"provider": map[string]any{"okta": map[string]any{"private_key": secret}},
	})
	if err != nil {
		t.Fatalf("cannot marshal main.tf.json: %v", err)
	}
	return string(b)
}

func TestFilterSensitiveInformationMultiline(t *testing.T) {
	ts := Setup{Configuration: map[string]any{"private_key": testPEM}}

	raw := mainTFJSON(t, testPEM)
	if strings.Contains(raw, testPEM) {
		t.Fatal("precondition failed: main.tf.json was expected to escape the newlines")
	}

	// terraform init prints human readable diagnostics that quote the file.
	if got := ts.filterSensitiveInformation(raw); strings.Contains(got, "MIIBOgIBAAJBAKj34G") {
		t.Errorf("single encoded secret survived the filter: %s", got)
	}

	// apply, plan and destroy are run with -json, so the quoted file is
	// encoded a second time.
	doubled, err := json.Marshal(map[string]any{"@level": "error", "snippet": map[string]any{"code": raw}})
	if err != nil {
		t.Fatalf("cannot marshal diagnostic: %v", err)
	}
	if got := ts.filterSensitiveInformation(string(doubled)); strings.Contains(got, "MIIBOgIBAAJBAKj34G") {
		t.Errorf("double encoded secret survived the filter: %s", got)
	}
}

func TestFilterSensitiveInformationSingleLine(t *testing.T) {
	ts := Setup{Configuration: map[string]any{"token": "s3cr3t-token", "empty": "", "n": 42}}

	got := ts.filterSensitiveInformation("using token s3cr3t-token now")
	if strings.Contains(got, "s3cr3t-token") {
		t.Errorf("single line secret survived the filter: %s", got)
	}
	if got != "using token REDACTED now" {
		t.Errorf("unexpected result: %q", got)
	}

	// An empty value must not redact everything.
	if got := ts.filterSensitiveInformation("abc"); got != "abc" {
		t.Errorf("empty configuration value altered the output: %q", got)
	}
}
