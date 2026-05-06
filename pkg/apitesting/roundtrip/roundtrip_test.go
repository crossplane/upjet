// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0
package roundtrip

import (
	"fmt"
	"math/rand"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ---- minimal runtime.Object types for scheme registration ----

type (
	testNodeA struct {
		metav1.TypeMeta
		metav1.ObjectMeta
	}
	testNodeB struct {
		metav1.TypeMeta
		metav1.ObjectMeta
	}
	testNodeC struct {
		metav1.TypeMeta
		metav1.ObjectMeta
	}
	testNodeD struct {
		metav1.TypeMeta
		metav1.ObjectMeta
	}
	testNodeE struct {
		metav1.TypeMeta
		metav1.ObjectMeta
	}
)

func (o *testNodeA) DeepCopyObject() runtime.Object { c := *o; return &c }
func (o *testNodeB) DeepCopyObject() runtime.Object { c := *o; return &c }
func (o *testNodeC) DeepCopyObject() runtime.Object { c := *o; return &c }
func (o *testNodeD) DeepCopyObject() runtime.Object { c := *o; return &c }
func (o *testNodeE) DeepCopyObject() runtime.Object { c := *o; return &c }

// ---- hub and spoke types for conversion round-trip tests ----

// hubObject is a minimal conversion.Hub implementation.
type hubObject struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Value string
}

func (h *hubObject) DeepCopyObject() runtime.Object { c := *h; return &c }
func (h *hubObject) Hub()                           {}

// spokeObject is a minimal conversion.Convertible implementation.
// ConvertTo/ConvertFrom do a field-complete copy so the round-trip is lossless.
type spokeObject struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Value string
}

func (s *spokeObject) DeepCopyObject() runtime.Object { c := *s; return &c }

func (s *spokeObject) ConvertTo(dst conversion.Hub) error {
	hub, ok := dst.(*hubObject)
	if !ok {
		return fmt.Errorf("unexpected hub type %T", dst)
	}
	hub.ObjectMeta = s.ObjectMeta
	hub.Value = s.Value
	return nil
}

func (s *spokeObject) ConvertFrom(src conversion.Hub) error {
	hub, ok := src.(*hubObject)
	if !ok {
		return fmt.Errorf("unexpected hub type %T", src)
	}
	s.ObjectMeta = hub.ObjectMeta
	s.Value = hub.Value
	return nil
}

// ---- GVK constants used across tests ----

var (
	gvkWidgetV1       = schema.GroupVersionKind{Group: "a.io", Version: "v1", Kind: "Widget"}
	gvkWidget2V1      = schema.GroupVersionKind{Group: "a.io", Version: "v1", Kind: "Widget2"}
	gvkWidgetListV1   = schema.GroupVersionKind{Group: "a.io", Version: "v1", Kind: "WidgetList"}
	gvkDeleteV1       = schema.GroupVersionKind{Group: "a.io", Version: "v1", Kind: "DeleteOptions"}
	gvkWidgetInternal = schema.GroupVersionKind{Group: "a.io", Version: runtime.APIVersionInternal, Kind: "Widget"}
	gvkGadgetV1       = schema.GroupVersionKind{Group: "b.io", Version: "v1", Kind: "Gadget"}
)

// makeFilterTestScheme returns a scheme populated with types covering the
// boundary conditions exercised by groupsToKindFromScheme and
// nonRoundTrippableTypes:
//
//   - Widget/Widget2 in "a.io" v1 (two normal kinds in one group)
//   - WidgetList     in "a.io" v1 (List-suffix → always excluded)
//   - DeleteOptions  in "a.io" v1 (defaultIgnoredKinds member → always excluded)
//   - Widget         in "a.io" __internal (internal version → excluded by nonRoundTrippable)
//   - Gadget         in "b.io" v1 (second group)
func makeFilterTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(gvkWidgetV1, &testNodeA{})
	s.AddKnownTypeWithName(gvkWidget2V1, &testNodeB{})
	s.AddKnownTypeWithName(gvkWidgetListV1, &testNodeC{})
	s.AddKnownTypeWithName(gvkDeleteV1, &testNodeD{})
	s.AddKnownTypeWithName(gvkWidgetInternal, &testNodeE{})
	s.AddKnownTypeWithName(gvkGadgetV1, &testNodeA{})
	return s
}

// newTestRT returns a RoundTripTest with all set fields initialised and the
// given scheme attached.  It does NOT call setupTestInfrastructure (which
// would touch the global conversion singleton), so it is safe to call from
// many test functions in the same binary.
func newTestRT(s *runtime.Scheme) *RoundTripTest {
	return &RoundTripTest{
		scheme:            s,
		includeGroups:     sets.New[string](),
		includeGroupKinds: sets.New[schema.GroupKind](),
		excludeGroups:     sets.New[string](),
		excludeGroupKinds: sets.New[schema.GroupKind](),
		cmpOpts:           append([]cmp.Option(nil), defaultComparisonOptions...),
	}
}

// ---- FuzzerOption unit tests ----

func TestFuzzerOptions(t *testing.T) {
	t.Run("FuzzerIterations", func(t *testing.T) {
		opts := fuzzerOptions{}
		FuzzerIterations(7)(&opts)
		if opts.FuzzIterations == nil || *opts.FuzzIterations != 7 {
			t.Errorf("expected 7, got %v", opts.FuzzIterations)
		}
	})

	t.Run("FuzzerNilChance", func(t *testing.T) {
		opts := fuzzerOptions{}
		FuzzerNilChance(0.3)(&opts)
		if opts.NilChance == nil || *opts.NilChance != 0.3 {
			t.Errorf("expected 0.3, got %v", opts.NilChance)
		}
	})

	t.Run("FuzzerNumElements", func(t *testing.T) {
		opts := fuzzerOptions{}
		FuzzerNumElements(2, 8)(&opts)
		if opts.MinElements == nil || *opts.MinElements != 2 {
			t.Errorf("MinElements: expected 2, got %v", opts.MinElements)
		}
		if opts.MaxElements == nil || *opts.MaxElements != 8 {
			t.Errorf("MaxElements: expected 8, got %v", opts.MaxElements)
		}
	})

	t.Run("FuzzerMaxDepth", func(t *testing.T) {
		opts := fuzzerOptions{}
		FuzzerMaxDepth(10)(&opts)
		if opts.MaxDepth == nil || *opts.MaxDepth != 10 {
			t.Errorf("expected 10, got %v", opts.MaxDepth)
		}
	})

	t.Run("FuzzerRandSource", func(t *testing.T) {
		src := rand.NewSource(42)
		opts := fuzzerOptions{}
		FuzzerRandSource(src)(&opts)
		if opts.RandSource != src {
			t.Error("RandSource not stored")
		}
	})

	t.Run("FuzzerSkipPatterns", func(t *testing.T) {
		p1 := regexp.MustCompile("^skip")
		p2 := regexp.MustCompile("^ignore")
		opts := fuzzerOptions{}
		FuzzerSkipPatterns(p1, p2)(&opts)
		if len(opts.SkipPatterns) != 2 {
			t.Fatalf("expected 2 patterns, got %d", len(opts.SkipPatterns))
		}
		if opts.SkipPatterns[0] != p1 || opts.SkipPatterns[1] != p2 {
			t.Error("patterns not stored in order")
		}
	})

	t.Run("FuzzerSkipPatterns_append", func(t *testing.T) {
		p1 := regexp.MustCompile("^a")
		p2 := regexp.MustCompile("^b")
		opts := fuzzerOptions{}
		FuzzerSkipPatterns(p1)(&opts)
		FuzzerSkipPatterns(p2)(&opts)
		if len(opts.SkipPatterns) != 2 {
			t.Fatalf("expected 2 patterns after two calls, got %d", len(opts.SkipPatterns))
		}
	})

	t.Run("FuzzerAllowUnexportedFields_true", func(t *testing.T) {
		opts := fuzzerOptions{}
		FuzzerAllowUnexportedFields(true)(&opts)
		if opts.AllowUnexportedFields == nil || !*opts.AllowUnexportedFields {
			t.Errorf("expected true, got %v", opts.AllowUnexportedFields)
		}
	})

	t.Run("FuzzerAllowUnexportedFields_false", func(t *testing.T) {
		opts := fuzzerOptions{}
		FuzzerAllowUnexportedFields(false)(&opts)
		if opts.AllowUnexportedFields == nil || *opts.AllowUnexportedFields {
			t.Errorf("expected false, got %v", opts.AllowUnexportedFields)
		}
	})
}

// ---- TestOption unit tests ----

func TestTestOptions(t *testing.T) {
	t.Run("WithFuzzerConfig_appends", func(t *testing.T) {
		rt := newTestRT(runtime.NewScheme())
		n := 7
		WithFuzzerConfig(FuzzerIterations(n))(rt)
		if len(rt.fuzzerConfigs) != 1 {
			t.Fatalf("expected 1 config, got %d", len(rt.fuzzerConfigs))
		}
		if rt.fuzzerConfigs[0].FuzzIterations == nil || *rt.fuzzerConfigs[0].FuzzIterations != n {
			t.Errorf("FuzzIterations not set in config")
		}

		WithFuzzerConfig(FuzzerNilChance(0.5))(rt)
		if len(rt.fuzzerConfigs) != 2 {
			t.Fatalf("second WithFuzzerConfig did not append; got %d configs", len(rt.fuzzerConfigs))
		}
	})

	t.Run("WithIncludeGroups", func(t *testing.T) {
		rt := newTestRT(runtime.NewScheme())
		WithIncludeGroups("a.io", "b.io")(rt)
		if !rt.includeGroups.Has("a.io") || !rt.includeGroups.Has("b.io") {
			t.Error("groups not inserted into includeGroups")
		}
		if rt.includeGroups.Has("c.io") {
			t.Error("unexpected group present")
		}
	})

	t.Run("WithExcludeGroups", func(t *testing.T) {
		rt := newTestRT(runtime.NewScheme())
		WithExcludeGroups("x.io")(rt)
		if !rt.excludeGroups.Has("x.io") {
			t.Error("group not inserted into excludeGroups")
		}
	})

	t.Run("WithIncludeGroupKinds", func(t *testing.T) {
		rt := newTestRT(runtime.NewScheme())
		gk := schema.GroupKind{Group: "a.io", Kind: "Widget"}
		WithIncludeGroupKinds(gk)(rt)
		if !rt.includeGroupKinds.Has(gk) {
			t.Error("GroupKind not inserted into includeGroupKinds")
		}
	})

	t.Run("WithExcludeGroupKinds", func(t *testing.T) {
		rt := newTestRT(runtime.NewScheme())
		gk := schema.GroupKind{Group: "a.io", Kind: "Widget"}
		WithExcludeGroupKinds(gk)(rt)
		if !rt.excludeGroupKinds.Has(gk) {
			t.Error("GroupKind not inserted into excludeGroupKinds")
		}
	})

	t.Run("WithComparisonOptions_appends", func(t *testing.T) {
		rt := newTestRT(runtime.NewScheme())
		before := len(rt.cmpOpts)
		WithComparisonOptions(EquateEmptyAndSingleZeroSlice())(rt)
		if len(rt.cmpOpts) != before+1 {
			t.Errorf("expected %d cmpOpts, got %d", before+1, len(rt.cmpOpts))
		}
	})

	t.Run("WithExtraFuzzFuncs_appends", func(t *testing.T) {
		rt := newTestRT(runtime.NewScheme())
		fn := func() {}
		WithExtraFuzzFuncs(fn)(rt)
		if len(rt.extraFuzzFns) != 1 {
			t.Fatalf("expected 1 extra fuzz fn, got %d", len(rt.extraFuzzFns))
		}
	})

	t.Run("WithCodecFactory_sets_custom", func(t *testing.T) {
		rt := newTestRT(runtime.NewScheme())
		cf := serializer.NewCodecFactory(runtime.NewScheme())
		WithCodecFactory(cf)(rt)
		if !rt.useCustomCodec {
			t.Error("useCustomCodec should be true after WithCodecFactory")
		}
	})
}

// ---- groupsToKindFromScheme tests ----

func TestGroupsToKindFromScheme(t *testing.T) {
	scheme := makeFilterTestScheme(t)

	gkWidget := gvkWidgetV1.GroupKind()
	gkWidget2 := gvkWidget2V1.GroupKind()
	gkGadget := gvkGadgetV1.GroupKind()

	cases := []struct {
		name          string
		apply         func(*RoundTripTest)
		expectIn      map[string][]schema.GroupKind // group → kinds that must be present
		expectAbsent  map[string][]schema.GroupKind // group → kinds that must be absent
		expectNoGroup []string                      // groups that must not appear at all
	}{
		{
			name:  "no filter returns all non-List non-ignored kinds",
			apply: func(*RoundTripTest) {},
			expectIn: map[string][]schema.GroupKind{
				"a.io": {gkWidget, gkWidget2},
				"b.io": {gkGadget},
			},
			// List-suffix and defaultIgnoredKinds must never appear
			expectAbsent: map[string][]schema.GroupKind{
				"a.io": {gvkWidgetListV1.GroupKind(), gvkDeleteV1.GroupKind()},
			},
		},
		{
			name:  "ExcludeGroups removes entire group",
			apply: func(rt *RoundTripTest) { WithExcludeGroups("a.io")(rt) },
			expectIn: map[string][]schema.GroupKind{
				"b.io": {gkGadget},
			},
			expectNoGroup: []string{"a.io"},
		},
		{
			name: "ExcludeGroupKinds removes specific kind within group",
			apply: func(rt *RoundTripTest) {
				WithExcludeGroupKinds(gkWidget)(rt)
			},
			expectIn: map[string][]schema.GroupKind{
				"a.io": {gkWidget2},
				"b.io": {gkGadget},
			},
			expectAbsent: map[string][]schema.GroupKind{
				"a.io": {gkWidget},
			},
		},
		{
			name:  "IncludeGroups restricts to listed group only",
			apply: func(rt *RoundTripTest) { WithIncludeGroups("b.io")(rt) },
			expectIn: map[string][]schema.GroupKind{
				"b.io": {gkGadget},
			},
			expectNoGroup: []string{"a.io"},
		},
		{
			name: "IncludeGroupKinds restricts to listed kind only",
			apply: func(rt *RoundTripTest) {
				WithIncludeGroupKinds(gkWidget)(rt)
			},
			expectIn: map[string][]schema.GroupKind{
				"a.io": {gkWidget},
			},
			expectAbsent: map[string][]schema.GroupKind{
				"a.io": {gkWidget2},
			},
			expectNoGroup: []string{"b.io"},
		},
		{
			name: "IncludeGroups with ExcludeGroupKinds",
			apply: func(rt *RoundTripTest) {
				WithIncludeGroups("a.io")(rt)
				WithExcludeGroupKinds(gkWidget)(rt)
			},
			expectIn: map[string][]schema.GroupKind{
				"a.io": {gkWidget2},
			},
			expectAbsent: map[string][]schema.GroupKind{
				"a.io": {gkWidget},
			},
			expectNoGroup: []string{"b.io"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := newTestRT(scheme)
			tc.apply(rt)
			result := rt.groupsToKindFromScheme()

			for group, kinds := range tc.expectIn {
				gks, ok := result[group]
				if !ok {
					t.Errorf("group %q missing from result", group)
					continue
				}
				for _, gk := range kinds {
					if !gks.Has(gk) {
						t.Errorf("group %q missing expected kind %q", group, gk.Kind)
					}
				}
			}

			for group, kinds := range tc.expectAbsent {
				gks, ok := result[group]
				if !ok {
					continue // group absent → kinds absent trivially
				}
				for _, gk := range kinds {
					if gks.Has(gk) {
						t.Errorf("group %q unexpectedly contains kind %q", group, gk.Kind)
					}
				}
			}

			for _, group := range tc.expectNoGroup {
				if _, ok := result[group]; ok {
					t.Errorf("group %q should not appear in result", group)
				}
			}
		})
	}
}

// ---- nonRoundTrippableTypes tests ----

func TestNonRoundTrippableTypes(t *testing.T) {
	scheme := makeFilterTestScheme(t)

	t.Run("no filter returns nil", func(t *testing.T) {
		rt := newTestRT(scheme)
		if got := rt.nonRoundTrippableTypes(); got != nil {
			t.Errorf("expected nil with no filters, got %v", got)
		}
	})

	t.Run("ExcludeGroups marks excluded GVKs as non-round-trippable", func(t *testing.T) {
		rt := newTestRT(scheme)
		WithExcludeGroups("a.io")(rt)
		nrt := rt.nonRoundTrippableTypes()

		// External-version Widget in excluded group must be non-round-trippable.
		if !nrt[gvkWidgetV1] {
			t.Errorf("expected %v to be non-round-trippable", gvkWidgetV1)
		}
		// Gadget in the allowed group must NOT be non-round-trippable.
		if nrt[gvkGadgetV1] {
			t.Errorf("expected %v to be round-trippable", gvkGadgetV1)
		}
		// Internal-version Widget must not appear (internal versions are skipped).
		if nrt[gvkWidgetInternal] {
			t.Errorf("internal-version GVK %v must not appear in nonRoundTrippableTypes", gvkWidgetInternal)
		}
	})

	t.Run("IncludeGroups marks non-included GVKs as non-round-trippable", func(t *testing.T) {
		rt := newTestRT(scheme)
		WithIncludeGroups("b.io")(rt)
		nrt := rt.nonRoundTrippableTypes()

		if !nrt[gvkWidgetV1] {
			t.Errorf("expected %v (excluded by include filter) to be non-round-trippable", gvkWidgetV1)
		}
		if nrt[gvkGadgetV1] {
			t.Errorf("expected %v (in included group) to be round-trippable", gvkGadgetV1)
		}
	})
}

// ---- getFillers tests ----

func TestGetFillers(t *testing.T) {
	s := runtime.NewScheme()
	cf := serializer.NewCodecFactory(s)

	iters5, iters10 := 5, 10

	rt := newTestRT(s)
	rt.codecFactory = cf
	rt.fuzzerConfigs = []fuzzerOptions{
		{FuzzIterations: &iters5},
		{}, // nil FuzzIterations → default
		{FuzzIterations: &iters10},
	}

	t.Run("returns one filler per config", func(t *testing.T) {
		fillers := rt.getFillers(false)
		if len(fillers) != 3 {
			t.Fatalf("expected 3 fillers, got %d", len(fillers))
		}
	})

	t.Run("custom iteration count is honoured", func(t *testing.T) {
		fillers := rt.getFillers(false)
		if fillers[0].iterations != iters5 {
			t.Errorf("expected %d, got %d", iters5, fillers[0].iterations)
		}
		if fillers[2].iterations != iters10 {
			t.Errorf("expected %d, got %d", iters10, fillers[2].iterations)
		}
	})

	t.Run("nil FuzzIterations uses default", func(t *testing.T) {
		fillers := rt.getFillers(false)
		if fillers[1].iterations != defaultFuzzIterations {
			t.Errorf("expected defaultFuzzIterations (%d), got %d",
				defaultFuzzIterations, fillers[1].iterations)
		}
	})

	t.Run("cluster-scoped filler zeroes Namespace", func(t *testing.T) {
		fillers := rt.getFillers(false)
		var meta metav1.ObjectMeta
		fillers[0].filler.Fill(&meta)
		if meta.Namespace != "" {
			t.Errorf("cluster-scoped filler produced non-empty Namespace: %q", meta.Namespace)
		}
	})

	t.Run("namespaced filler produces non-empty Namespace in most runs", func(t *testing.T) {
		fillers := rt.getFillers(true)
		sawNonEmpty := false
		for range 50 {
			var meta metav1.ObjectMeta
			fillers[0].filler.Fill(&meta)
			if meta.Namespace != "" {
				sawNonEmpty = true
				break
			}
		}
		if !sawNonEmpty {
			t.Error("namespaced filler never produced non-empty Namespace in 50 runs")
		}
	})
}

// ---- spokeHubSpoke / hubSpokeHub tests ----

// makeConversionTestRT builds a RoundTripTest with hub (v1) and spoke (v2alpha1)
// registered in the scheme, ready for spokeHubSpoke and hubSpokeHub calls.
func makeConversionTestRT(t *testing.T) (*RoundTripTest, schema.GroupVersionKind, schema.GroupVersionKind) {
	t.Helper()

	hubGVK := schema.GroupVersionKind{Group: "rt.test.io", Version: "v1", Kind: "TestObj"}
	spokeGVK := schema.GroupVersionKind{Group: "rt.test.io", Version: "v2alpha1", Kind: "TestObj"}

	s := runtime.NewScheme()
	s.AddKnownTypeWithName(hubGVK, &hubObject{})
	s.AddKnownTypeWithName(spokeGVK, &spokeObject{})

	src := rand.NewSource(99)
	iters := 10

	rt := newTestRT(s)
	rt.codecFactory = serializer.NewCodecFactory(s)
	rt.fuzzerConfigs = []fuzzerOptions{{
		FuzzIterations: &iters,
		RandSource:     src,
	}}

	return rt, hubGVK, spokeGVK
}

func TestSpokeHubSpoke(t *testing.T) {
	rt, hubGVK, spokeGVK := makeConversionTestRT(t)
	fillers := rt.getFillers(false)
	rt.spokeHubSpoke(t, hubGVK, spokeGVK, fillers)
}

func TestHubSpokeHub(t *testing.T) {
	rt, hubGVK, spokeGVK := makeConversionTestRT(t)
	fillers := rt.getFillers(false)
	rt.hubSpokeHub(t, hubGVK, spokeGVK, fillers)
}
