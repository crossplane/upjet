// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

func TestCanonicalize(t *testing.T) {
	tests := map[string]struct {
		inputFile    string
		expectedFile string
		err          error
	}{
		"SuccessfulObjectConversion": {
			inputFile:    "policy.json",
			expectedFile: "policy_canonical.json",
		},
		"SuccessfulArrayConversion": {
			inputFile:    "array.json",
			expectedFile: "array_canonical.json",
		},
		"NoopConversion": {
			inputFile:    "policy_canonical.json",
			expectedFile: "policy_canonical.json",
		},
		"InvalidJSON": {
			inputFile: "invalid.json",
			err:       errors.Wrap(errors.New(`ReadString: expects " or n, but found }, error found in #10 byte of ...|"a": "b",}|..., bigger context ...|{"a": "b",}|...`), `failed to unmarshal the JSON document: {"a": "b",}`),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join("testdata", tc.inputFile))
			if err != nil {
				t.Fatalf("Failed to read the input file: %v", err)
			}

			expectedOutput := ""
			if tc.expectedFile != "" {
				output, err := os.ReadFile(filepath.Join("testdata", tc.expectedFile))
				if err != nil {
					t.Fatalf("Failed to read expected the output file: %v", err)
				}
				expectedOutput = strings.Join(strings.Split(strings.TrimSpace(string(output)), "\n")[3:], "\n")
			}

			inputJSON := strings.Join(strings.Split(strings.TrimSpace(string(input)), "\n")[3:], "\n")
			canonicalJSON, err := Canonicalize(inputJSON)
			if err != nil {
				if diff := cmp.Diff(tc.err, err, test.EquateErrors()); diff != "" {
					t.Fatalf("Canonicalize(...): -wantErr, +gotErr: %s", diff)
				}
				return
			}
			if diff := cmp.Diff(expectedOutput, canonicalJSON); diff != "" {
				t.Errorf("Canonicalize(...): -want, +got: \n%s", diff)
			}
		})
	}
}
