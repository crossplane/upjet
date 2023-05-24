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

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// configuration migration steps follow any existing API migration steps
	stepEditConfigurations = iota + stepAPIEnd + 1
)

const (
	errConfigurationOutput = "failed to output configuration JSON merge document"
)

func (pg *PlanGenerator) stepConfiguration(s step) *Step {
	if pg.Plan.Spec.stepMap[s] != nil {
		return pg.Plan.Spec.stepMap[s]
	}

	pg.Plan.Spec.stepMap[s] = &Step{}
	switch s { // nolint:gocritic,exhaustive
	case stepEditConfigurations:
		setPatchStep("edit-configurations", pg.Plan.Spec.stepMap[s])
	default:
		panic(fmt.Sprintf(errInvalidStepFmt, s))
	}
	return pg.Plan.Spec.stepMap[s]
}

func (pg *PlanGenerator) stepEditConfiguration(source unstructured.Unstructured, target *UnstructuredWithMetadata, vName string) error {
	target.Metadata.Path = fmt.Sprintf("%s/%s.yaml", pg.stepConfiguration(stepEditConfigurations).Name, vName)
	pg.stepConfiguration(stepEditConfigurations).Patch.Files = append(pg.stepConfiguration(stepEditConfigurations).Patch.Files, target.Metadata.Path)
	patchMap, err := computeJSONMergePathDoc(source, target.Object)
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
