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
)

func (pg *PlanGenerator) stepBackupAllResources() {
	pg.stepBackupManagedResources()
	pg.stepBackupCompositeResources()
	pg.stepBackupClaims()
}

func (pg *PlanGenerator) stepBackupManagedResources() {
	s := pg.stepConfiguration(stepBackupMRs)
	s.Exec.Args = []string{"-c", "kubectl get managed -o yaml > backup/managed-resources.yaml"}
}

func (pg *PlanGenerator) stepBackupCompositeResources() {
	s := pg.stepConfiguration(stepBackupComposites)
	s.Exec.Args = []string{"-c", "kubectl get composite -o yaml > backup/composite-resources.yaml"}
}

func (pg *PlanGenerator) stepBackupClaims() {
	s := pg.stepConfiguration(stepBackupClaims)
	s.Exec.Args = []string{"-c", "kubectl get claim --all-namespaces -o yaml > backup/claim-resources.yaml"}
}

func (pg *PlanGenerator) stepCheckHealthOfNewProvider(source UnstructuredWithMetadata, targets []*UnstructuredWithMetadata) error {
	for _, t := range targets {
		var s *Step
		isFamilyConfig, err := checkContainsFamilyConfigProvider(targets)
		if err != nil {
			return errors.Wrapf(err, "could not decide whether the provider family config")
		}
		if isFamilyConfig {
			s = pg.stepConfigurationWithSubStep(stepCheckHealthFamilyProvider, true)
		} else {
			s = pg.stepConfigurationWithSubStep(stepCheckHealthNewServiceScopedProvider, true)
		}
		s.Exec.Args = []string{"-c", fmt.Sprintf("kubectl wait provider.pkg %s --for condition=Healthy", t.Object.GetName())}
		t.Object.Object = addGVK(source.Object, t.Object.Object)
		t.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(t.Object))
	}
	return nil
}

func (pg *PlanGenerator) stepCheckInstallationOfNewProvider(source UnstructuredWithMetadata, targets []*UnstructuredWithMetadata) error {
	for _, t := range targets {
		var s *Step
		isFamilyConfig, err := checkContainsFamilyConfigProvider(targets)
		if err != nil {
			return errors.Wrapf(err, "could not decide whether the provider family config")
		}
		if isFamilyConfig {
			s = pg.stepConfigurationWithSubStep(stepCheckInstallationFamilyProviderRevision, true)
		} else {
			s = pg.stepConfigurationWithSubStep(stepCheckInstallationServiceScopedProviderRevision, true)
		}
		s.Exec.Args = []string{"-c", fmt.Sprintf("kubectl wait provider.pkg %s --for condition=Installed", t.Object.GetName())}
		t.Object.Object = addGVK(source.Object, t.Object.Object)
		t.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(t.Object))
	}
	return nil
}

func (pg *PlanGenerator) stepBuildConfiguration() {
	s := pg.stepConfiguration(stepBuildConfiguration)
	s.Exec.Args = []string{"-c", "up xpkg build --package-root={{PKG_ROOT}} --examples-root={{EXAMPLES_ROOT}} -o {{PKG_PATH}}"}
}

func (pg *PlanGenerator) stepPushConfiguration() {
	s := pg.stepConfiguration(stepPushConfiguration)
	s.Exec.Args = []string{"-c", "up xpkg push {{TARGET_CONFIGURATION_PACKAGE}} -f {{PKG_PATH}}"}
}
