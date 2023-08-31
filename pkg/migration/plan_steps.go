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
	"sort"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/rand"
)

type step int

const (
	errMarshalSourceForPatch = "failed to marshal source object for computing JSON merge patch"
	errMarshalTargetForPatch = "failed to marshal target object for computing JSON merge patch"
	errMergePatch            = "failed to compute the JSON merge patch document"
	errMergePatchMap         = "failed to unmarshal the JSON merge patch document into map"
	errInvalidStepFmt        = "invalid step ID: %d"
)

func setApplyStep(name string, s *Step) {
	s.Name = name
	s.Type = StepTypeApply
	s.Apply = &ApplyStep{}
}

func setPatchStep(name string, s *Step) {
	s.Name = name
	s.Type = StepTypePatch
	s.Patch = &PatchStep{}
	s.Patch.Type = PatchTypeMerge
}

func setDeleteStep(name string, s *Step) {
	s.Name = name
	s.Type = StepTypeDelete
	deletePolicy := FinalizerPolicyRemove
	s.Delete = &DeleteStep{
		Options: &DeleteOptions{
			FinalizerPolicy: &deletePolicy,
		},
	}
}

func setExecStep(name string, s *Step) {
	s.Name = name
	s.Type = StepTypeExec
	s.Exec = &ExecStep{
		Command: "sh",
	}
}

func (pg *PlanGenerator) commitSteps() {
	if len(pg.Plan.Spec.stepMap) == 0 {
		return
	}
	pg.Plan.Spec.Steps = make([]Step, 0, len(pg.Plan.Spec.stepMap))
	keys := make([]string, 0, len(pg.Plan.Spec.stepMap))
	for s := range pg.Plan.Spec.stepMap {
		keys = append(keys, s)
	}
	sort.Strings(keys)

	addManualExecution := true
	switch t := pg.source.(type) {
	case *sources:
		for _, source := range t.backends {
			if _, ok := source.(*FileSystemSource); ok {
				addManualExecution = false
				break
			}
		}
	case *FileSystemSource:
		addManualExecution = false
	}

	if addManualExecution {
		for _, s := range keys {
			AddManualExecution(pg.Plan.Spec.stepMap[s])
			pg.Plan.Spec.Steps = append(pg.Plan.Spec.Steps, *pg.Plan.Spec.stepMap[s])
		}
	} else {
		for _, s := range keys {
			pg.Plan.Spec.Steps = append(pg.Plan.Spec.Steps, *pg.Plan.Spec.stepMap[s])
		}
	}
}

// AddManualExecution sets the manual execution hint for
// the specified step.
func AddManualExecution(s *Step) {
	switch s.Type {
	case StepTypeExec:
		s.ManualExecution = []string{fmt.Sprintf("%s %s %q", s.Exec.Command, s.Exec.Args[0], strings.Join(s.Exec.Args[1:], " "))}
	case StepTypePatch:
		for _, f := range s.Patch.Files {
			s.ManualExecution = append(s.ManualExecution, fmt.Sprintf("kubectl patch --type='%s' -f %s --patch-file %s", s.Patch.Type, f, f))
		}
	case StepTypeApply:
		for _, f := range s.Apply.Files {
			s.ManualExecution = append(s.ManualExecution, fmt.Sprintf("kubectl apply -f %s", f))
		}
	case StepTypeDelete:
		for _, r := range s.Delete.Resources {
			s.ManualExecution = append(s.ManualExecution, fmt.Sprintf("kubectl delete %s %s", strings.Join([]string{r.Kind, r.Group}, "."), r.Name))
		}
	}
}

func (pg *PlanGenerator) stepEnabled(s step) bool {
	for _, i := range pg.enabledSteps {
		if i == s {
			return true
		}
	}
	return false
}

func computeJSONMergePathDoc(source, target unstructured.Unstructured) (map[string]any, error) {
	sourceBuff, err := source.MarshalJSON()
	if err != nil {
		return nil, errors.Wrap(err, errMarshalSourceForPatch)
	}
	targetBuff, err := target.MarshalJSON()
	if err != nil {
		return nil, errors.Wrap(err, errMarshalTargetForPatch)
	}
	patch, err := jsonmergepatch.CreateThreeWayJSONMergePatch(sourceBuff, targetBuff, sourceBuff)
	if err != nil {
		return nil, errors.Wrap(err, errMergePatch)
	}

	var result map[string]any
	return result, errors.Wrap(json.Unmarshal(patch, &result), errMergePatchMap)
}

func getQualifiedName(u unstructured.Unstructured) string {
	namePrefix := u.GetName()
	if len(namePrefix) == 0 {
		namePrefix = fmt.Sprintf("%s%s", u.GetGenerateName(), rand.String(5))
	}
	gvk := u.GroupVersionKind()
	return fmt.Sprintf("%s.%ss.%s", namePrefix, strings.ToLower(gvk.Kind), gvk.Group)
}

func getVersionedName(u unstructured.Unstructured) string {
	v := u.GroupVersionKind().Version
	qName := getQualifiedName(u)
	if v == "" {
		return qName
	}
	return fmt.Sprintf("%s_%s", qName, v)
}
