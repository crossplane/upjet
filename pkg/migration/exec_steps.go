package migration

import (
	"fmt"
	"github.com/pkg/errors"
)

func (pg *PlanGenerator) stepBackupAllResources() error {
	if err := pg.stepBackupManagedResources(); err != nil {
		return errors.Wrap(err, "cannot get backup of managed resources")
	}
	if err := pg.stepBackupCompositeResources(); err != nil {
		return errors.Wrap(err, "cannot get backup of composite resources")
	}
	if err := pg.stepBackupClaims(); err != nil {
		return errors.Wrap(err, "cannot get backup of claims")
	}
	return nil
}

func (pg *PlanGenerator) stepBackupManagedResources() error {
	s := pg.stepConfiguration(stepBackupMRs)
	s.Exec.Args = []string{"-c", "'kubectl get managed -o yaml > backup/managed-resources.yaml'"}
	if err := execCommand(pg, s); err != nil {
		return err
	}
	return nil
}

func (pg *PlanGenerator) stepBackupCompositeResources() error {
	s := pg.stepConfiguration(stepBackupComposites)
	s.Exec.Args = []string{"-c", "'kubectl get composite -o yaml > backup/composite-resources.yaml'"}
	if err := execCommand(pg, s); err != nil {
		return err
	}
	return nil
}

func (pg *PlanGenerator) stepBackupClaims() error {
	s := pg.stepConfiguration(stepBackupClaims)
	s.Exec.Args = []string{"-c", "'kubectl get claim --all-namespaces -o yaml > backup/claim-resources.yaml'"}
	if err := execCommand(pg, s); err != nil {
		return err
	}
	return nil
}

func (pg *PlanGenerator) stepCheckHealthOfNewProvider(source UnstructuredWithMetadata, targets []*UnstructuredWithMetadata) error {
	for _, t := range targets {
		s := pg.stepConfigurationWithSubStep(stepCheckHealthNewServiceScopedProvider, true)
		s.Exec.Args = []string{"-c", fmt.Sprintf("'kubectl wait provider.pkg %s --for condition=Healthy'", t.Object.GetName())}
		t.Object.Object = addGVK(source.Object, t.Object.Object)
		t.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(t.Object))
		if err := execCommand(pg, s); err != nil {
			return err
		}
	}
	return nil
}

func (pg *PlanGenerator) stepCheckInstallationOfNewProvider(source UnstructuredWithMetadata, targets []*UnstructuredWithMetadata) error {
	for _, t := range targets {
		s := pg.stepConfigurationWithSubStep(stepCheckInstallationServiceScopedProviderRevision, true)
		s.Exec.Args = []string{"-c", fmt.Sprintf("'kubectl wait provider.pkg %s --for condition=Installed'", t.Object.GetName())}
		t.Object.Object = addGVK(source.Object, t.Object.Object)
		t.Metadata.Path = fmt.Sprintf("%s/%s.yaml", s.Name, getVersionedName(t.Object))
		if err := execCommand(pg, s); err != nil {
			return err
		}
	}
	return nil
}

func (pg *PlanGenerator) stepBuildConfiguration() error {
	s := pg.stepConfiguration(stepBuildConfiguration)
	s.Exec.Args = []string{"-c", "'up xpkg build --name test-smaller-provider-migration.xpkg --package-root=package --examples-root=examples'"}
	if err := execCommand(pg, s); err != nil {
		return err
	}
	return nil
}

func (pg *PlanGenerator) stepPushConfiguration() error {
	s := pg.stepConfiguration(stepPushConfiguration)
	s.Exec.Args = []string{"-c", "'up xpkg push ${ORG}/${PLATFORM}:${TAG} -f package/test-smaller-provider-migration.xpkg'"}
	if err := execCommand(pg, s); err != nil {
		return err
	}
	return nil
}

func execCommand(pg *PlanGenerator, s *Step) error {
	if _, err := pg.forkExecutor.Step(*s, nil); err != nil {
		return errors.Wrap(err, "command cannot executed successfully")
	}
	return nil
}
