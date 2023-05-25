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

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// configuration migration steps follow any existing API migration steps
	stepNewServiceScopedProvider = iota + stepAPIEnd + 1
	stepPatchSkipDependencyResolution
	stepEditPackageLock
	stepDeleteMonolithicProvider
	stepActivateServiceScopedProviderRevision
	stepEditConfigurations
)

const (
	errConfigurationOutput = "failed to output configuration JSON merge document"
)

func (pg *PlanGenerator) stepConfiguration(s step) *Step {
	return pg.stepConfigurationWithSubStep(s, false)
}

func (pg *PlanGenerator) configurationSubStep(s step) string {
	ss := -1
	subStep := pg.subSteps[s]
	if subStep != "" {
		s, err := strconv.Atoi(subStep)
		if err == nil {
			ss = s
		}
	}
	pg.subSteps[s] = strconv.Itoa(ss + 1)
	return pg.subSteps[s]
}

func (pg *PlanGenerator) stepConfigurationWithSubStep(s step, newSubStep bool) *Step {
	stepKey := strconv.Itoa(int(s))
	if newSubStep {
		stepKey = fmt.Sprintf("%s.%s", stepKey, pg.configurationSubStep(s))
	}
	if pg.Plan.Spec.stepMap[stepKey] != nil {
		return pg.Plan.Spec.stepMap[stepKey]
	}

	pg.Plan.Spec.stepMap[stepKey] = &Step{}
	switch s { // nolint:gocritic,exhaustive
	case stepNewServiceScopedProvider:
		setApplyStep("new-ssop", pg.Plan.Spec.stepMap[stepKey])
	case stepPatchSkipDependencyResolution:
		setPatchStep("skip-dependency-resolution", pg.Plan.Spec.stepMap[stepKey])
	case stepEditPackageLock:
		setPatchStep("edit-package-lock", pg.Plan.Spec.stepMap[stepKey])
	case stepDeleteMonolithicProvider:
		setDeleteStep("delete-monolithic-provider", pg.Plan.Spec.stepMap[stepKey])
	case stepActivateServiceScopedProviderRevision:
		setPatchStep("activate-ssop", pg.Plan.Spec.stepMap[stepKey])
	case stepEditConfigurations:
		setPatchStep("edit-configurations", pg.Plan.Spec.stepMap[stepKey])
	default:
		panic(fmt.Sprintf(errInvalidStepFmt, s))
	}
	return pg.Plan.Spec.stepMap[stepKey]
}

func (pg *PlanGenerator) stepEditConfiguration(source UnstructuredWithMetadata, target *UnstructuredWithMetadata) error {
	// set spec.SkipDependencyResolution: true for the configuration
	s := pg.stepConfiguration(stepPatchSkipDependencyResolution)
	source.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(source.Object))
	s.Patch.Files = append(s.Patch.Files, source.Metadata.Path)
	if err := pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(source.Object, map[string]any{
				"spec": map[string]any{
					"skipDependencyResolution": true,
				},
			}),
		},
		Metadata: source.Metadata,
	}); err != nil {
		return errors.Wrapf(err, errEditMonolithFmt, source.Metadata.Path)
	}

	s = pg.stepConfiguration(stepEditConfigurations)
	target.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(target.Object))
	s.Patch.Files = append(s.Patch.Files, target.Metadata.Path)
	patchMap, err := computeJSONMergePathDoc(source.Object, target.Object)
	if err != nil {
		return err
	}
	return errors.Wrap(pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(target.Object, patchMap),
		},
		Metadata: target.Metadata,
	}), errConfigurationOutput)
}
