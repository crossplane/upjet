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
	"strconv"
	"strings"
	"time"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/claim"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	xpv1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
)

const (
	errSourceHasNext           = "failed to generate migration plan: Could not check next object from source"
	errSourceNext              = "failed to generate migration plan: Could not get next object from source"
	errUnstructuredConvert     = "failed to convert from unstructured object to v1.Composition"
	errUnstructuredMarshal     = "failed to marshal unstructured object to JSON"
	errResourceMigrate         = "failed to migrate resource"
	errCompositePause          = "failed to pause composite resource"
	errCompositesEdit          = "failed to edit composite resources"
	errCompositesStart         = "failed to start composite resources"
	errCompositionMigrate      = "failed to migrate the composition"
	errComposedTemplateBase    = "failed to migrate the base of a composed template"
	errComposedTemplateMigrate = "failed to migrate the composed templates of the composition"
	errResourceOutput          = "failed to output migrated resource"
	errResourceOrphan          = "failed to orphan managed resource"
	errCompositionOutput       = "failed to output migrated composition"
	errCompositeOutput         = "failed to output migrated composite"
	errClaimOutput             = "failed to output migrated claim"
	errClaimsEdit              = "failed to edit claims"
	errPlanGeneration          = "failed to generate the migration plan"
	errPause                   = "failed to store a paused manifest"
	errMissingGVK              = "managed resource is missing its GVK. Resource converters must set GVKs on any managed resources they newly generate."
)

type step int

const (
	stepPauseManaged step = iota
	stepPauseComposites
	stepCreateNewManaged
	stepNewCompositions
	stepEditComposites
	stepEditClaims
	stepDeletionPolicyOrphan
	stepDeleteOldManaged
	stepStartManaged
	stepStartComposites
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

// PlanGenerator generates a migration.Plan reading the manifests available
// from `source`, converting managed resources and compositions using the
// available `migration.Converter`s registered in the `registry` and
// writing the output manifests to the specified `target`.
type PlanGenerator struct {
	source   Source
	target   Target
	registry *Registry
	// Plan is the migration.Plan whose steps are expected
	// to complete a migration when they're executed in order.
	Plan Plan
	// ErrorOnInvalidPatchSchema errors and stops plan generation in case
	// an error is encountered while checking the conformance of a patch
	// statement against the migration source or the migration target.
	ErrorOnInvalidPatchSchema bool
}

// NewPlanGenerator constructs a new PlanGenerator using the specified
// Source and Target and the default converter Registry.
func NewPlanGenerator(registry *Registry, source Source, target Target, opts ...PlanGeneratorOption) PlanGenerator {
	pg := &PlanGenerator{
		source:   source,
		target:   target,
		registry: registry,
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
	pg.buildPlan()
	return errors.Wrap(pg.convert(), errPlanGeneration)
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
		c, err := convertToComposition(o.Object.Object)
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
		switch gvk := o.Object.GroupVersionKind(); gvk {
		case xpv1.CompositionGroupVersionKind:
			target, converted, err := pg.convertComposition(o)
			if err != nil {
				return errors.Wrap(err, errCompositionMigrate)
			}
			if converted {
				migratedName := fmt.Sprintf("%s-migrated", o.Object.GetName())
				convertedComposition[o.Object.GetName()] = migratedName
				target.Object.SetName(migratedName)
				if err := pg.stepNewComposition(target); err != nil {
					return errors.Wrap(err, errCompositionMigrate)
				}
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

func toManagedResource(c runtime.ObjectCreater, u unstructured.Unstructured) (resource.Managed, bool, error) {
	gvk := u.GroupVersionKind()
	if gvk == xpv1.CompositionGroupVersionKind {
		return nil, false, nil
	}
	obj, err := c.New(gvk)
	if err != nil {
		return nil, false, errors.Wrapf(err, errFmtNewObject, gvk)
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, obj); err != nil {
		return nil, false, errors.Wrap(err, errFromUnstructured)
	}
	mg, ok := obj.(resource.Managed)
	return mg, ok, nil
}

func (pg *PlanGenerator) convertComposition(o UnstructuredWithMetadata) (*UnstructuredWithMetadata, bool, error) { // nolint:gocyclo
	convertedPS, err := pg.convertPatchSets(o)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to convert patch sets")
	}
	comp, err := convertToComposition(o.Object.Object)
	if err != nil {
		return nil, false, errors.Wrap(err, errUnstructuredConvert)
	}
	var targetResources []*xpv1.ComposedTemplate
	isConverted := false
	for _, cmp := range comp.Spec.Resources {
		u, err := FromRawExtension(cmp.Base)
		if err != nil {
			return nil, false, errors.Wrap(err, errCompositionMigrate)
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

func (pg *PlanGenerator) setDefaultsOnTargetTemplate(sourceName *string, sourceNameUsed *bool, gvkSource, gvkTarget schema.GroupVersionKind, target *xpv1.ComposedTemplate, patchSets []xpv1.PatchSet, convertedPS []string) error {
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

// NOTE: to cover different migration scenarios, we may use
// "migration templates" instead of a static plan. But a static plan should be
// fine as a start.
func (pg *PlanGenerator) buildPlan() {
	pg.Plan.Spec.Steps = make([]Step, 10)

	pg.Plan.Spec.Steps[stepPauseManaged].Name = "pause-managed"
	pg.Plan.Spec.Steps[stepPauseManaged].Type = StepTypePatch
	pg.Plan.Spec.Steps[stepPauseManaged].Patch = &PatchStep{}
	pg.Plan.Spec.Steps[stepPauseManaged].Patch.Type = PatchTypeMerge

	pg.Plan.Spec.Steps[stepPauseComposites].Name = "pause-composites"
	pg.Plan.Spec.Steps[stepPauseComposites].Type = StepTypePatch
	pg.Plan.Spec.Steps[stepPauseComposites].Patch = &PatchStep{}
	pg.Plan.Spec.Steps[stepPauseComposites].Patch.Type = PatchTypeMerge

	pg.Plan.Spec.Steps[stepCreateNewManaged].Name = "create-new-managed"
	pg.Plan.Spec.Steps[stepCreateNewManaged].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepCreateNewManaged].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepNewCompositions].Name = "new-compositions"
	pg.Plan.Spec.Steps[stepNewCompositions].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepNewCompositions].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepEditComposites].Name = "edit-composites"
	pg.Plan.Spec.Steps[stepEditComposites].Type = StepTypePatch
	pg.Plan.Spec.Steps[stepEditComposites].Patch = &PatchStep{}
	pg.Plan.Spec.Steps[stepEditComposites].Patch.Type = PatchTypeMerge

	pg.Plan.Spec.Steps[stepEditClaims].Name = "edit-claims"
	pg.Plan.Spec.Steps[stepEditClaims].Type = StepTypePatch
	pg.Plan.Spec.Steps[stepEditClaims].Patch = &PatchStep{}
	pg.Plan.Spec.Steps[stepEditClaims].Patch.Type = PatchTypeMerge

	pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Name = "deletion-policy-orphan"
	pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Type = StepTypePatch
	pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Patch = &PatchStep{}
	pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Patch.Type = PatchTypeMerge

	pg.Plan.Spec.Steps[stepDeleteOldManaged].Name = "delete-old-managed"
	pg.Plan.Spec.Steps[stepDeleteOldManaged].Type = StepTypeDelete
	deletePolicy := FinalizerPolicyRemove
	pg.Plan.Spec.Steps[stepDeleteOldManaged].Delete = &DeleteStep{
		Options: &DeleteOptions{
			FinalizerPolicy: &deletePolicy,
		},
	}

	pg.Plan.Spec.Steps[stepStartManaged].Name = "start-managed"
	pg.Plan.Spec.Steps[stepStartManaged].Type = StepTypePatch
	pg.Plan.Spec.Steps[stepStartManaged].Patch = &PatchStep{}
	pg.Plan.Spec.Steps[stepStartManaged].Patch.Type = PatchTypeMerge

	pg.Plan.Spec.Steps[stepStartComposites].Name = "start-composites"
	pg.Plan.Spec.Steps[stepStartComposites].Type = StepTypePatch
	pg.Plan.Spec.Steps[stepStartComposites].Patch = &PatchStep{}
	pg.Plan.Spec.Steps[stepStartComposites].Patch.Type = PatchTypeMerge
	pg.Plan.Version = versionV010
}

func (pg *PlanGenerator) addStepsForManagedResource(u *UnstructuredWithMetadata) error {
	if _, ok, err := toManagedResource(pg.registry.scheme, u.Object); err != nil || !ok {
		// not a managed resource or unable to determine
		// whether it's a managed resource
		return nil // nolint:nilerr
	}
	qName := getQualifiedName(u.Object)
	if err := pg.stepPauseManagedResource(u, qName); err != nil {
		return err
	}
	if err := pg.stepOrphanManagedResource(u, qName); err != nil {
		return err
	}
	pg.stepDeleteOldManagedResource(u)
	return nil
}

func (pg *PlanGenerator) stepStartManagedResource(u *UnstructuredWithMetadata) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepStartManaged].Name, getQualifiedName(u.Object))
	pg.Plan.Spec.Steps[stepStartManaged].Patch.Files = append(pg.Plan.Spec.Steps[stepStartManaged].Patch.Files, u.Metadata.Path)
	return pg.pause(*u, false)
}

func (pg *PlanGenerator) stepPauseManagedResource(u *UnstructuredWithMetadata, qName string) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepPauseManaged].Name, qName)
	pg.Plan.Spec.Steps[stepPauseManaged].Patch.Files = append(pg.Plan.Spec.Steps[stepPauseManaged].Patch.Files, u.Metadata.Path)
	return pg.pause(*u, true)
}

func (pg *PlanGenerator) stepPauseComposite(u *UnstructuredWithMetadata) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepPauseComposites].Name, getQualifiedName(u.Object))
	pg.Plan.Spec.Steps[stepPauseComposites].Patch.Files = append(pg.Plan.Spec.Steps[stepPauseComposites].Patch.Files, u.Metadata.Path)
	return pg.pause(*u, true)
}

func (pg *PlanGenerator) stepOrphanManagedResource(u *UnstructuredWithMetadata, qName string) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Name, qName)
	pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Patch.Files = append(pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Patch.Files, u.Metadata.Path)
	return errors.Wrap(pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(u.Object, map[string]any{
				"spec": map[string]any{
					"deletionPolicy": string(v1.DeletionOrphan),
				},
			}),
		},
		Metadata: u.Metadata,
	}), errResourceOrphan)
}

func (pg *PlanGenerator) stepDeleteOldManagedResource(u *UnstructuredWithMetadata) {
	pg.Plan.Spec.Steps[stepDeleteOldManaged].Delete.Resources = append(pg.Plan.Spec.Steps[stepDeleteOldManaged].Delete.Resources,
		Resource{
			GroupVersionKind: FromGroupVersionKind(u.Object.GroupVersionKind()),
			Name:             u.Object.GetName(),
		})
}

func (pg *PlanGenerator) pause(u UnstructuredWithMetadata, isPaused bool) error {
	return errors.Wrap(pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(u.Object, map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						meta.AnnotationKeyReconciliationPaused: strconv.FormatBool(isPaused),
					},
				},
			}),
		},
		Metadata: Metadata{
			Path: u.Metadata.Path,
		},
	}), errPause)
}

func getQualifiedName(u unstructured.Unstructured) string {
	namePrefix := u.GetName()
	if len(namePrefix) == 0 {
		namePrefix = fmt.Sprintf("%s%s", u.GetGenerateName(), rand.String(5))
	}
	gvk := u.GroupVersionKind()
	return fmt.Sprintf("%s.%ss.%s", namePrefix, strings.ToLower(gvk.Kind), gvk.Group)
}

func (pg *PlanGenerator) stepNewManagedResource(u *UnstructuredWithMetadata) error {
	meta.AddAnnotations(&u.Object, map[string]string{meta.AnnotationKeyReconciliationPaused: "true"})
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepCreateNewManaged].Name, getQualifiedName(u.Object))
	pg.Plan.Spec.Steps[stepCreateNewManaged].Apply.Files = append(pg.Plan.Spec.Steps[stepCreateNewManaged].Apply.Files, u.Metadata.Path)
	if err := pg.target.Put(*u); err != nil {
		return errors.Wrap(err, errResourceOutput)
	}
	return nil
}

func (pg *PlanGenerator) stepNewComposition(u *UnstructuredWithMetadata) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepNewCompositions].Name, getQualifiedName(u.Object))
	pg.Plan.Spec.Steps[stepNewCompositions].Apply.Files = append(pg.Plan.Spec.Steps[stepNewCompositions].Apply.Files, u.Metadata.Path)
	if err := pg.target.Put(*u); err != nil {
		return errors.Wrap(err, errCompositionOutput)
	}
	return nil
}

func (pg *PlanGenerator) stepStartComposites(composites []UnstructuredWithMetadata) error {
	for _, u := range composites {
		u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepStartComposites].Name, getQualifiedName(u.Object))
		pg.Plan.Spec.Steps[stepStartComposites].Patch.Files = append(pg.Plan.Spec.Steps[stepStartComposites].Patch.Files, u.Metadata.Path)
		if err := pg.pause(u, false); err != nil {
			return errors.Wrap(err, errCompositeOutput)
		}
	}
	return nil
}

func (pg *PlanGenerator) stepEditComposites(composites []UnstructuredWithMetadata, convertedMap map[corev1.ObjectReference][]UnstructuredWithMetadata, convertedComposition map[string]string) error {
	for _, u := range composites {
		cp := composite.Unstructured{Unstructured: u.Object}
		refs := cp.GetResourceReferences()
		// compute new spec.resourceRefs so that the XR references the new MRs
		newRefs := make([]corev1.ObjectReference, 0, len(refs))
		for _, ref := range refs {
			converted, ok := convertedMap[ref]
			if !ok {
				newRefs = append(newRefs, ref)
				continue
			}
			for _, o := range converted {
				gvk := o.Object.GroupVersionKind()
				newRefs = append(newRefs, corev1.ObjectReference{
					Kind:       gvk.Kind,
					Name:       o.Object.GetName(),
					APIVersion: gvk.GroupVersion().String(),
				})
			}
		}
		cp.SetResourceReferences(newRefs)
		// compute new spec.compositionRef
		if ref := cp.GetCompositionReference(); ref != nil && convertedComposition[ref.Name] != "" {
			ref.Name = convertedComposition[ref.Name]
			cp.SetCompositionReference(ref)
		}
		spec := u.Object.Object["spec"].(map[string]any)
		u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepEditComposites].Name, getQualifiedName(u.Object))
		pg.Plan.Spec.Steps[stepEditComposites].Patch.Files = append(pg.Plan.Spec.Steps[stepEditComposites].Patch.Files, u.Metadata.Path)
		if err := pg.target.Put(UnstructuredWithMetadata{
			Object: unstructured.Unstructured{
				Object: addNameGVK(u.Object, map[string]any{
					"spec": map[string]any{
						keyResourceRefs:   spec[keyResourceRefs],
						keyCompositionRef: spec[keyCompositionRef]},
				}),
			},
			Metadata: u.Metadata,
		}); err != nil {
			return errors.Wrap(err, errCompositeOutput)
		}
	}
	return nil
}

func (pg *PlanGenerator) stepEditClaims(claims []UnstructuredWithMetadata, convertedComposition map[string]string) error {
	for _, u := range claims {
		cm := claim.Unstructured{Unstructured: u.Object}
		if ref := cm.GetCompositionReference(); ref != nil && convertedComposition[ref.Name] != "" {
			ref.Name = convertedComposition[ref.Name]
			cm.SetCompositionReference(ref)
		}
		u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepEditClaims].Name, getQualifiedName(u.Object))
		pg.Plan.Spec.Steps[stepEditClaims].Patch.Files = append(pg.Plan.Spec.Steps[stepEditClaims].Patch.Files, u.Metadata.Path)
		if err := pg.target.Put(UnstructuredWithMetadata{
			Object: unstructured.Unstructured{
				Object: addNameGVK(u.Object, map[string]any{
					"spec": map[string]any{
						keyCompositionRef: u.Object.Object["spec"].(map[string]any)[keyCompositionRef],
					},
				}),
			},
			Metadata: u.Metadata,
		}); err != nil {
			return errors.Wrap(err, errClaimOutput)
		}
	}
	return nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
