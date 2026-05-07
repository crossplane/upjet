// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0
package roundtrip

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/randfill"
)

func TestASCIIStringFuzzer(t *testing.T) {
	f := randfill.New().Funcs(ASCIIStringFuzzer)
	for range 200 {
		var s string
		f.Fill(&s)
		if len(s) > 29 {
			t.Fatalf("string length %d exceeds maximum of 29: %q", len(s), s)
		}
		for _, ch := range s {
			if !strings.ContainsRune(dnsLetters, ch) {
				t.Fatalf("unexpected character %q in output %q (not in dnsLetters)", ch, s)
			}
		}
	}
}

func TestObjectMetaNamespaceFuzzer(t *testing.T) {
	t.Run("cluster-scoped always produces empty Namespace", func(t *testing.T) {
		f := randfill.New().Funcs(objectMetaNamespaceFuzzer(false))
		for range 50 {
			var meta metav1.ObjectMeta
			f.Fill(&meta)
			if meta.Namespace != "" {
				t.Fatalf("cluster-scoped fuzzer produced non-empty Namespace: %q", meta.Namespace)
			}
		}
	})

	t.Run("namespaced always produces non-empty Namespace", func(t *testing.T) {
		f := randfill.New().Funcs(objectMetaNamespaceFuzzer(true)).NilChance(0)
		for range 100 {
			var meta metav1.ObjectMeta
			f.Fill(&meta)
			if meta.Namespace == "" {
				t.Fatal("namespaced fuzzer produced an empty Namespace")
			}
		}
	})
}
