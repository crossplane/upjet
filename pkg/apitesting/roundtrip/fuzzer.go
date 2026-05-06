// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0
package roundtrip

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/randfill"
)

// dnsLetters is the character set used by ASCIIStringFuzzer.  Restricting to
// lowercase alphanumerics and hyphens keeps generated strings valid for use as
// Kubernetes names, label values, and annotation keys.
const dnsLetters = "abcdefghijklmnopqrstuvwxyz0123456789-"

// ASCIIStringFuzzer is a randfill-compatible fuzzer function that fills string
// fields with random lowercase-alphanumeric strings of up to 29 characters.
// Register it via WithExtraFuzzFuncs or rely on the default that NewRoundTripTest
// includes it automatically.
//
// Restricting the character set avoids encoding issues and makes test output
// readable when a diff is printed.
func ASCIIStringFuzzer(s *string, c randfill.Continue) {
	n := c.Rand.Intn(30)
	b := make([]byte, n)
	for i := range n {
		b[i] = dnsLetters[c.Rand.Intn(len(dnsLetters))]
	}
	*s = string(b)
}

// objectMetaNamespaceFuzzer returns a randfill-compatible fuzzer function for
// metav1.ObjectMeta. When namespaced is true the Namespace field is set to a
// non-empty random string (prefixed with "ns" because c.String(n) picks a
// length in [0, n) and can produce ""); when false Namespace is cleared to the
// empty string.
func objectMetaNamespaceFuzzer(namespaced bool) func(*metav1.ObjectMeta, randfill.Continue) {
	return func(meta *metav1.ObjectMeta, c randfill.Continue) {
		c.FillNoCustom(meta)
		if namespaced {
			meta.Namespace = "ns" + c.String(15)
		} else {
			meta.Namespace = ""
		}
	}
}
