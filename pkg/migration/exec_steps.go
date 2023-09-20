// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

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
