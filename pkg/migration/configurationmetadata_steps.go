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

	xpmetav1 "github.com/crossplane/crossplane/apis/pkg/meta/v1"
	xpmetav1alpha1 "github.com/crossplane/crossplane/apis/pkg/meta/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// configuration migration steps follow any existing API migration steps
	stepOrphanMRs = iota + stepAPIEnd + 1
	stepNewServiceScopedProvider
	stepConfigurationPackageDisableDepResolution
	stepEditPackageLock
	stepDeleteMonolithicProvider
	stepActivateServiceScopedProviderRevision
	stepEditConfigurationMetadata
	stepEditConfigurationPackage
	stepConfigurationPackageEnableDepResolution
	stepRevertOrphanMRs
	stepConfigurationEnd
)

func getConfigurationMigrationSteps() []step {
	steps := make([]step, 0, stepConfigurationEnd-stepAPIEnd-1)
	for i := stepAPIEnd + 1; i < stepConfigurationEnd; i++ {
		steps = append(steps, i)
	}
	return steps
}

const (
	errConfigurationMetadataOutput = "failed to output configuration JSON merge document"
)

func (pg *PlanGenerator) convertConfigurationMetadata(o UnstructuredWithMetadata) error {
	isConverted := false
	var conf metav1.Object
	var err error
	for _, confConv := range pg.registry.configurationMetaConverters {
		if confConv.re == nil || confConv.converter == nil || !confConv.re.MatchString(o.Object.GetName()) {
			continue
		}

		conf, err = toConfigurationMetadata(o.Object)
		if err != nil {
			return err
		}
		switch o.Object.GroupVersionKind().Version {
		case "v1alpha1":
			err = confConv.converter.ConfigurationMetadataV1Alpha1(conf.(*xpmetav1alpha1.Configuration))
		default:
			err = confConv.converter.ConfigurationMetadataV1(conf.(*xpmetav1.Configuration))
		}
		if err != nil {
			return errors.Wrapf(err, "failed to call converter on Configuration: %s", conf.GetName())
		}
		// TODO: if a configuration converter only converts a specific version,
		// (or does not convert the given configuration),
		// we will have a false positive. Better to compute and check
		// a diff here.
		isConverted = true
	}
	if !isConverted {
		return nil
	}
	return pg.stepEditConfigurationMetadata(o, &UnstructuredWithMetadata{
		Object:   ToSanitizedUnstructured(conf),
		Metadata: o.Metadata,
	})
}

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

func (pg *PlanGenerator) stepConfigurationWithSubStep(s step, newSubStep bool) *Step { // nolint:gocyclo // easy to follow all steps here
	stepKey := strconv.Itoa(int(s))
	if newSubStep {
		stepKey = fmt.Sprintf("%s.%s", stepKey, pg.configurationSubStep(s))
	}
	if pg.Plan.Spec.stepMap[stepKey] != nil {
		return pg.Plan.Spec.stepMap[stepKey]
	}

	pg.Plan.Spec.stepMap[stepKey] = &Step{}
	switch s { // nolint:gocritic,exhaustive
	case stepOrphanMRs:
		setPatchStep("deletion-policy-orphan", pg.Plan.Spec.stepMap[stepKey])
	case stepRevertOrphanMRs:
		setPatchStep("deletion-policy-delete", pg.Plan.Spec.stepMap[stepKey])
	case stepNewServiceScopedProvider:
		setApplyStep("new-ssop", pg.Plan.Spec.stepMap[stepKey])
	case stepConfigurationPackageDisableDepResolution:
		setPatchStep("disable-dependency-resolution", pg.Plan.Spec.stepMap[stepKey])
	case stepConfigurationPackageEnableDepResolution:
		setPatchStep("enable-dependency-resolution", pg.Plan.Spec.stepMap[stepKey])
	case stepEditConfigurationPackage:
		setPatchStep("edit-configuration-package", pg.Plan.Spec.stepMap[stepKey])
	case stepEditPackageLock:
		setPatchStep("edit-package-lock", pg.Plan.Spec.stepMap[stepKey])
	case stepDeleteMonolithicProvider:
		setDeleteStep("delete-monolithic-provider", pg.Plan.Spec.stepMap[stepKey])
	case stepActivateServiceScopedProviderRevision:
		setPatchStep("activate-ssop", pg.Plan.Spec.stepMap[stepKey])
	case stepEditConfigurationMetadata:
		setPatchStep("edit-configuration-metadata", pg.Plan.Spec.stepMap[stepKey])
	default:
		panic(fmt.Sprintf(errInvalidStepFmt, s))
	}
	return pg.Plan.Spec.stepMap[stepKey]
}

func (pg *PlanGenerator) stepEditConfigurationMetadata(source UnstructuredWithMetadata, target *UnstructuredWithMetadata) error {
	s := pg.stepConfiguration(stepEditConfigurationMetadata)
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
	}), errConfigurationMetadataOutput)
}
