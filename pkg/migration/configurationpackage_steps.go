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

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	errSetDeletionPolicyFmt        = "failed to put the patch file to set the deletion policy to %q: %s"
	errEditConfigurationPackageFmt = `failed to put the edited Configuration package: %s`
)

func (pg *PlanGenerator) convertConfigurationPackage(o UnstructuredWithMetadata) error {
	pkg, err := toConfigurationPackageV1(o.Object)
	if err != nil {
		return err
	}

	// add step for disabling the dependency resolution
	// for the configuration package
	s := pg.stepConfiguration(stepConfigurationPackageDisableDepResolution)
	p := fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(o.Object))
	s.Patch.Files = append(s.Patch.Files, p)
	if err := pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(o.Object, map[string]any{
				"spec": map[string]any{
					"skipDependencyResolution": true,
				},
			}),
		},
		Metadata: Metadata{
			Path: p,
		},
	}); err != nil {
		return err
	}

	// add step for enabling the dependency resolution
	// for the configuration package
	s = pg.stepConfiguration(stepConfigurationPackageEnableDepResolution)
	p = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(o.Object))
	s.Patch.Files = append(s.Patch.Files, p)
	if err := pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(o.Object, map[string]any{
				"spec": map[string]any{
					"skipDependencyResolution": false,
				},
			}),
		},
		Metadata: Metadata{
			Path: p,
		},
	}); err != nil {
		return err
	}

	// add the step for editing the configuration package
	for _, pkgConv := range pg.registry.configurationPackageConverters {
		if pkgConv.re == nil || pkgConv.converter == nil || !pkgConv.re.MatchString(pkg.Spec.Package) {
			continue
		}
		err := pkgConv.converter.ConfigurationPackageV1(pkg)
		if err != nil {
			return errors.Wrapf(err, "failed to call converter on Configuration package: %s", pkg.Spec.Package)
		}
		// TODO: if a converter only converts a specific version,
		// (or does not convert the given configuration),
		// we will have a false positive. Better to compute and check
		// a diff here.
		target := &UnstructuredWithMetadata{
			Object:   ToSanitizedUnstructured(pkg),
			Metadata: o.Metadata,
		}
		if err := pg.stepEditConfigurationPackage(o, target); err != nil {
			return err
		}
	}
	return nil
}

func (pg *PlanGenerator) stepEditConfigurationPackage(source UnstructuredWithMetadata, t *UnstructuredWithMetadata) error {
	if !pg.stepEnabled(stepEditConfigurationPackage) {
		return nil
	}
	s := pg.stepConfigurationWithSubStep(stepEditConfigurationPackage, true)
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

func (pg *PlanGenerator) stepOrhanMR(u UnstructuredWithMetadata) (bool, error) {
	return pg.stepSetDeletionPolicy(u, stepOrphanMRs, v1.DeletionOrphan, true)
}

func (pg *PlanGenerator) stepRevertOrhanMR(u UnstructuredWithMetadata) (bool, error) {
	return pg.stepSetDeletionPolicy(u, stepRevertOrphanMRs, v1.DeletionDelete, false)
}

func (pg *PlanGenerator) stepSetDeletionPolicy(u UnstructuredWithMetadata, step step, policy v1.DeletionPolicy, checkCurrentPolicy bool) (bool, error) {
	if !pg.stepEnabled(step) {
		return false, nil
	}
	pv := fieldpath.Pave(u.Object.Object)
	p, err := pv.GetString("spec.deletionPolicy")
	if err != nil && !fieldpath.IsNotFound(err) {
		return false, errors.Wrapf(err, "failed to get the current deletion policy from MR: %s", u.Object.GetName())
	}
	if checkCurrentPolicy && err == nil && v1.DeletionPolicy(p) == policy {
		return false, nil
	}
	s := pg.stepConfiguration(step)
	u.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(u.Object))
	s.Patch.Files = append(s.Patch.Files, u.Metadata.Path)
	return true, errors.Wrapf(pg.target.Put(UnstructuredWithMetadata{
		Object: unstructured.Unstructured{
			Object: addNameGVK(u.Object, map[string]any{
				"spec": map[string]any{
					"deletionPolicy": string(policy),
				},
			}),
		},
		Metadata: u.Metadata,
	}), errSetDeletionPolicyFmt, policy, u.Object.GetName())
}
