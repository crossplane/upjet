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

// clusterScopedFuzzer is a randfill-compatible fuzzer function for
// metav1.ObjectMeta that forces Namespace to empty string, ensuring generated
// objects are valid for cluster-scoped resources.
func clusterScopedFuzzer(meta *metav1.ObjectMeta, c randfill.Continue) {
	c.FillNoCustom(meta)
	meta.Namespace = ""
}

// namespacedFuzzer is a randfill-compatible fuzzer function for
// metav1.ObjectMeta that fills Namespace with a random string, ensuring
// generated objects are valid for namespace-scoped resources.
func namespacedFuzzer(meta *metav1.ObjectMeta, c randfill.Continue) {
	c.FillNoCustom(meta)
	meta.Namespace = c.String(16)
}
