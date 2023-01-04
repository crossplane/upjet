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
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/claim"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	xpv1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
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
	errDeletionOrphan          = "failed to set deletion policy to Orphan"
	errCompositionOutput       = "failed to output migrated composition"
	errCompositeOutput         = "failed to output migrated composite"
	errClaimOutput             = "failed to output migrated claim"
	errClaimsEdit              = "failed to edit claims"
	errPlanGeneration          = "failed to generate the migration plan"
	errPause                   = "failed to store a paused manifest"
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
)

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
}

// NewPlanGenerator constructs a new PlanGenerator using the specified
// Source and Target and the default converter Registry.
func NewPlanGenerator(registry *Registry, source Source, target Target) PlanGenerator {
	return PlanGenerator{
		source:   source,
		target:   target,
		registry: registry,
	}
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

			targets, converted, err := pg.convertResource(o)
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

func (pg *PlanGenerator) convertResource(o UnstructuredWithMetadata) ([]UnstructuredWithMetadata, bool, error) {
	gvk := o.Object.GroupVersionKind()
	conv := pg.registry.converters[gvk]
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
	converted := make([]UnstructuredWithMetadata, 0, len(resources))
	for _, mg := range resources {
		converted = append(converted, UnstructuredWithMetadata{
			Object:   ToSanitizedUnstructured(mg),
			Metadata: o.Metadata,
		})
	}
	return converted, true, nil
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
	c, err := convertToComposition(o.Object.Object)
	if err != nil {
		return nil, false, errors.Wrap(err, errUnstructuredConvert)
	}
	targetPatchSets := make([]xpv1.PatchSet, 0, len(c.Spec.PatchSets))
	for _, ps := range c.Spec.PatchSets {
		targetPatchSets = append(targetPatchSets, *ps.DeepCopy())
	}
	var targetResources []*xpv1.ComposedTemplate
	isConverted := false
	for _, cmp := range c.Spec.Resources {
		u, err := FromRawExtension(cmp.Base)
		if err != nil {
			return nil, false, errors.Wrap(err, errCompositionMigrate)
		}
		gvk := u.GroupVersionKind()
		converted, ok, err := pg.convertResource(UnstructuredWithMetadata{
			Object:   u,
			Metadata: o.Metadata,
		})
		if err != nil {
			return nil, false, errors.Wrap(err, errComposedTemplateBase)
		}
		isConverted = isConverted || ok
		cmps := make([]*xpv1.ComposedTemplate, 0, len(converted))
		for _, u := range converted {
			buff, err := u.Object.MarshalJSON()
			if err != nil {
				return nil, false, errors.Wrap(err, errUnstructuredMarshal)
			}
			c := cmp.DeepCopy()
			c.Base = runtime.RawExtension{
				Raw: buff,
			}
			cmps = append(cmps, c)
		}
		conv := pg.registry.converters[gvk]
		if conv != nil {
			ps, err := conv.Composition(targetPatchSets, cmp, cmps...)
			if err != nil {
				return nil, false, errors.Wrap(err, errComposedTemplateMigrate)
			}
			targetPatchSets = ps
		}
		targetResources = append(targetResources, cmps...)
	}
	c.Spec.PatchSets = targetPatchSets
	c.Spec.Resources = make([]xpv1.ComposedTemplate, 0, len(targetResources))
	for _, cmp := range targetResources {
		c.Spec.Resources = append(c.Spec.Resources, *cmp)
	}
	return &UnstructuredWithMetadata{
		Object:   ToSanitizedUnstructured(&c),
		Metadata: o.Metadata,
	}, isConverted, nil
}

// NOTE: to cover different migration scenarios, we may use
// "migration templates" instead of a static plan. But a static plan should be
// fine as a start.
func (pg *PlanGenerator) buildPlan() {
	pg.Plan.Spec.Steps = make([]Step, 10)

	pg.Plan.Spec.Steps[stepPauseManaged].Name = "pause-managed"
	pg.Plan.Spec.Steps[stepPauseManaged].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepPauseManaged].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepPauseComposites].Name = "pause-composites"
	pg.Plan.Spec.Steps[stepPauseComposites].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepPauseComposites].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepCreateNewManaged].Name = "create-new-managed"
	pg.Plan.Spec.Steps[stepCreateNewManaged].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepCreateNewManaged].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepNewCompositions].Name = "new-compositions"
	pg.Plan.Spec.Steps[stepNewCompositions].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepNewCompositions].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepEditComposites].Name = "edit-composites"
	pg.Plan.Spec.Steps[stepEditComposites].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepEditComposites].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepEditClaims].Name = "edit-claims"
	pg.Plan.Spec.Steps[stepEditClaims].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepEditClaims].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Name = "deletion-policy-orphan"
	pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepDeleteOldManaged].Name = "delete-old-managed"
	pg.Plan.Spec.Steps[stepDeleteOldManaged].Type = StepTypeDelete
	deletePolicy := FinalizerPolicyRemove
	pg.Plan.Spec.Steps[stepDeleteOldManaged].Delete = &DeleteStep{
		Options: &DeleteOptions{
			FinalizerPolicy: &deletePolicy,
		},
	}

	pg.Plan.Spec.Steps[stepStartManaged].Name = "start-managed"
	pg.Plan.Spec.Steps[stepStartManaged].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepStartManaged].Apply = &ApplyStep{}

	pg.Plan.Spec.Steps[stepStartComposites].Name = "start-composites"
	pg.Plan.Spec.Steps[stepStartComposites].Type = StepTypeApply
	pg.Plan.Spec.Steps[stepStartComposites].Apply = &ApplyStep{}
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
	annot := u.Object.GetAnnotations()
	if annot != nil {
		delete(annot, meta.AnnotationKeyReconciliationPaused)
		u.Object.SetAnnotations(annot)
	}

	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepStartManaged].Name, getQualifiedName(u.Object))
	pg.Plan.Spec.Steps[stepStartManaged].Apply.Files = append(pg.Plan.Spec.Steps[stepStartManaged].Apply.Files, u.Metadata.Path)
	return errors.Wrap(pg.target.Put(*u), errResourceOutput)
}

func (pg *PlanGenerator) stepPauseManagedResource(u *UnstructuredWithMetadata, qName string) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepPauseManaged].Name, qName)
	pg.Plan.Spec.Steps[stepPauseManaged].Apply.Files = append(pg.Plan.Spec.Steps[stepPauseManaged].Apply.Files, u.Metadata.Path)
	return pg.pause(u.Metadata.Path, &u.Object)
}

func (pg *PlanGenerator) stepPauseComposite(u *UnstructuredWithMetadata) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepPauseComposites].Name, getQualifiedName(u.Object))
	pg.Plan.Spec.Steps[stepPauseComposites].Apply.Files = append(pg.Plan.Spec.Steps[stepPauseComposites].Apply.Files, u.Metadata.Path)
	return pg.pause(u.Metadata.Path, &u.Object)
}

func (pg *PlanGenerator) stepOrphanManagedResource(u *UnstructuredWithMetadata, qName string) error {
	pv := fieldpath.Pave(u.Object.Object)
	if err := pv.SetValue("spec.deletionPolicy", v1.DeletionOrphan); err != nil {
		return errors.Wrap(err, errDeletionOrphan)
	}
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Name, qName)
	pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Apply.Files = append(pg.Plan.Spec.Steps[stepDeletionPolicyOrphan].Apply.Files, u.Metadata.Path)
	return errors.Wrap(pg.target.Put(*u), errResourceOrphan)
}

func (pg *PlanGenerator) stepDeleteOldManagedResource(u *UnstructuredWithMetadata) {
	pg.Plan.Spec.Steps[stepDeleteOldManaged].Delete.Resources = append(pg.Plan.Spec.Steps[stepDeleteOldManaged].Delete.Resources,
		Resource{
			GroupVersionKind: FromGroupVersionKind(u.Object.GroupVersionKind()),
			Name:             u.Object.GetName(),
		})
}

func (pg *PlanGenerator) pause(fp string, u *unstructured.Unstructured) error {
	meta.AddAnnotations(u, map[string]string{meta.AnnotationKeyReconciliationPaused: "true"})
	return errors.Wrap(pg.target.Put(UnstructuredWithMetadata{
		Object: *u,
		Metadata: Metadata{
			Path: fp,
		},
	}), errPause)
}

func getQualifiedName(u unstructured.Unstructured) string {
	gvk := u.GroupVersionKind()
	return fmt.Sprintf("%s.%ss.%s", u.GetName(), strings.ToLower(gvk.Kind), gvk.Group)
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
		annot := u.Object.GetAnnotations()
		delete(annot, meta.AnnotationKeyReconciliationPaused)
		u.Object.SetAnnotations(annot)
		u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepStartComposites].Name, getQualifiedName(u.Object))
		pg.Plan.Spec.Steps[stepStartComposites].Apply.Files = append(pg.Plan.Spec.Steps[stepStartComposites].Apply.Files, u.Metadata.Path)
		if err := pg.target.Put(u); err != nil {
			return errors.Wrap(err, errCompositeOutput)
		}
	}
	return nil
}

func (pg *PlanGenerator) stepEditComposites(composites []UnstructuredWithMetadata, convertedMap map[corev1.ObjectReference][]UnstructuredWithMetadata, convertedComposition map[string]string) error {
	for i, u := range composites {
		cp := composite.Unstructured{Unstructured: u.Object}
		refs := cp.GetResourceReferences()
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
		if ref := cp.GetCompositionReference(); ref != nil && convertedComposition[ref.Name] != "" {
			ref.Name = convertedComposition[ref.Name]
			cp.SetCompositionReference(ref)
		}
		u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.Plan.Spec.Steps[stepEditComposites].Name, getQualifiedName(u.Object))
		pg.Plan.Spec.Steps[stepEditComposites].Apply.Files = append(pg.Plan.Spec.Steps[stepEditComposites].Apply.Files, u.Metadata.Path)
		if err := pg.target.Put(u); err != nil {
			return errors.Wrap(err, errCompositeOutput)
		}
		composites[i] = u
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
		pg.Plan.Spec.Steps[stepEditClaims].Apply.Files = append(pg.Plan.Spec.Steps[stepEditClaims].Apply.Files, u.Metadata.Path)
		if err := pg.target.Put(u); err != nil {
			return errors.Wrap(err, errClaimOutput)
		}
	}
	return nil
}
