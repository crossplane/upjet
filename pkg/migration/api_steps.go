// Copyright 2023 Upbound Inc.
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
	"strconv"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/claim"
	"github.com/crossplane/crossplane-runtime/pkg/resource/unstructured/composite"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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
	// this must be the last step
	stepAPIEnd
)

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
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepStartManaged).Name, getQualifiedName(u.Object))
	pg.stepAPI(stepStartManaged).Patch.Files = append(pg.stepAPI(stepStartManaged).Patch.Files, u.Metadata.Path)
	return pg.pause(*u, false)
}

func (pg *PlanGenerator) stepPauseManagedResource(u *UnstructuredWithMetadata, qName string) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepPauseManaged).Name, qName)
	pg.stepAPI(stepPauseManaged).Patch.Files = append(pg.stepAPI(stepPauseManaged).Patch.Files, u.Metadata.Path)
	return pg.pause(*u, true)
}

func (pg *PlanGenerator) stepPauseComposite(u *UnstructuredWithMetadata) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepPauseComposites).Name, getQualifiedName(u.Object))
	pg.stepAPI(stepPauseComposites).Patch.Files = append(pg.stepAPI(stepPauseComposites).Patch.Files, u.Metadata.Path)
	return pg.pause(*u, true)
}

func (pg *PlanGenerator) stepOrphanManagedResource(u *UnstructuredWithMetadata, qName string) error {
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

func (pg *PlanGenerator) stepDeleteOldManagedResource(u *UnstructuredWithMetadata) {
	pg.stepAPI(stepDeleteOldManaged).Delete.Resources = append(pg.stepAPI(stepDeleteOldManaged).Delete.Resources,
		Resource{
			GroupVersionKind: FromGroupVersionKind(u.Object.GroupVersionKind()),
			Name:             u.Object.GetName(),
		})
}

func (pg *PlanGenerator) stepNewManagedResource(u *UnstructuredWithMetadata) error {
	meta.AddAnnotations(&u.Object, map[string]string{meta.AnnotationKeyReconciliationPaused: "true"})
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepCreateNewManaged).Name, getQualifiedName(u.Object))
	pg.stepAPI(stepCreateNewManaged).Apply.Files = append(pg.stepAPI(stepCreateNewManaged).Apply.Files, u.Metadata.Path)
	if err := pg.target.Put(*u); err != nil {
		return errors.Wrap(err, errResourceOutput)
	}
	return nil
}

func (pg *PlanGenerator) stepNewComposition(u *UnstructuredWithMetadata) error {
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepAPI(stepNewCompositions).Name, getQualifiedName(u.Object))
	pg.stepAPI(stepNewCompositions).Apply.Files = append(pg.stepAPI(stepNewCompositions).Apply.Files, u.Metadata.Path)
	if err := pg.target.Put(*u); err != nil {
		return errors.Wrap(err, errCompositionOutput)
	}
	return nil
}

func (pg *PlanGenerator) stepStartComposites(composites []UnstructuredWithMetadata) error {
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
func (pg *PlanGenerator) stepAPI(s step) *Step { // nolint:gocyclo // all steps under a single clause for readability
	if pg.Plan.Spec.stepMap[s] != nil {
		return pg.Plan.Spec.stepMap[s]
	}

	pg.Plan.Spec.stepMap[s] = &Step{}
	switch s { // nolint:exhaustive
	case stepPauseManaged:
		setPatchStep("pause-managed", pg.Plan.Spec.stepMap[s])

	case stepPauseComposites:
		setPatchStep("pause-composites", pg.Plan.Spec.stepMap[s])

	case stepCreateNewManaged:
		setApplyStep("create-new-managed", pg.Plan.Spec.stepMap[s])

	case stepNewCompositions:
		setApplyStep("new-compositions", pg.Plan.Spec.stepMap[s])

	case stepEditComposites:
		setPatchStep("edit-composites", pg.Plan.Spec.stepMap[s])

	case stepEditClaims:
		setPatchStep("edit-claims", pg.Plan.Spec.stepMap[s])

	case stepDeletionPolicyOrphan:
		setPatchStep("deletion-policy-orphan", pg.Plan.Spec.stepMap[s])

	case stepDeleteOldManaged:
		pg.Plan.Spec.stepMap[s].Name = "delete-old-managed"
		pg.Plan.Spec.stepMap[s].Type = StepTypeDelete
		deletePolicy := FinalizerPolicyRemove
		pg.Plan.Spec.stepMap[s].Delete = &DeleteStep{
			Options: &DeleteOptions{
				FinalizerPolicy: &deletePolicy,
			},
		}

	case stepStartManaged:
		setPatchStep("start-managed", pg.Plan.Spec.stepMap[s])

	case stepStartComposites:
		setPatchStep("start-composites", pg.Plan.Spec.stepMap[s])
	default:
		panic(fmt.Sprintf(errInvalidStepFmt, s))
	}
	return pg.Plan.Spec.stepMap[s]
}
