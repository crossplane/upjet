// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

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
