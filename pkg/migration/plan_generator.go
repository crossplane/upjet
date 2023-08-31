// Copyright 2022 Upbound Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package migration

import (
	"fmt"

	"reflect"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	xpv1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	xpmetav1 "github.com/crossplane/crossplane/apis/pkg/meta/v1"
	xpmetav1alpha1 "github.com/crossplane/crossplane/apis/pkg/meta/v1alpha1"
	xppkgv1 "github.com/crossplane/crossplane/apis/pkg/v1"
	xppkgv1beta1 "github.com/crossplane/crossplane/apis/pkg/v1beta1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
)

const (
	errSourceHasNext                   = "failed to generate migration plan: Could not check next object from source"
	errSourceNext                      = "failed to generate migration plan: Could not get next object from source"
	errPreProcessFmt                   = "failed to pre-process the manifest of category %q"
	errSourceReset                     = "failed to generate migration plan: Could not get reset the source"
	errUnstructuredConvert             = "failed to convert from unstructured object to v1.Composition"
	errUnstructuredMarshal             = "failed to marshal unstructured object to JSON"
	errResourceMigrate                 = "failed to migrate resource"
	errCompositePause                  = "failed to pause composite resource"
	errCompositesEdit                  = "failed to edit composite resources"
	errCompositesStart                 = "failed to start composite resources"
	errCompositionMigrateFmt           = "failed to migrate the composition: %s"
	errConfigurationMetadataMigrateFmt = "failed to migrate the configuration metadata: %s"
	errConfigurationPackageMigrateFmt  = "failed to migrate the configuration package: %s"
	errProviderMigrateFmt              = "failed to migrate the Provider package: %s"
	errLockMigrateFmt                  = "failed to migrate the package lock: %s"
	errComposedTemplateBase            = "failed to migrate the base of a composed template"
	errComposedTemplateMigrate         = "failed to migrate the composed templates of the composition"
	errResourceOutput                  = "failed to output migrated resource"
	errResourceOrphan                  = "failed to orphan managed resource"
	errCompositionOutput               = "failed to output migrated composition"
	errCompositeOutput                 = "failed to output migrated composite"
	errClaimOutput                     = "failed to output migrated claim"
	errClaimsEdit                      = "failed to edit claims"
	errPlanGeneration                  = "failed to generate the migration plan"
	errPause                           = "failed to store a paused manifest"
	errMissingGVK                      = "managed resource is missing its GVK. Resource converters must set GVKs on any managed resources they newly generate."
)

const (
	versionV010 = "0.1.0"

	keyCompositionRef = "compositionRef"
	keyResourceRefs   = "resourceRefs"
)

// PlanGeneratorOption configures a PlanGenerator
type PlanGeneratorOption func(generator *PlanGenerator)

// WithErrorOnInvalidPatchSchema returns a PlanGeneratorOption for configuring
// whether the PlanGenerator should error and stop the migration plan
// generation in case an error is encountered while checking a patch
// statement's conformance to the migration source or target.
func WithErrorOnInvalidPatchSchema(e bool) PlanGeneratorOption {
	return func(pg *PlanGenerator) {
		pg.ErrorOnInvalidPatchSchema = e
	}
}

// WithSkipGVKs configures the set of GVKs to skip for conversion
// during a migration.
func WithSkipGVKs(gvk ...schema.GroupVersionKind) PlanGeneratorOption {
	return func(pg *PlanGenerator) {
		pg.SkipGVKs = gvk
	}
}

// WithMultipleSources can be used to configure multiple sources for a
// PlanGenerator.
func WithMultipleSources(source ...Source) PlanGeneratorOption {
	return func(pg *PlanGenerator) {
		pg.source = &sources{backends: source}
	}
}

// WithEnableConfigurationMigrationSteps enables only
// the configuration migration steps.
// TODO: to be replaced with a higher abstraction encapsulating
// migration scenarios.
func WithEnableConfigurationMigrationSteps() PlanGeneratorOption {
	return func(pg *PlanGenerator) {
		pg.enabledSteps = getConfigurationMigrationSteps()
	}
}

func WithEnableOnlyFileSystemAPISteps() PlanGeneratorOption {
	return func(pg *PlanGenerator) {
		pg.enabledSteps = getAPIMigrationStepsFileSystemMode()
	}
}

type sources struct {
	backends []Source
	i        int
}

func (s *sources) HasNext() (bool, error) {
	if s.i >= len(s.backends) {
		return false, nil
	}
	ok, err := s.backends[s.i].HasNext()
	if err != nil || ok {
		return ok, err
	}
	s.i++
	return s.HasNext()
}

func (s *sources) Next() (UnstructuredWithMetadata, error) {
	return s.backends[s.i].Next()
}

func (s *sources) Reset() error {
	for _, src := range s.backends {
		if err := src.Reset(); err != nil {
			return err
		}
	}
	s.i = 0
	return nil
}

// PlanGenerator generates a migration.Plan reading the manifests available
// from `source`, converting managed resources and compositions using the
// available `migration.Converter`s registered in the `registry` and
// writing the output manifests to the specified `target`.
type PlanGenerator struct {
	source       Source
	target       Target
	registry     *Registry
	subSteps     map[step]string
	enabledSteps []step
	// Plan is the migration.Plan whose steps are expected
	// to complete a migration when they're executed in order.
	Plan Plan
	// ErrorOnInvalidPatchSchema errors and stops plan generation in case
	// an error is encountered while checking the conformance of a patch
	// statement against the migration source or the migration target.
	ErrorOnInvalidPatchSchema bool
	// GVKs of managed resources that
	// should be skipped for conversion during the migration, if no
	// converters are registered for them. If any of the GVK components
	// is left empty, it will be a wildcard component.
	// Exact matching with an empty group name is not possible.
	SkipGVKs []schema.GroupVersionKind
}

// NewPlanGenerator constructs a new PlanGenerator using the specified
// Source and Target and the default converter Registry.
func NewPlanGenerator(registry *Registry, source Source, target Target, opts ...PlanGeneratorOption) PlanGenerator {
	pg := &PlanGenerator{
		source:       &sources{backends: []Source{source}},
		target:       target,
		registry:     registry,
		subSteps:     map[step]string{},
		enabledSteps: getAPIMigrationSteps(),
	}
	for _, o := range opts {
		o(pg)
	}
	return *pg
}

// GeneratePlan generates a migration plan for the manifests available from
// the configured Source and writing them to the configured Target using the
// configured converter Registry. The generated Plan is available in the
// PlanGenerator.Plan variable if the generation is successful
// (i.e., no errors are reported).
func (pg *PlanGenerator) GeneratePlan() error {
	pg.Plan.Spec.stepMap = make(map[string]*Step)
	pg.Plan.Version = versionV010
	defer pg.commitSteps()
	if err := pg.preProcess(); err != nil {
		return err
	}
	if err := pg.source.Reset(); err != nil {
		return errors.Wrap(err, errSourceReset)
	}
	return errors.Wrap(pg.convert(), errPlanGeneration)
}

func (pg *PlanGenerator) preProcess() error {
	if len(pg.registry.unstructuredPreProcessors) == 0 {
		return nil
	}
	for hasNext, err := pg.source.HasNext(); ; hasNext, err = pg.source.HasNext() {
		if err != nil {
			return errors.Wrap(err, errSourceHasNext)
		}
		if !hasNext {
			break
		}
		o, err := pg.source.Next()
		if err != nil {
			return errors.Wrap(err, errSourceNext)
		}
		for _, pp := range pg.registry.unstructuredPreProcessors[o.Metadata.Category] {
			if err := pp.PreProcess(o); err != nil {
				return errors.Wrapf(err, errPreProcessFmt, o.Metadata.Category)
			}
		}
	}
	return nil
}

func (pg *PlanGenerator) convertPatchSets(o UnstructuredWithMetadata) ([]string, error) {
	var converted []string
	for _, psConv := range pg.registry.patchSetConverters {
		if psConv.re == nil || psConv.converter == nil {
			continue
		}
		if !psConv.re.MatchString(o.Object.GetName()) {
			continue
		}
		c, err := ToComposition(o.Object)
		if err != nil {
			return nil, errors.Wrap(err, errUnstructuredConvert)
		}
		oldPatchSets := make([]xpv1.PatchSet, len(c.Spec.PatchSets))
		for i, ps := range c.Spec.PatchSets {
			oldPatchSets[i] = *ps.DeepCopy()
		}
		psMap := convertToMap(c.Spec.PatchSets)
		if err := psConv.converter.PatchSets(psMap); err != nil {
			return nil, errors.Wrapf(err, "failed to call PatchSet converter on Composition: %s", c.GetName())
		}
		newPatchSets := convertFromMap(psMap, oldPatchSets, true)
		converted = append(converted, getConvertedPatchSetNames(newPatchSets, oldPatchSets)...)
		pv := fieldpath.Pave(o.Object.Object)
		if err := pv.SetValue("spec.patchSets", newPatchSets); err != nil {
			return nil, errors.Wrapf(err, "failed to set converted patch sets on Composition: %s", c.GetName())
		}
	}
	return converted, nil
}

func (pg *PlanGenerator) categoricalConvert(u *UnstructuredWithMetadata) error {
	if u.Metadata.Category == categoryUnknown {
		return nil
	}
	source := *u
	source.Object = *u.Object.DeepCopy()
	converters := pg.registry.categoricalConverters[u.Metadata.Category]
	if converters == nil {
		return nil
	}
	// TODO: if a categorical converter does not convert the given object,
	// we will have a false positive. Better to compute and check
	// a diff here.
	for _, converter := range converters {
		if err := converter.Convert(u); err != nil {
			return errors.Wrapf(err, "failed to convert unstructured object of category: %s", u.Metadata.Category)
		}
	}
	return pg.stepEditCategory(source, u)
}

func (pg *PlanGenerator) convert() error { //nolint: gocyclo
	convertedMR := make(map[corev1.ObjectReference][]UnstructuredWithMetadata)
	convertedComposition := make(map[string]string)
	var composites []UnstructuredWithMetadata
	var claims []UnstructuredWithMetadata
	for hasNext, err := pg.source.HasNext(); ; hasNext, err = pg.source.HasNext() {
		if err != nil {
			return errors.Wrap(err, errSourceHasNext)
		}
		if !hasNext {
			break
		}
		o, err := pg.source.Next()
		if err != nil {
			return errors.Wrap(err, errSourceNext)
		}

		if err := pg.categoricalConvert(&o); err != nil {
			return err
		}

		switch gvk := o.Object.GroupVersionKind(); gvk {
		case xppkgv1.ConfigurationGroupVersionKind:
			if err := pg.convertConfigurationPackage(o); err != nil {
				return errors.Wrapf(err, errConfigurationPackageMigrateFmt, o.Object.GetName())
			}
		case xpmetav1.ConfigurationGroupVersionKind, xpmetav1alpha1.ConfigurationGroupVersionKind:
			if err := pg.convertConfigurationMetadata(o); err != nil {
				return errors.Wrapf(err, errConfigurationMetadataMigrateFmt, o.Object.GetName())
			}
			pg.stepBackupAllResources()
			pg.stepBuildConfiguration()
			pg.stepPushConfiguration()
		case xpv1.CompositionGroupVersionKind:
			target, converted, err := pg.convertComposition(o)
			if err != nil {
				return errors.Wrapf(err, errCompositionMigrateFmt, o.Object.GetName())
			}
			if converted {
				migratedName := fmt.Sprintf("%s-migrated", o.Object.GetName())
				convertedComposition[o.Object.GetName()] = migratedName
				target.Object.SetName(migratedName)
				if err := pg.stepNewComposition(target); err != nil {
					return errors.Wrapf(err, errCompositionMigrateFmt, o.Object.GetName())
				}
			}
		case xppkgv1.ProviderGroupVersionKind:
			isConverted, err := pg.convertProviderPackage(o)
			if err != nil {
				return errors.Wrap(err, errProviderMigrateFmt)
			}
			if isConverted {
				if err := pg.stepDeleteMonolith(o); err != nil {
					return err
				}
			}
		case xppkgv1beta1.LockGroupVersionKind:
			if err := pg.convertPackageLock(o); err != nil {
				return errors.Wrapf(err, errLockMigrateFmt, o.Object.GetName())
			}
		default:
			if o.Metadata.Category == CategoryComposite {
				if err := pg.stepPauseComposite(&o); err != nil {
					return errors.Wrap(err, errCompositePause)
				}
				composites = append(composites, o)
				continue
			}

			if o.Metadata.Category == CategoryClaim {
				claims = append(claims, o)
				continue
			}

			targets, converted, err := pg.convertResource(o, false)
			if err != nil {
				return errors.Wrap(err, errResourceMigrate)
			}
			if converted {
				convertedMR[corev1.ObjectReference{
					Kind:       gvk.Kind,
					Name:       o.Object.GetName(),
					APIVersion: gvk.GroupVersion().String(),
				}] = targets
				for _, tu := range targets {
					tu := tu
					if err := pg.stepNewManagedResource(&tu); err != nil {
						return errors.Wrap(err, errResourceMigrate)
					}
					if err := pg.stepStartManagedResource(&tu); err != nil {
						return errors.Wrap(err, errResourceMigrate)
					}
				}
			} else if _, ok, _ := toManagedResource(pg.registry.scheme, o.Object); ok {
				if err := pg.stepStartManagedResource(&o); err != nil {
					return errors.Wrap(err, errResourceMigrate)
				}
			}
		}
		if err := pg.addStepsForManagedResource(&o); err != nil {
			return err
		}
	}
	if err := pg.stepEditComposites(composites, convertedMR, convertedComposition); err != nil {
		return errors.Wrap(err, errCompositesEdit)
	}
	if err := pg.stepStartComposites(composites); err != nil {
		return errors.Wrap(err, errCompositesStart)
	}
	if err := pg.stepEditClaims(claims, convertedComposition); err != nil {
		return errors.Wrap(err, errClaimsEdit)
	}
	return nil
}

func (pg *PlanGenerator) convertResource(o UnstructuredWithMetadata, compositionContext bool) ([]UnstructuredWithMetadata, bool, error) {
	gvk := o.Object.GroupVersionKind()
	conv := pg.registry.resourceConverters[gvk]
	if conv == nil {
		return []UnstructuredWithMetadata{o}, false, nil
	}
	// we have already ensured that the GVK belongs to a managed resource type
	mg, _, err := toManagedResource(pg.registry.scheme, o.Object)
	if err != nil {
		return nil, false, errors.Wrap(err, errResourceMigrate)
	}
	resources, err := conv.Resource(mg)
	if err != nil {
		return nil, false, errors.Wrap(err, errResourceMigrate)
	}
	if err := assertGVK(resources); err != nil {
		return nil, true, errors.Wrap(err, errResourceMigrate)
	}
	if !compositionContext {
		assertMetadataName(mg.GetName(), resources)
	}
	converted := make([]UnstructuredWithMetadata, 0, len(resources))
	for _, mg := range resources {
		converted = append(converted, UnstructuredWithMetadata{
			Object:   ToSanitizedUnstructured(mg),
			Metadata: o.Metadata,
		})
	}
	return converted, true, nil
}

func assertGVK(resources []resource.Managed) error {
	for _, r := range resources {
		if reflect.ValueOf(r.GetObjectKind().GroupVersionKind()).IsZero() {
			return errors.New(errMissingGVK)
		}
	}
	return nil
}

func assertMetadataName(parentName string, resources []resource.Managed) {
	for i, r := range resources {
		if len(r.GetName()) != 0 || len(r.GetGenerateName()) != 0 {
			continue
		}
		resources[i].SetGenerateName(fmt.Sprintf("%s-", parentName))
	}
}

func (pg *PlanGenerator) convertComposition(o UnstructuredWithMetadata) (*UnstructuredWithMetadata, bool, error) { // nolint:gocyclo
	convertedPS, err := pg.convertPatchSets(o)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to convert patch sets")
	}
	comp, err := ToComposition(o.Object)
	if err != nil {
		return nil, false, errors.Wrap(err, errUnstructuredConvert)
	}
	var targetResources []*xpv1.ComposedTemplate
	isConverted := false
	for _, cmp := range comp.Spec.Resources {
		u, err := FromRawExtension(cmp.Base)
		if err != nil {
			return nil, false, errors.Wrapf(err, errCompositionMigrateFmt, o.Object.GetName())
		}
		gvk := u.GroupVersionKind()
		converted, ok, err := pg.convertResource(UnstructuredWithMetadata{
			Object:   u,
			Metadata: o.Metadata,
		}, true)
		if err != nil {
			return nil, false, errors.Wrap(err, errComposedTemplateBase)
		}
		isConverted = isConverted || ok
		cmps := make([]*xpv1.ComposedTemplate, 0, len(converted))
		sourceNameUsed := false
		for _, u := range converted {
			buff, err := u.Object.MarshalJSON()
			if err != nil {
				return nil, false, errors.Wrap(err, errUnstructuredMarshal)
			}
			c := cmp.DeepCopy()
			c.Base = runtime.RawExtension{
				Raw: buff,
			}
			if err := pg.setDefaultsOnTargetTemplate(cmp.Name, &sourceNameUsed, gvk, u.Object.GroupVersionKind(), c, comp.Spec.PatchSets, convertedPS); err != nil {
				return nil, false, errors.Wrap(err, errComposedTemplateMigrate)
			}
			cmps = append(cmps, c)
		}
		conv := pg.registry.templateConverters[gvk]
		if conv != nil {
			if err := conv.ComposedTemplate(cmp, cmps...); err != nil {
				return nil, false, errors.Wrap(err, errComposedTemplateMigrate)
			}
		}
		targetResources = append(targetResources, cmps...)
	}
	comp.Spec.Resources = make([]xpv1.ComposedTemplate, 0, len(targetResources))
	for _, cmp := range targetResources {
		comp.Spec.Resources = append(comp.Spec.Resources, *cmp)
	}
	return &UnstructuredWithMetadata{
		Object:   ToSanitizedUnstructured(&comp),
		Metadata: o.Metadata,
	}, isConverted, nil
}

func (pg *PlanGenerator) isGVKSkipped(sourceGVK schema.GroupVersionKind) bool {
	for _, gvk := range pg.SkipGVKs {
		if (len(gvk.Group) == 0 || gvk.Group == sourceGVK.Group) &&
			(len(gvk.Version) == 0 || gvk.Version == sourceGVK.Version) &&
			(len(gvk.Kind) == 0 || gvk.Kind == sourceGVK.Kind) {
			return true
		}
	}
	return false
}

func (pg *PlanGenerator) setDefaultsOnTargetTemplate(sourceName *string, sourceNameUsed *bool, gvkSource, gvkTarget schema.GroupVersionKind, target *xpv1.ComposedTemplate, patchSets []xpv1.PatchSet, convertedPS []string) error {
	if pg.isGVKSkipped(gvkSource) {
		return nil
	}
	// remove invalid patches that do not conform to the migration target's schema
	if err := pg.removeInvalidPatches(gvkSource, gvkTarget, patchSets, target, convertedPS); err != nil {
		return errors.Wrap(err, "failed to set the defaults on the migration target composed template")
	}
	if *sourceNameUsed || gvkSource.Kind != gvkTarget.Kind {
		if sourceName != nil && len(*sourceName) > 0 {
			targetName := fmt.Sprintf("%s-%s", *sourceName, rand.String(5))
			target.Name = &targetName
		}
	} else {
		*sourceNameUsed = true
	}
	return nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
