<!--
SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>

SPDX-License-Identifier: CC-BY-4.0
-->

# API roundtrip testing

A testing utility library for verifying that Crossplane provider managed resources
correctly survive two kinds of round trips:

| Test | What it checks |
|---|---|
| **Serialization** | Every registered type can be JSON-encoded and decoded back to an identical object. |
| **Conversion** | Every multi-version type survives spoke→hub→spoke and hub→spoke→hub conversion with no data loss. |

The library builds on `k8s.io/apimachinery`'s fuzzing/round-trip infrastructure and
`sigs.k8s.io/randfill` to generate random objects, then exercises the conversion
functions registered by `pkg/controller/conversion.RegisterConversions`.

---

## How it works

### Serialization round-trip

`TestSerializationRoundtrip` delegates to the upstream
`roundtrip.RoundTripExternalTypesWithoutProtobuf` helper. For every type in the
scheme it:

1. Creates a randomly filled instance using the fuzzer.
2. Marshals it to JSON.
3. Unmarshals back to a new instance.
4. Asserts the two instances are equal.

### Conversion round-trip

`TestConversionRoundtrip` discovers every API group in the scheme, finds the hub
version and all spoke versions for each `GroupKind`, then runs two sub-tests per
(hub, spoke) pair:

- **spoke → hub → spoke**: fill a spoke, convert to hub, convert back to a new spoke, compare.
- **hub → spoke → hub**: fill a hub, convert to spoke, convert back to a new hub, compare.

Each sub-test repeats the cycle `defaultFuzzIterations` (5) times with fresh random data.
Sub-tests are run in parallel per group to keep the suite fast.

---

## Usage

### Minimal setup

```go
// e2e/roundtrip/roundtrip_test.go
package roundtrip

import (
    "testing"

    "github.com/crossplane/upjet/v2/pkg/apitesting/roundtrip"
    "k8s.io/apimachinery/pkg/runtime"

    clusterapis    "github.com/upbound/provider-foo/apis/cluster"
    namespacedapis "github.com/upbound/provider-foo/apis/namespaced"
    "github.com/upbound/provider-foo/config"
    "github.com/upbound/provider-foo/xpprovider"
)

func TestRoundTrip(t *testing.T) {
    schema, err := xpprovider.GetProviderSchema(t.Context())
    if err != nil {
        t.Fatalf("GetProviderSchema: %s", err)
    }

    provider, err := config.GetProvider(t.Context(), schema, false)
    if err != nil {
        t.Fatalf("GetProvider: %s", err)
    }

    providerNamespaced, err := config.GetNamespacedProvider(t.Context(), schema, false)
    if err != nil {
        t.Fatalf("GetNamespacedProvider: %s", err)
    }

    testScheme := runtime.NewScheme()
    if err := clusterapis.AddToScheme(testScheme); err != nil {
        t.Fatalf("cluster-scoped apis AddToScheme: %s", err)
    }
    if err := namespacedapis.AddToScheme(testScheme); err != nil {
        t.Fatalf("namespaced apis AddToScheme: %s", err)
    }

    rt, err := roundtrip.NewRoundTripTest(provider, providerNamespaced, testScheme)
    if err != nil {
        t.Fatalf("NewRoundTripTest: %s", err)
    }

    t.Run("TestSerializationRoundtrip", rt.TestSerializationRoundtrip)
    t.Run("TestConversionRoundtrip",    rt.TestConversionRoundtrip)
}
```

Pass `nil` for `providerNamespaced` if the provider only exposes cluster-scoped resources.

---

## Options

All options are passed to `NewRoundTripTest` as variadic `TestOption` arguments.

### Codec

| Option | Description |
|---|---|
| `WithCodecFactory(c)` | Override the codec factory derived from the scheme. |

### Filtering which kinds to test

| Option | Description |
|---|---|
| `WithIncludeGroups(groups...)` | Only test these API groups. |
| `WithIncludeGroupKinds(gks...)` | Only test these GroupKinds. |
| `WithExcludeGroups(groups...)` | Skip these API groups. |
| `WithExcludeGroupKinds(gks...)` | Skip these GroupKinds. |

When no include filter is set, all groups registered in the scheme are tested
(minus `defaultIgnoredKinds` and the empty/core group).

### Fuzzer configuration

`WithFuzzerConfig(opts ...FuzzerOption)` adds a fuzzer configuration.  Each
registered configuration is run in sequence for every (kind, version-pair),
accumulating coverage across different fuzz parameters.  When no
`WithFuzzerConfig` call is made a single default configuration is used
(NilChance≈0.2, NumElements 0–3, 10 iterations).

Multiple calls each add a **distinct** configuration:

```go
rt, _ := roundtrip.NewRoundTripTest(provider, nil,
    roundtrip.WithScheme(testScheme),
    // First pass: no nil pointers, 20 iterations
    roundtrip.WithFuzzerConfig(
        roundtrip.FuzzerNilChance(0),
        roundtrip.FuzzerIterations(20),
    ),
    // Second pass: sparse data, 5 iterations
    roundtrip.WithFuzzerConfig(
        roundtrip.FuzzerNilChance(0.8),
        roundtrip.FuzzerNumElements(0, 1),
        roundtrip.FuzzerIterations(5),
    ),
)
```

Available `FuzzerOption` constructors:

| Constructor | Description |
|---|---|
| `FuzzerIterations(n)` | Number of fuzz-fill + round-trip cycles for this config. Default: 10. |
| `FuzzerNilChance(p)` | Probability [0,1] that pointer fields are left nil. Default: ~0.2. |
| `FuzzerNumElements(min, max)` | Min/max elements for maps and slices. Default: 0–3. |
| `FuzzerMaxDepth(d)` | Maximum recursion depth for nested structs. |
| `FuzzerRandSource(src)` | Deterministic random source (e.g. `rand.NewSource(42)`). |
| `FuzzerSkipPatterns(patterns...)` | Skip fields whose names match any regexp. |
| `FuzzerAllowUnexportedFields(bool)` | Whether to fill unexported struct fields. |

`WithExtraFuzzFuncs(fns...)` adds `func(*T, randfill.Continue)` functions that
are applied globally to **every** fuzzer configuration.

### Comparison options

| Option | Description |
|---|---|
| `WithComparisonOptions(opts...)` | Append `cmp.Option` values used when comparing objects after round trip. |

---

## Exported fuzzer helpers

These can be passed to `WithExtraFuzzFuncs`:

| Helper | Description |
|---|---|
| `ASCIIStringFuzzer` | Fills strings with random lowercase-alphanumeric characters (included by default). |


Example:

```go
rt, err := roundtrip.NewRoundTripTest(provider, nil,
    roundtrip.WithScheme(testScheme),
    roundtrip.WithExtraFuzzFuncs(
        roundtrip.NoNilElementsInPointerSlices(codecFactory)...),
    roundtrip.WithComparisonOptions(roundtrip.EquateNilPtr()),
)
```

---