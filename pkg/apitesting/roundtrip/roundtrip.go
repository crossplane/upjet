// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

// Package roundtrip provides a testing utility library for verifying that
// Crossplane provider managed resources correctly survive serialization and
// version-conversion round trips.
//
// The library exercises two kinds of invariants:
//
//  1. Serialization round-trip: an object fuzzed with random data can be
//     encoded to JSON/YAML and decoded back to an identical object.
//
//  2. Conversion round-trip: an object converted spoke→hub→spoke (or
//     hub→spoke→hub) is bit-identical to the original, proving that no
//     data is lost across API version conversions registered via
//     pkg/controller/conversion.RegisterConversions.
//
// Basic usage:
//
//	func TestRoundTrip(t *testing.T) {
//	    provider, _ := config.GetProvider(t.Context(), schema, false)
//	    providerNamespaced, _ := config.GetNamespacedProvider(t.Context(), schema, false)
//
//	    testScheme := runtime.NewScheme()
//	    clusterapis.AddToScheme(testScheme)
//	    namespacedapis.AddToScheme(testScheme)
//
//	    rt, _ := roundtrip.NewRoundTripTest(provider, providerNamespaced, testScheme)
//
//	    t.Run("Serialization", rt.TestSerializationRoundtrip)
//	    t.Run("Conversion",    rt.TestConversionRoundtrip)
//	}
package roundtrip

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	"k8s.io/apimachinery/pkg/api/apitesting/roundtrip"
	genericfuzzer "k8s.io/apimachinery/pkg/apis/meta/fuzzer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	"sigs.k8s.io/randfill"

	"github.com/crossplane/upjet/v2/pkg/config"
	ujconversion "github.com/crossplane/upjet/v2/pkg/controller/conversion"
)

const (
	// defaultFuzzIterations is the number of fuzz-fill + round-trip cycles run
	// per (kind, version-pair) when no FuzzerIterations option is set.
	defaultFuzzIterations = 5
	// defaultFuzzMinElements is the lower bound on the number of elements
	// generated for maps and slices during fuzzing.
	defaultFuzzMinElements = 0
	// defaultFuzzMaxElements is the upper bound on the number of elements
	// generated for maps and slices during fuzzing.
	defaultFuzzMaxElements = 3
)

// defaultIgnoredKinds lists Kubernetes meta-API kinds that carry no provider
// types and should be excluded from both serialization and conversion tests.
var defaultIgnoredKinds = sets.New(
	"ListOptions", "CreateOptions", "GetOptions",
	"UpdateOptions", "PatchOptions", "DeleteOptions", "WatchEvent")

var (
	// cmpoptIgnoreFieldConversionAnnotation drops the upjet internal annotation
	// that records per-field conversion metadata from object comparisons. The
	// annotation is written during ConvertTo/ConvertFrom and its exact value is
	// not part of the user-visible API contract.
	cmpoptIgnoreFieldConversionAnnotation = cmpopts.IgnoreMapEntries(func(k, v string) bool {
		return k == "internal.upjet.crossplane.io/field-conversions"
	})

	// defaultComparisonOptions are the cmp.Options applied to every object
	// comparison performed by the test suite unless overridden via
	// WithComparisonOptions.
	defaultComparisonOptions = []cmp.Option{
		cmpoptIgnoreFieldConversionAnnotation,
		cmpopts.EquateEmpty(),
	}
)

// RoundTripTest holds all state needed to run serialization and conversion
// round-trip tests for a single provider.  Create it once with
// NewRoundTripTest and reuse it across sub-tests.
type RoundTripTest struct {
	// providerCluster is the cluster-scoped provider configuration used to
	// register conversion functions.
	providerCluster *config.Provider
	// providerNamespaced is the namespaced provider configuration. May be nil
	// for providers that only expose cluster-scoped resources.
	providerNamespaced *config.Provider
	// scheme holds all types registered by the caller and is used both for
	// fuzzing (codec factory) and for iterating over known GVKs.
	scheme *runtime.Scheme
	// codecFactory is derived from scheme and handed to the k8s fuzzing
	// infrastructure.
	codecFactory serializer.CodecFactory

	// fuzzerConfigs is the list of fuzzer configurations to run per
	// (kind, version-pair).  Each configuration produces an independent
	// randfill.Filler and runs its own iteration count.  At least one entry is
	// always present after NewRoundTripTest; callers add more with
	// WithFuzzerConfig.
	fuzzerConfigs []fuzzerOptions
	// extraFuzzFns are additional randfill-compatible fuzzer functions appended
	// to every fuzzer built from fuzzerConfigs.  Add them with WithExtraFuzzFuncs.
	extraFuzzFns []any

	// cmpOpts are the cmp.Options used when comparing objects after a round
	// trip.  Defaults to defaultComparisonOptions; extend with
	// WithComparisonOptions.
	cmpOpts []cmp.Option

	// includeGroups, when non-empty, restricts conversion tests to the listed
	// API groups. Populated by WithIncludeGroups.
	includeGroups sets.Set[string]
	// includeGroupKinds, when non-empty, restricts conversion tests to the
	// listed GroupKinds. Populated by WithIncludeGroupKinds.
	includeGroupKinds sets.Set[schema.GroupKind]
	// excludeGroups lists API groups to exclude from conversion tests.  Populated
	// by WithExcludeGroups.
	excludeGroups sets.Set[string]
	// excludeGroupKinds lists specific GroupKinds to exclude from conversion tests.
	// Populated by WithExcludeGroupKinds.
	excludeGroupKinds sets.Set[schema.GroupKind]
}

// fuzzerOptions collects optional configuration for a single randfill.Filler.
// All fields are pointers so that zero values can be distinguished from "not
// set", allowing the defaults to apply when a field is absent.
type fuzzerOptions struct {
	// FuzzIterations is the number of fuzz-fill + round-trip cycles run for
	// each (kind, version-pair) by this fuzzer.  Defaults to
	// defaultFuzzIterations when nil.
	FuzzIterations *int
	// NilChance overrides the probability that a pointer field is left nil.
	// Must be in [0, 1].
	NilChance *float64
	// MinElements overrides the minimum number of elements generated for maps
	// and slices.
	MinElements *int
	// MaxElements overrides the maximum number of elements generated for maps
	// and slices.
	MaxElements *int
	// MaxDepth overrides the maximum recursion depth for nested structs.
	MaxDepth *int
	// RandSource supplies a custom random source for deterministic fuzzing.
	// When nil a random seed is chosen automatically.
	RandSource rand.Source
	// AllowUnexportedFields, when true, allows the fuzzer to fill unexported
	// struct fields.
	AllowUnexportedFields *bool
	// SkipPatterns lists regexp patterns; fields whose names match any pattern
	// are skipped by the fuzzer.
	SkipPatterns []*regexp.Regexp
}

// fillerWithIterations pairs a ready-to-use randfill.Filler with the number
// of round-trip cycles it should drive.
type fillerWithIterations struct {
	filler     *randfill.Filler
	iterations int
}

// FuzzerOption configures a single fuzzerOptions entry.  Pass one or more to
// WithFuzzerConfig to register a new fuzzer configuration.
type FuzzerOption func(*fuzzerOptions)

// FuzzerIterations sets the number of fuzz-fill + round-trip cycles this
// fuzzer configuration will run per (kind, version-pair).
func FuzzerIterations(n int) FuzzerOption {
	return func(o *fuzzerOptions) { o.FuzzIterations = &n }
}

// FuzzerNilChance sets the probability [0, 1] that pointer fields are left
// nil.  Use 0 to force all pointers to be non-nil.
func FuzzerNilChance(p float64) FuzzerOption {
	return func(o *fuzzerOptions) { o.NilChance = &p }
}

// FuzzerNumElements sets the min and max number of elements generated for maps
// and slices.
func FuzzerNumElements(min, max int) FuzzerOption {
	return func(o *fuzzerOptions) {
		o.MinElements = &min
		o.MaxElements = &max
	}
}

// FuzzerMaxDepth sets the maximum recursion depth the fuzzer will descend into
// nested structs.
func FuzzerMaxDepth(d int) FuzzerOption {
	return func(o *fuzzerOptions) { o.MaxDepth = &d }
}

// FuzzerRandSource sets a deterministic random source for this fuzzer
// configuration.
func FuzzerRandSource(src rand.Source) FuzzerOption {
	return func(o *fuzzerOptions) { o.RandSource = src }
}

// FuzzerSkipPatterns registers regexp patterns; any struct field whose name
// matches a pattern will be skipped by this fuzzer.
func FuzzerSkipPatterns(patterns ...*regexp.Regexp) FuzzerOption {
	return func(o *fuzzerOptions) {
		o.SkipPatterns = append(o.SkipPatterns, patterns...)
	}
}

// FuzzerAllowUnexportedFields allows this fuzzer to fill unexported struct
// fields.
func FuzzerAllowUnexportedFields(allow bool) FuzzerOption {
	return func(o *fuzzerOptions) { o.AllowUnexportedFields = &allow }
}

// TestOption is a functional option that customises a RoundTripTest.
type TestOption func(*RoundTripTest)

// WithCodecFactory overrides the codec factory derived from the scheme.  Use
// this when you need to control codec negotiation (e.g. to add custom
// serializers).
func WithCodecFactory(c serializer.CodecFactory) TestOption {
	return func(rtt *RoundTripTest) {
		rtt.codecFactory = c
	}
}

// WithIncludeGroups restricts the conversion round-trip test to the given API
// groups. When neither WithIncludeGroups nor WithIncludeGroupKinds is set,
// all groups registered in the scheme are tested.
func WithIncludeGroups(groups ...string) TestOption {
	return func(rtt *RoundTripTest) {
		rtt.includeGroups.Insert(groups...)
	}
}

// WithIncludeGroupKinds restricts the conversion round-trip test to the given
// GroupKinds.  When neither WithIncludeGroups nor WithIncludeGroupKinds is
// set, all kinds registered in the scheme are tested.
func WithIncludeGroupKinds(groupKinds ...schema.GroupKind) TestOption {
	return func(rtt *RoundTripTest) {
		rtt.includeGroupKinds.Insert(groupKinds...)
	}
}

// WithExcludeGroups excludes the given API groups from the conversion
// round-trip test.
func WithExcludeGroups(groups ...string) TestOption {
	return func(rtt *RoundTripTest) {
		rtt.excludeGroups.Insert(groups...)
	}
}

// WithExcludeGroupKinds excludes the given GroupKinds from the conversion
// round-trip test.
func WithExcludeGroupKinds(groupKinds ...schema.GroupKind) TestOption {
	return func(rtt *RoundTripTest) {
		rtt.excludeGroupKinds.Insert(groupKinds...)
	}
}

// WithComparisonOptions appends additional cmp.Options to those used when
// comparing objects after a round trip.
func WithComparisonOptions(cmpOpts ...cmp.Option) TestOption {
	return func(rtt *RoundTripTest) {
		rtt.cmpOpts = append(rtt.cmpOpts, cmpOpts...)
	}
}

// WithExtraFuzzFuncs appends additional randfill-compatible fuzzer functions
// (signature: func(*T, randfill.Continue)) to every fuzzer built by the test
// suite.  This is useful to restrict a field to a valid value domain (e.g.
// only valid enum strings).
func WithExtraFuzzFuncs(fns ...any) TestOption {
	return func(rtt *RoundTripTest) {
		rtt.extraFuzzFns = append(rtt.extraFuzzFns, fns...)
	}
}

// WithFuzzerConfig adds a new fuzzer configuration to the test suite.  The
// conversion tests run every registered configuration in sequence for each
// (kind, version-pair), accumulating coverage across different fuzz
// parameters.
//
// Multiple calls each add a distinct configuration:
//
//	rt, _ := roundtrip.NewRoundTripTest(provider, nil,
//	    roundtrip.WithFuzzerConfig(
//	        roundtrip.FuzzerNilChance(0),
//	        roundtrip.FuzzerIterations(20),
//	    ),
//	    roundtrip.WithFuzzerConfig(
//	        roundtrip.FuzzerNilChance(0.5),
//	        roundtrip.FuzzerNumElements(0, 5),
//	    ),
//	)
//
// When no WithFuzzerConfig is provided, a single default configuration is used
// (NilChance≈0.2, NumElements 0–3, 10 iterations).
func WithFuzzerConfig(opts ...FuzzerOption) TestOption {
	return func(rtt *RoundTripTest) {
		cfg := fuzzerOptions{}
		for _, o := range opts {
			o(&cfg)
		}
		rtt.fuzzerConfigs = append(rtt.fuzzerConfigs, cfg)
	}
}

// NewRoundTripTest constructs a RoundTripTest and performs one-time setup
// (scheme codec factory, conversion registration).
//
// provider is the cluster-scoped provider configuration.  providerNamespaced
// is the namespaced variant; pass nil if the provider only exposes
// cluster-scoped resources.  scheme must have all provider types registered
// before being passed here.  opts customise test behaviour.
func NewRoundTripTest(provider *config.Provider, providerNamespaced *config.Provider, scheme *runtime.Scheme, opts ...TestOption) (*RoundTripTest, error) {
	rt := &RoundTripTest{
		scheme:             scheme,
		providerCluster:    provider,
		providerNamespaced: providerNamespaced,
		includeGroups:      sets.New[string](),
		includeGroupKinds:  sets.New[schema.GroupKind](),
		excludeGroups:      sets.New[string](),
		excludeGroupKinds:  sets.New[schema.GroupKind](),
	}

	rt.cmpOpts = append(rt.cmpOpts, defaultComparisonOptions...)
	for _, opt := range opts {
		opt(rt)
	}

	// Guarantee at least one fuzzer configuration so callers never have to
	// call WithFuzzerConfig explicitly for the common case.
	if len(rt.fuzzerConfigs) == 0 {
		rt.fuzzerConfigs = []fuzzerOptions{{}}
	}

	err := rt.setupTestInfrastructure()
	if err != nil {
		return nil, errors.Wrap(err, "failed to setup test infrastructure")
	}
	return rt, nil
}

// setupTestInfrastructure performs expensive one-time setup shared across all
// sub-tests: it rebuilds the codec factory from the (potentially caller-supplied)
// scheme and registers hub↔spoke conversions for both cluster-scoped and
// namespaced providers.
func (rt *RoundTripTest) setupTestInfrastructure() error {
	rt.codecFactory = serializer.NewCodecFactory(rt.scheme)
	if err := ujconversion.RegisterConversions(rt.providerCluster, rt.providerNamespaced, rt.scheme); err != nil {
		return fmt.Errorf("failed to register conversions: %w", err)
	}
	return nil
}

// cmpK8sObjects compares two Kubernetes runtime.Objects for equality after
// stripping volatile metadata fields (resourceVersion, uid, etc.) that are
// not part of the provider API contract.  The test is failed immediately if
// a diff is found.
func (rt *RoundTripTest) cmpK8sObjects(t *testing.T, a, b runtime.Object) {
	t.Helper()

	if ao, ok := a.(metav1.Object); ok {
		normalizeMeta(ao)
	}
	if bo, ok := b.(metav1.Object); ok {
		normalizeMeta(bo)
	}

	if diff := cmp.Diff(a, b, rt.cmpOpts...); diff != "" {
		t.Fatalf("round-trip diff (-want +got):\n%s", diff)
	}
}

// getFuzzer constructs a randfill.Filler from opts.  A fresh random source is
// chosen automatically when opts.RandSource is nil, so parallel sub-tests each
// get an independent sequence and avoid data races.
func (rt *RoundTripTest) getFuzzer(opts fuzzerOptions) *randfill.Filler {
	randSource := opts.RandSource
	if randSource == nil {
		randSource = rand.NewSource(rand.Int63()) //nolint:gosec // for testing only
	}

	fuzzerFns := []fuzzer.FuzzerFuncs{genericfuzzer.Funcs, rt.customFuzzFuncs}
	objFuzzer := fuzzer.FuzzerFor(
		fuzzer.MergeFuzzerFuncs(fuzzerFns...),
		randSource,
		rt.codecFactory,
	)

	if opts.NilChance != nil {
		objFuzzer = objFuzzer.NilChance(*opts.NilChance)
	}

	minElements := defaultFuzzMinElements
	maxElements := defaultFuzzMaxElements
	if opts.MinElements != nil {
		minElements = *opts.MinElements
	}
	if opts.MaxElements != nil {
		maxElements = *opts.MaxElements
	}
	objFuzzer = objFuzzer.NumElements(minElements, maxElements)

	if opts.MaxDepth != nil {
		objFuzzer = objFuzzer.MaxDepth(*opts.MaxDepth)
	}

	if opts.AllowUnexportedFields != nil {
		objFuzzer = objFuzzer.AllowUnexportedFields(*opts.AllowUnexportedFields)
	}

	for _, pattern := range opts.SkipPatterns {
		objFuzzer = objFuzzer.SkipFieldsWithPattern(pattern)
	}

	return objFuzzer
}

// getFillers builds one fillerWithIterations per entry in rt.fuzzerConfigs.
// Each filler has NumElements(0,1) and NilChance(0) applied on top of its
// config — a temporary workaround for singleton-list and pointer-slice
// handling in upjet conversions — and the appropriate scope function
// (namespaced or cluster-scoped) for ObjectMeta.
//
// Callers must not share the returned slice across goroutines: randfill.Filler
// is not goroutine-safe.  Call getFillers once per kind sub-test.
func (rt *RoundTripTest) getFillers(namespaced bool) []fillerWithIterations {
	fillers := make([]fillerWithIterations, 0, len(rt.fuzzerConfigs))
	for _, cfg := range rt.fuzzerConfigs {
		f := rt.getFuzzer(cfg).NumElements(0, 1).NilChance(0)
		if namespaced {
			f = f.Funcs(namespacedFuzzer)
		} else {
			f = f.Funcs(clusterScopedFuzzer)
		}
		iters := defaultFuzzIterations
		if cfg.FuzzIterations != nil {
			iters = *cfg.FuzzIterations
		}
		fillers = append(fillers, fillerWithIterations{filler: f, iterations: iters})
	}
	return fillers
}

// TestSerializationRoundtrip verifies that every type registered in the scheme
// and passing the include/exclude filters survives a JSON encode→decode cycle
// with no data loss.  It delegates to the upstream k8s roundtrip helper.
// The same WithIncludeGroups/WithExcludeGroups/WithIncludeGroupKinds/
// WithExcludeGroupKinds filters that apply to TestConversionRoundtrip also
// apply here.  The first fuzzer configuration is used to build the fuzzer.
func (rt *RoundTripTest) TestSerializationRoundtrip(t *testing.T) {
	objFuzzer := rt.getFuzzer(rt.fuzzerConfigs[0])
	roundtrip.RoundTripExternalTypesWithoutProtobuf(t, rt.scheme, rt.codecFactory, objFuzzer, rt.nonRoundTrippableTypes())
}

// nonRoundTrippableTypes returns a map of GVKs that should be skipped by the
// serialization round-trip test.  A GVK is skipped when its GroupKind is not
// present in the set produced by groupsToKindFromScheme — i.e. when it is
// excluded via WithExcludeGroups/WithExcludeGroupKinds or absent from the
// include set set by WithIncludeGroups/WithIncludeGroupKinds.
func (rt *RoundTripTest) nonRoundTrippableTypes() map[schema.GroupVersionKind]bool {
	hasFilter := rt.includeGroups.Len() > 0 || rt.includeGroupKinds.Len() > 0 ||
		rt.excludeGroups.Len() > 0 || rt.excludeGroupKinds.Len() > 0
	if !hasFilter {
		return nil
	}

	allowed := rt.groupsToKindFromScheme()
	result := make(map[schema.GroupVersionKind]bool)
	for gvk := range rt.scheme.AllKnownTypes() {
		if gvk.Version == runtime.APIVersionInternal {
			continue
		}
		groupKinds, ok := allowed[gvk.Group]
		if !ok || !groupKinds.Has(gvk.GroupKind()) {
			result[gvk] = true
		}
	}
	return result
}

// TestConversionRoundtrip iterates over every API group registered in the
// scheme, discovers hub and spoke versions for each GroupKind, and runs both
// spoke→hub→spoke and hub→spoke→hub conversions, asserting that the result is
// identical to the input.
//
// All fuzzer configurations registered via WithFuzzerConfig are exercised for
// each (kind, version-pair).  Groups and kinds can be narrowed or excluded
// with the WithInclude* and WithExclude* options.  Any GroupKind with fewer
// than two registered versions is skipped with a log message.
func (rt *RoundTripTest) TestConversionRoundtrip(t *testing.T) {
	groupKinds := rt.groupsToKindFromScheme()
	for group, kinds := range groupKinds {
		if group == "" {
			// skip K8s core API group
			continue
		}
		namespaced := rt.providerNamespaced != nil && rt.providerNamespaced.RootGroup != "" &&
			strings.HasSuffix(group, rt.providerNamespaced.RootGroup)

		t.Run(group, func(t *testing.T) {
			t.Parallel()
			for _, gk := range kinds.UnsortedList() {
				availableVersions := rt.scheme.VersionsForGroupKind(gk)
				if len(availableVersions) < 2 {
					t.Logf("skipping %q, not multi-version ", gk)
					continue
				}

				t.Run(gk.Kind, func(t *testing.T) {
					t.Parallel()
					t.Logf("testing group %q", gk)
					// Each kind sub-test gets its own fillers so parallel kinds
					// within a group never share a randfill.Filler (not goroutine-safe).
					rt.testKind(t, gk, availableVersions, rt.getFillers(namespaced))
				})
			}
		})
	}
}

// customFuzzFuncs returns the set of fuzzer functions that are always applied:
// ASCIIStringFuzzer (to keep generated strings within printable ASCII) plus any
// functions registered by the caller via WithExtraFuzzFuncs.
func (rt *RoundTripTest) customFuzzFuncs(_ serializer.CodecFactory) []any {
	fuzzFns := make([]any, 0, len(rt.extraFuzzFns)+1)
	fuzzFns = append(fuzzFns, ASCIIStringFuzzer)
	fuzzFns = append(fuzzFns, rt.extraFuzzFns...)
	return fuzzFns
}

// testKind identifies the hub and spoke versions for gk, then runs
// spokeHubSpoke and hubSpokeHub sub-tests for every spoke version, serially.
// The test is failed if no hub version is found and skipped if any version
// lacks a conversion implementation.
func (rt *RoundTripTest) testKind(t *testing.T, gk schema.GroupKind, gvList []schema.GroupVersion, fillers []fillerWithIterations) {
	var hubVersion string
	spokes := make([]string, 0, len(gvList))
	for _, gv := range gvList {
		gvk := gk.WithVersion(gv.Version)
		object, err := rt.scheme.New(gvk)
		if err != nil {
			t.Fatalf("cannot create object from scheme for %q: %v", gvk.String(), err)
		}
		switch object.(type) {
		case conversion.Hub:
			if hubVersion != "" {
				t.Fatalf("multiple Hub versions detected for %s: %q and %q", gk.String(), hubVersion, gv.Version)
			}
			hubVersion = gv.Version
		case conversion.Convertible:
			spokes = append(spokes, gv.Version)
		default:
			t.Errorf("%s implements neither conversion.Hub nor conversion.Convertible", gvk)
		}
	}

	if hubVersion == "" {
		t.Fatalf("No hub version exists in scheme for %s", gk)
	}
	if len(spokes) == 0 {
		t.Fatalf("No spoke version exists in scheme for %s", gk)
	}
	t.Logf("Using hub version %q for %s", hubVersion, gk)
	t.Logf("Using spoke version(s) %q for %s", strings.Join(spokes, ","), gk)

	for _, spokeVersion := range spokes {
		t.Run(fmt.Sprintf("Spoke_%s_Over_Hub_%s", spokeVersion, hubVersion), func(t *testing.T) {
			t.Logf("%s: spoke_%s -> hub_%s -> spoke_%s", gk.String(), spokeVersion, hubVersion, spokeVersion)
			rt.spokeHubSpoke(t, gk.WithVersion(hubVersion), gk.WithVersion(spokeVersion), fillers)
		})
		t.Run(fmt.Sprintf("Hub_%s_Over_Spoke_%s", hubVersion, spokeVersion), func(t *testing.T) {
			t.Logf("%s: hub_%s -> spoke_%s -> hub_%s", gk.String(), hubVersion, spokeVersion, hubVersion)
			rt.hubSpokeHub(t, gk.WithVersion(hubVersion), gk.WithVersion(spokeVersion), fillers)
		})
	}
}

// spokeHubSpoke verifies the spoke→hub→spoke conversion cycle for every
// provided filler: a randomly filled spoke object is converted to the hub
// version and then back to a new spoke instance; the two spokes must be
// identical.  The cycle repeats fw.iterations times per filler.
func (rt *RoundTripTest) spokeHubSpoke(t *testing.T, hubGvk, spokeGvk schema.GroupVersionKind, fillers []fillerWithIterations) { //nolint:gocyclo // easier to follow as a unit
	for _, fw := range fillers {
		for range fw.iterations {
			spokeSrcRuntime, err := rt.scheme.New(spokeGvk)
			if err != nil {
				t.Fatalf("failed to instantiate object for spoke %s: %v", spokeGvk, err)
			}
			spokeSrc, ok := spokeSrcRuntime.(conversion.Convertible)
			if !ok {
				t.Fatalf("object is not Convertible: %v", spokeSrcRuntime)
			}

			hubIntermediateRuntime, err := rt.scheme.New(hubGvk)
			if err != nil {
				t.Fatalf("failed to instantiate object for hub %s: %v", hubGvk, err)
			}
			hubIntermediate, ok := hubIntermediateRuntime.(conversion.Hub)
			if !ok {
				t.Fatalf("object is not Hub: %v", hubIntermediateRuntime)
			}

			spokeFinalRuntime, err := rt.scheme.New(spokeGvk)
			if err != nil {
				t.Fatalf("failed to instantiate object for spoke %s: %v", spokeGvk, err)
			}
			spokeFinal, ok := spokeFinalRuntime.(conversion.Convertible)
			if !ok {
				t.Fatalf("object is not Convertible: %v", spokeFinalRuntime)
			}

			fw.filler.Fill(spokeSrcRuntime)

			if err := spokeSrc.ConvertTo(hubIntermediate); err != nil {
				t.Fatalf("spokeSrc.ConvertTo(hubIntermediate): %v", err)
			}
			if err := spokeFinal.ConvertFrom(hubIntermediate); err != nil {
				t.Fatalf("spokeFinal.ConvertFrom(hubIntermediate): %v", err)
			}
			rt.cmpK8sObjects(t, spokeSrc.DeepCopyObject(), spokeFinal.DeepCopyObject())
		}
	}
}

// hubSpokeHub verifies the hub→spoke→hub conversion cycle for every provided
// filler: a randomly filled hub object is converted to a spoke version and
// then back to a new hub instance; the two hubs must be identical.  The cycle
// repeats fw.iterations times per filler.
func (rt *RoundTripTest) hubSpokeHub(t *testing.T, hubGvk, spokeGvk schema.GroupVersionKind, fillers []fillerWithIterations) { //nolint:gocyclo // easier to follow as a unit
	for _, fw := range fillers {
		for range fw.iterations {
			srcHubRuntime, err := rt.scheme.New(hubGvk)
			if err != nil {
				t.Fatalf("failed to instantiate object for source Hub %s: %v", hubGvk, err)
			}
			srcHub, ok := srcHubRuntime.(conversion.Hub)
			if !ok {
				t.Fatalf("object does not implement conversion.Hub: %v", srcHubRuntime)
			}

			intermediateSpokeRuntime, err := rt.scheme.New(spokeGvk)
			if err != nil {
				t.Fatalf("failed to instantiate object for intermediate spoke %s: %v", spokeGvk, err)
			}
			intermediateSpoke, ok := intermediateSpokeRuntime.(conversion.Convertible)
			if !ok {
				t.Fatalf("object does not implement conversion.Convertible: %v", intermediateSpokeRuntime)
			}

			finalHubRuntime, err := rt.scheme.New(hubGvk)
			if err != nil {
				t.Fatalf("failed to instantiate object for final hub %s: %v", hubGvk, err)
			}
			finalHub, ok := finalHubRuntime.(conversion.Hub)
			if !ok {
				t.Fatalf("object is not a conversion.Hub: %v", finalHubRuntime)
			}

			fw.filler.Fill(srcHubRuntime)

			if err := intermediateSpoke.ConvertFrom(srcHub); err != nil {
				t.Logf("Source Hub Object: %+v", srcHub)
				t.Fatalf("intermediateSpoke.ConvertFrom(srcHub): %v", err)
			}
			if err := intermediateSpoke.ConvertTo(finalHub); err != nil {
				t.Logf("Source Hub Object: %+v", srcHub)
				t.Logf("intermediate Spoke Object: %+v", intermediateSpoke)
				t.Fatalf("intermediateSpoke.ConvertTo(finalHub): %v", err)
			}

			rt.cmpK8sObjects(t, srcHub.DeepCopyObject(), finalHub.DeepCopyObject())
		}
	}
}

// groupsToKindFromScheme returns a map from API group to the set of GroupKinds
// registered in that group, after applying include/exclude filters.  List kinds
// and Kubernetes meta-API kinds (Options, WatchEvent, …) are always excluded.
func (rt *RoundTripTest) groupsToKindFromScheme() map[string]sets.Set[schema.GroupKind] { //nolint:gocyclo // easier to follow as a unit
	hasIncludeFilter := rt.includeGroups.Len() > 0 || rt.includeGroupKinds.Len() > 0
	ret := make(map[string]sets.Set[schema.GroupKind])

	for gvk := range rt.scheme.AllKnownTypes() {
		if strings.HasSuffix(gvk.Kind, "List") || defaultIgnoredKinds.Has(gvk.Kind) {
			continue
		}
		if rt.excludeGroups.Has(gvk.Group) || rt.excludeGroupKinds.Has(gvk.GroupKind()) {
			continue
		}
		if hasIncludeFilter && !rt.includeGroups.Has(gvk.Group) && !rt.includeGroupKinds.Has(gvk.GroupKind()) {
			continue
		}
		if _, ok := ret[gvk.Group]; !ok {
			ret[gvk.Group] = sets.New[schema.GroupKind]()
		}
		ret[gvk.Group].Insert(gvk.GroupKind())
	}
	return ret
}
