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
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	errPutSSOPPackageFmt = "failed to put the SSOP package: %s"
	errActivateSSOP      = "failed to put the activated SSOP package: %s"
)

func (pg *PlanGenerator) convertProviderPackage(o UnstructuredWithMetadata) (bool, error) { //nolint:gocyclo
	pkg, err := toProviderPackage(o.Object)
	if err != nil {
		return false, err
	}
	isConverted := false
	for _, pkgConv := range pg.registry.providerPackageConverters {
		if pkgConv.re == nil || pkgConv.converter == nil || !pkgConv.re.MatchString(pkg.Spec.Package) {
			continue
		}
		targetPkgs, err := pkgConv.converter.ProviderPackageV1(*pkg)
		if err != nil {
			return false, errors.Wrapf(err, "failed to call converter on Provider package: %s", pkg.Spec.Package)
		}
		if len(targetPkgs) == 0 {
			continue
		}
		// TODO: if a configuration converter only converts a specific version,
		// (or does not convert the given configuration),
		// we will have a false positive. Better to compute and check
		// a diff here.
		isConverted = true
		converted := make([]*UnstructuredWithMetadata, 0, len(targetPkgs))
		for _, p := range targetPkgs {
			p := p
			converted = append(converted, &UnstructuredWithMetadata{
				Object:   ToSanitizedUnstructured(&p),
				Metadata: o.Metadata,
			})
		}
		if err := pg.stepNewSSOPs(o, converted); err != nil {
			return false, err
		}
		if err := pg.stepActivateSSOPs(converted); err != nil {
			return false, err
		}
		if err := pg.stepCheckHealthOfNewProvider(o, converted); err != nil {
			return false, err
		}
		if err := pg.stepCheckInstallationOfNewProvider(o, converted); err != nil {
			return false, err
		}
	}
	return isConverted, nil
}

func (pg *PlanGenerator) stepDeleteMonolith(source UnstructuredWithMetadata) error {
	// delete the monolithic provider package
	s := pg.stepConfigurationWithSubStep(stepDeleteMonolithicProvider, true)
	source.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(source.Object))
	s.Description = fmt.Sprintf("%s: %s", s.Description, source.Object.GetName())
	s.Delete.Resources = []Resource{
		{
			GroupVersionKind: FromGroupVersionKind(source.Object.GroupVersionKind()),
			Name:             source.Object.GetName(),
		},
	}
	return nil
}

// add steps for the new SSOPs
func (pg *PlanGenerator) stepNewSSOPs(source UnstructuredWithMetadata, targets []*UnstructuredWithMetadata) error {
	var s *Step
	isFamilyConfig, err := checkContainsFamilyConfigProvider(targets)
	if err != nil {
		return errors.Wrapf(err, "could not decide whether the provider family config")
	}
	if isFamilyConfig {
		s = pg.stepConfigurationWithSubStep(stepNewFamilyProvider, true)
	} else {
		s = pg.stepConfigurationWithSubStep(stepNewServiceScopedProvider, true)
	}
	for _, t := range targets {
		t.Object.Object = addGVK(source.Object, t.Object.Object)
		t.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(t.Object))
		s.Apply.Files = append(s.Apply.Files, t.Metadata.Path)
		if err := pg.target.Put(*t); err != nil {
			return errors.Wrapf(err, errPutSSOPPackageFmt, t.Metadata.Path)
		}
	}
	return nil
}

// add steps for activating SSOPs
func (pg *PlanGenerator) stepActivateSSOPs(targets []*UnstructuredWithMetadata) error {
	var s *Step
	isFamilyConfig, err := checkContainsFamilyConfigProvider(targets)
	if err != nil {
		return errors.Wrapf(err, "could not decide whether the provider family config")
	}
	if isFamilyConfig {
		s = pg.stepConfigurationWithSubStep(stepActivateFamilyProviderRevision, true)
	} else {
		s = pg.stepConfigurationWithSubStep(stepActivateServiceScopedProviderRevision, true)
	}
	for _, t := range targets {
		t.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(t.Object))
		s.Patch.Files = append(s.Patch.Files, t.Metadata.Path)
		if err := pg.target.Put(UnstructuredWithMetadata{
			Object: unstructured.Unstructured{
				Object: addNameGVK(t.Object, map[string]any{
					"spec": map[string]any{
						"revisionActivationPolicy": "Automatic",
					},
				}),
			},
			Metadata: t.Metadata,
		}); err != nil {
			return errors.Wrapf(err, errActivateSSOP, t.Metadata.Path)
		}
	}
	return nil
}

func checkContainsFamilyConfigProvider(targets []*UnstructuredWithMetadata) (bool, error) {
	for _, t := range targets {
		paved := fieldpath.Pave(t.Object.Object)
		pkg, err := paved.GetString("spec.package")
		if err != nil {
			return false, errors.Wrap(err, "could not get package of provider")
		}
		if strings.Contains(pkg, "provider-family") {
			return true, nil
		}
	}
	return false, nil
}
