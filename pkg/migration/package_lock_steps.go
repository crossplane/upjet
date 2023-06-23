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

func (pg *PlanGenerator) convertPackageLock(o UnstructuredWithMetadata) error {
	lock, err := toPackageLock(o.Object)
	if err != nil {
		return err
	}
	isConverted := false
	for _, lockConv := range pg.registry.packageLockConverters {
		if lockConv.re == nil || lockConv.converter == nil || !lockConv.re.MatchString(lock.GetName()) {
			continue
		}
		if err := lockConv.converter.PackageLockV1Beta1(lock); err != nil {
			return errors.Wrapf(err, "failed to call converter on package lock: %s", lock.GetName())
		}
		// TODO: if a lock converter does not convert the given lock,
		// we will have a false positive. Better to compute and check
		// a diff here.
		isConverted = true
	}
	if !isConverted {
		return nil
	}
	target := &UnstructuredWithMetadata{
		Object:   ToSanitizedUnstructured(lock),
		Metadata: o.Metadata,
	}
	if err := pg.stepEditPackageLock(o, target); err != nil {
		return err
	}
	return nil
}

func (pg *PlanGenerator) stepEditPackageLock(source UnstructuredWithMetadata, t *UnstructuredWithMetadata) error {
	// add step for editing the package lock
	s := pg.stepConfiguration(stepEditPackageLock)
	t.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(t.Object))
	s.Patch.Files = append(s.Patch.Files, t.Metadata.Path)
	s.ManualExecution = []string{fmt.Sprintf("kubectl patch %s %s --type='%s' --patch-file %s", getKindGroupName(t.Object), t.Object.GetName(), s.Patch.Type, t.Metadata.Path)}
	patchMap, err := computeJSONMergePathDoc(source.Object, t.Object)
	if err != nil {
		return err
	}
	return errors.Wrapf(pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(t.Object, patchMap),
		},
		Metadata: t.Metadata,
	}), errEditConfigurationPackageFmt, t.Object.GetName())
}
