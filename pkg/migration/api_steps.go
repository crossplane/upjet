// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"fmt"
	"strconv"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/claim"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
)

const (
	stepPauseManaged step = iota
	stepPauseComposites
	stepCreateNewManaged
	stepNewCompositions
	stepEditComposites
	stepEditClaims
	stepDeletionPolicyOrphan
	stepRemoveFinalizers
	stepDeleteOldManaged
	stepStartManaged
	stepStartComposites
	// this must be the last step
	stepAPIEnd
)

func getAPIMigrationSteps() []step {
	steps := make([]step, 0, stepAPIEnd)
	for i := step(0); i < stepAPIEnd; i++ {
		steps = append(steps, i)
	}
	return steps
}

func getAPIMigrationStepsFileSystemMode() []step {
	return []step{
		stepCreateNewManaged,
		stepNewCompositions,
		stepEditComposites,
		stepEditClaims,
		stepStartManaged,
		stepStartComposites,
		// this must be the last step
		stepAPIEnd,
	}
}

func (pg *PlanGenerator) addStepsForManagedResource(u *UnstructuredWithMetadata) error {
	if u.Metadata.Category != CategoryManaged {
		if _, ok, err := toManagedResource(pg.registry.scheme, u.Object); err != nil || !ok {
			// not a managed resource or unable to determine
			// whether it's a managed resource
			return nil //nolint:nilerr
		}
	}
	qName := getQualifiedName(u.Object)
	if err := pg.stepPauseManagedResource(u, qName); err != nil {
		return err
	}
	if err := pg.stepOrphanManagedResource(u, qName); err != nil {
		return err
	}
	if err := pg.stepRemoveFinalizersManagedResource(u, qName); err != nil {
		return err
	}
	pg.stepDeleteOldManagedResource(u)
	orphaned, err := pg.stepOrhanMR(*u)
	if err != nil {
		return err
	}
	if !orphaned {
		return nil
	}
	_, err = pg.stepRevertOrhanMR(*u)
	return err
}

func (pg *PlanGenerator) stepStartManagedResource(u *UnstructuredWithMetadata) error {
	if !pg.stepEnabled(stepStartManaged) {
		return nil
	}
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepStartManaged).Name, getQualifiedName(u.Object))
	pg.stepAPI(stepStartManaged).Patch.Files = append(pg.stepAPI(stepStartManaged).Patch.Files, u.Metadata.Path)
	return pg.pause(*u, false)
}

func (pg *PlanGenerator) stepPauseManagedResource(u *UnstructuredWithMetadata, qName string) error {
	if !pg.stepEnabled(stepPauseManaged) {
		return nil
	}
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepPauseManaged).Name, qName)
	pg.stepAPI(stepPauseManaged).Patch.Files = append(pg.stepAPI(stepPauseManaged).Patch.Files, u.Metadata.Path)
	return pg.pause(*u, true)
}

func (pg *PlanGenerator) stepPauseComposite(u *UnstructuredWithMetadata) error {
	if !pg.stepEnabled(stepPauseComposites) {
		return nil
	}
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepPauseComposites).Name, getQualifiedName(u.Object))
	pg.stepAPI(stepPauseComposites).Patch.Files = append(pg.stepAPI(stepPauseComposites).Patch.Files, u.Metadata.Path)
	return pg.pause(*u, true)
}

func (pg *PlanGenerator) stepOrphanManagedResource(u *UnstructuredWithMetadata, qName string) error {
	if !pg.stepEnabled(stepDeletionPolicyOrphan) {
		return nil
	}
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepDeletionPolicyOrphan).Name, qName)
	pg.stepAPI(stepDeletionPolicyOrphan).Patch.Files = append(pg.stepAPI(stepDeletionPolicyOrphan).Patch.Files, u.Metadata.Path)
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

func (pg *PlanGenerator) stepRemoveFinalizersManagedResource(u *UnstructuredWithMetadata, qName string) error {
	if !pg.stepEnabled(stepRemoveFinalizers) {
		return nil
	}
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepRemoveFinalizers).Name, qName)
	pg.stepAPI(stepRemoveFinalizers).Patch.Files = append(pg.stepAPI(stepRemoveFinalizers).Patch.Files, u.Metadata.Path)
	return pg.removeFinalizers(*u)
}

func (pg *PlanGenerator) stepDeleteOldManagedResource(u *UnstructuredWithMetadata) {
	if !pg.stepEnabled(stepDeleteOldManaged) {
		return
	}
	pg.stepAPI(stepDeleteOldManaged).Delete.Resources = append(pg.stepAPI(stepDeleteOldManaged).Delete.Resources,
		Resource{
			GroupVersionKind: FromGroupVersionKind(u.Object.GroupVersionKind()),
			Name:             u.Object.GetName(),
		})
}

func (pg *PlanGenerator) stepNewManagedResource(u *UnstructuredWithMetadata) error {
	if !pg.stepEnabled(stepCreateNewManaged) {
		return nil
	}
	meta.AddAnnotations(&u.Object, map[string]string{meta.AnnotationKeyReconciliationPaused: "true"})
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepCreateNewManaged).Name, getQualifiedName(u.Object))
	pg.stepAPI(stepCreateNewManaged).Apply.Files = append(pg.stepAPI(stepCreateNewManaged).Apply.Files, u.Metadata.Path)
	if err := pg.target.Put(*u); err != nil {
		return errors.Wrap(err, errResourceOutput)
	}
	return nil
}

func (pg *PlanGenerator) stepNewComposition(u *UnstructuredWithMetadata) error {
	if !pg.stepEnabled(stepNewCompositions) {
		return nil
	}
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepNewCompositions).Name, getQualifiedName(u.Object))
	pg.stepAPI(stepNewCompositions).Apply.Files = append(pg.stepAPI(stepNewCompositions).Apply.Files, u.Metadata.Path)
	if err := pg.target.Put(*u); err != nil {
		return errors.Wrap(err, errCompositionOutput)
	}
	return nil
}

func (pg *PlanGenerator) stepStartComposites(composites []UnstructuredWithMetadata) error {
	if !pg.stepEnabled(stepStartComposites) {
		return nil
	}
	for _, u := range composites {
		u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepStartComposites).Name, getQualifiedName(u.Object))
		pg.stepAPI(stepStartComposites).Patch.Files = append(pg.stepAPI(stepStartComposites).Patch.Files, u.Metadata.Path)
		if err := pg.pause(u, false); err != nil {
			return errors.Wrap(err, errCompositeOutput)
		}
	}
	return nil
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

func (pg *PlanGenerator) removeFinalizers(u UnstructuredWithMetadata) error {
	return errors.Wrap(pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(u.Object, map[string]any{
				"metadata": map[string]any{
					"finalizers": []any{},
				},
			}),
		},
		Metadata: Metadata{
			Path: u.Metadata.Path,
		},
	}), errResourceRemoveFinalizer)
}

func (pg *PlanGenerator) stepEditComposites(composites []UnstructuredWithMetadata, convertedMap map[corev1.ObjectReference][]UnstructuredWithMetadata, convertedComposition map[string]string) error {
	if !pg.stepEnabled(stepEditComposites) {
		return nil
	}
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
		u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepEditComposites).Name, getQualifiedName(u.Object))
		pg.stepAPI(stepEditComposites).Patch.Files = append(pg.stepAPI(stepEditComposites).Patch.Files, u.Metadata.Path)
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
	if !pg.stepEnabled(stepEditClaims) {
		return nil
	}
	for _, u := range claims {
		cm := claim.Unstructured{Unstructured: u.Object}
		if ref := cm.GetCompositionReference(); ref != nil && convertedComposition[ref.Name] != "" {
			ref.Name = convertedComposition[ref.Name]
			cm.SetCompositionReference(ref)
		}
		u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepEditClaims).Name, getQualifiedName(u.Object))
		pg.stepAPI(stepEditClaims).Patch.Files = append(pg.stepAPI(stepEditClaims).Patch.Files, u.Metadata.Path)
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

// NOTE: to cover different migration scenarios, we may use
// "migration templates" instead of a static plan. But a static plan should be
// fine as a start.
func (pg *PlanGenerator) stepAPI(s step) *Step { //nolint:gocyclo // all steps under a single clause for readability
	stepKey := strconv.Itoa(int(s))
	if pg.Plan.Spec.stepMap[stepKey] != nil {
		return pg.Plan.Spec.stepMap[stepKey]
	}

	pg.Plan.Spec.stepMap[stepKey] = &Step{}
	switch s { //nolint:exhaustive
	case stepPauseManaged:
		setPatchStep("pause-managed", pg.Plan.Spec.stepMap[stepKey])

	case stepPauseComposites:
		setPatchStep("pause-composites", pg.Plan.Spec.stepMap[stepKey])

	case stepCreateNewManaged:
		setApplyStep("create-new-managed", pg.Plan.Spec.stepMap[stepKey])

	case stepNewCompositions:
		setApplyStep("new-compositions", pg.Plan.Spec.stepMap[stepKey])

	case stepEditComposites:
		setPatchStep("edit-composites", pg.Plan.Spec.stepMap[stepKey])

	case stepEditClaims:
		setPatchStep("edit-claims", pg.Plan.Spec.stepMap[stepKey])

	case stepDeletionPolicyOrphan:
		setPatchStep("deletion-policy-orphan", pg.Plan.Spec.stepMap[stepKey])

	case stepRemoveFinalizers:
		setPatchStep("remove-finalizers", pg.Plan.Spec.stepMap[stepKey])

	case stepDeleteOldManaged:
		setDeleteStep("delete-old-managed", pg.Plan.Spec.stepMap[stepKey])

	case stepStartManaged:
		setPatchStep("start-managed", pg.Plan.Spec.stepMap[stepKey])

	case stepStartComposites:
		setPatchStep("start-composites", pg.Plan.Spec.stepMap[stepKey])
	default:
		panic(fmt.Sprintf(errInvalidStepFmt, s))
	}
	return pg.Plan.Spec.stepMap[stepKey]
}
