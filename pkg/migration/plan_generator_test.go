// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	xpmetav1 "github.com/crossplane/crossplane/apis/pkg/meta/v1"
	xpmetav1alpha1 "github.com/crossplane/crossplane/apis/pkg/meta/v1alpha1"
	xppkgv1 "github.com/crossplane/crossplane/apis/pkg/v1"
	xppkgv1beta1 "github.com/crossplane/crossplane/apis/pkg/v1beta1"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/crossplane/upjet/pkg/migration/fake"
)

func TestGeneratePlan(t *testing.T) {
	type fields struct {
		source   Source
		target   *testTarget
		registry *Registry
		opts     []PlanGeneratorOption
	}
	type want struct {
		err               error
		migrationPlanPath string
		// names of resource files to be loaded
		migratedResourceNames []string
		preProcessResults     map[Category][]string
	}
	tests := map[string]struct {
		fields fields
		want   want
	}{
		"EmptyPlan": {
			fields: fields{
				source:   newTestSource(map[string]Metadata{}),
				target:   newTestTarget(),
				registry: getRegistry(),
			},
			want: want{},
		},
		"PreProcess": {
			fields: fields{
				source: newTestSource(map[string]Metadata{
					"testdata/plan/composition.yaml": {Category: CategoryComposition},
				}),
				target:   newTestTarget(),
				registry: getRegistry(withPreProcessor(CategoryComposition, &preProcessor{})),
			},
			want: want{
				preProcessResults: map[Category][]string{
					CategoryComposition: {"example.compositions.apiextensions.crossplane.io_v1"},
				},
			},
		},
		"PlanWithManagedResourceAndClaim": {
			fields: fields{
				source: newTestSource(map[string]Metadata{
					"testdata/plan/sourcevpc.yaml":   {Category: CategoryManaged},
					"testdata/plan/claim.yaml":       {Category: CategoryClaim},
					"testdata/plan/composition.yaml": {},
					"testdata/plan/xrd.yaml":         {},
					"testdata/plan/xr.yaml":          {Category: CategoryComposite}}),
				target: newTestTarget(),
				registry: getRegistry(
					withPreProcessor(CategoryManaged, &preProcessor{}),
					withDelegatingConverter(fake.MigrationSourceGVK, delegatingConverter{
						rFn: func(mg xpresource.Managed) ([]xpresource.Managed, error) {
							s := mg.(*fake.MigrationSourceObject)
							t := &fake.MigrationTargetObject{}
							if _, err := CopyInto(s, t, fake.MigrationTargetGVK, "spec.forProvider.tags", "mockManaged"); err != nil {
								return nil, err
							}
							t.Spec.ForProvider.Tags = make(map[string]string, len(s.Spec.ForProvider.Tags))
							for _, tag := range s.Spec.ForProvider.Tags {
								v := tag.Value
								t.Spec.ForProvider.Tags[tag.Key] = v
							}
							return []xpresource.Managed{
								t,
							}, nil
						},
						cmpFn: func(_ v1.ComposedTemplate, convertedTemplates ...*v1.ComposedTemplate) error {
							// convert patches in the migration target composed templates
							for i := range convertedTemplates {
								convertedTemplates[i].Patches = append([]v1.Patch{
									{FromFieldPath: ptrFromString("spec.parameters.tagValue"),
										ToFieldPath: ptrFromString(`spec.forProvider.tags["key1"]`),
									}, {
										FromFieldPath: ptrFromString("spec.parameters.tagValue"),
										ToFieldPath:   ptrFromString(`spec.forProvider.tags["key2"]`),
									},
								}, convertedTemplates[i].Patches...)
							}
							return nil
						},
					}),
					withPatchSetConverter(patchSetConverter{
						re:        AllCompositions,
						converter: &testConverter{},
					})),
			},
			want: want{
				migrationPlanPath: "testdata/plan/generated/migration_plan.yaml",
				migratedResourceNames: []string{
					"pause-managed/sample-vpc.vpcs.fakesourceapi.yaml",
					"edit-claims/my-resource.myresources.test.com.yaml",
					"start-managed/sample-vpc.vpcs.faketargetapi.yaml",
					"pause-composites/my-resource-dwjgh.xmyresources.test.com.yaml",
					"edit-composites/my-resource-dwjgh.xmyresources.test.com.yaml",
					"deletion-policy-orphan/sample-vpc.vpcs.fakesourceapi.yaml",
					"remove-finalizers/sample-vpc.vpcs.fakesourceapi.yaml",
					"new-compositions/example-migrated.compositions.apiextensions.crossplane.io.yaml",
					"start-composites/my-resource-dwjgh.xmyresources.test.com.yaml",
					"create-new-managed/sample-vpc.vpcs.faketargetapi.yaml",
				},
				preProcessResults: map[Category][]string{
					CategoryManaged: {"sample-vpc.vpcs.fakesourceapi_v1alpha1"},
				},
			},
		},
		"PlanWithManagedResourceAndClaimForFileSystemMode": {
			fields: fields{
				source: newTestSource(map[string]Metadata{
					"testdata/plan/sourcevpc.yaml":   {Category: CategoryManaged},
					"testdata/plan/claim.yaml":       {Category: CategoryClaim},
					"testdata/plan/composition.yaml": {},
					"testdata/plan/xrd.yaml":         {},
					"testdata/plan/xr.yaml":          {Category: CategoryComposite}}),
				target: newTestTarget(),
				registry: getRegistry(
					withPreProcessor(CategoryManaged, &preProcessor{}),
					withDelegatingConverter(fake.MigrationSourceGVK, delegatingConverter{
						rFn: func(mg xpresource.Managed) ([]xpresource.Managed, error) {
							s := mg.(*fake.MigrationSourceObject)
							t := &fake.MigrationTargetObject{}
							if _, err := CopyInto(s, t, fake.MigrationTargetGVK, "spec.forProvider.tags", "mockManaged"); err != nil {
								return nil, err
							}
							t.Spec.ForProvider.Tags = make(map[string]string, len(s.Spec.ForProvider.Tags))
							for _, tag := range s.Spec.ForProvider.Tags {
								v := tag.Value
								t.Spec.ForProvider.Tags[tag.Key] = v
							}
							return []xpresource.Managed{
								t,
							}, nil
						},
						cmpFn: func(_ v1.ComposedTemplate, convertedTemplates ...*v1.ComposedTemplate) error {
							// convert patches in the migration target composed templates
							for i := range convertedTemplates {
								convertedTemplates[i].Patches = append([]v1.Patch{
									{FromFieldPath: ptrFromString("spec.parameters.tagValue"),
										ToFieldPath: ptrFromString(`spec.forProvider.tags["key1"]`),
									}, {
										FromFieldPath: ptrFromString("spec.parameters.tagValue"),
										ToFieldPath:   ptrFromString(`spec.forProvider.tags["key2"]`),
									},
								}, convertedTemplates[i].Patches...)
							}
							return nil
						},
					}),
					withPatchSetConverter(patchSetConverter{
						re:        AllCompositions,
						converter: &testConverter{},
					})),
				opts: []PlanGeneratorOption{WithEnableOnlyFileSystemAPISteps()},
			},
			want: want{
				migrationPlanPath: "testdata/plan/generated/migration_plan_filesystem.yaml",
				migratedResourceNames: []string{
					"edit-claims/my-resource.myresources.test.com.yaml",
					"start-managed/sample-vpc.vpcs.faketargetapi.yaml",
					"edit-composites/my-resource-dwjgh.xmyresources.test.com.yaml",
					"new-compositions/example-migrated.compositions.apiextensions.crossplane.io.yaml",
					"start-composites/my-resource-dwjgh.xmyresources.test.com.yaml",
					"create-new-managed/sample-vpc.vpcs.faketargetapi.yaml",
				},
				preProcessResults: map[Category][]string{
					CategoryManaged: {"sample-vpc.vpcs.fakesourceapi_v1alpha1"},
				},
			},
		},
		"PlanWithConfigurationMetaV1": {
			fields: fields{
				source: newTestSource(map[string]Metadata{
					"testdata/plan/configurationv1.yaml": {}}),
				target: newTestTarget(),
				registry: getRegistry(
					withConfigurationMetadataConverter(configurationMetadataConverter{
						re:        AllConfigurations,
						converter: &configurationMetaTestConverter{},
					})),
				opts: []PlanGeneratorOption{WithEnableConfigurationMigrationSteps()},
			},
			want: want{
				migrationPlanPath: "testdata/plan/generated/configurationv1_migration_plan.yaml",
				migratedResourceNames: []string{
					"edit-configuration-metadata/platform-ref-aws.configurations.meta.pkg.crossplane.io_v1.yaml",
				},
			},
		},
		"PlanWithConfigurationMetaV1Alpha1": {
			fields: fields{
				source: newTestSource(map[string]Metadata{
					"testdata/plan/configurationv1alpha1.yaml": {}}),
				target: newTestTarget(),
				registry: getRegistry(
					withConfigurationMetadataConverter(configurationMetadataConverter{
						re:        AllConfigurations,
						converter: &configurationMetaTestConverter{},
					})),
				opts: []PlanGeneratorOption{WithEnableConfigurationMigrationSteps()},
			},
			want: want{
				migrationPlanPath: "testdata/plan/generated/configurationv1alpha1_migration_plan.yaml",
				migratedResourceNames: []string{
					"edit-configuration-metadata/platform-ref-aws.configurations.meta.pkg.crossplane.io_v1alpha1.yaml",
				},
			},
		},
		"PlanWithProviderPackageV1": {
			fields: fields{
				source: newTestSource(map[string]Metadata{
					"testdata/plan/providerv1.yaml": {}}),
				target: newTestTarget(),
				registry: getRegistry(
					withProviderPackageConverter(providerPackageConverter{
						re:        regexp.MustCompile(`xpkg.upbound.io/upbound/provider-aws:.+`),
						converter: &monolithProviderToFamilyConfigConverter{},
					}),
					withProviderPackageConverter(providerPackageConverter{
						re:        regexp.MustCompile(`xpkg.upbound.io/upbound/provider-aws:.+`),
						converter: &monolithicProviderToSSOPConverter{},
					})),
				opts: []PlanGeneratorOption{WithEnableConfigurationMigrationSteps()},
			},
			want: want{
				migrationPlanPath: "testdata/plan/generated/providerv1_migration_plan.yaml",
				migratedResourceNames: []string{
					"new-ssop/provider-family-aws.providers.pkg.crossplane.io_v1.yaml",
					"new-ssop/provider-aws-ec2.providers.pkg.crossplane.io_v1.yaml",
					"new-ssop/provider-aws-eks.providers.pkg.crossplane.io_v1.yaml",
					"activate-ssop/provider-family-aws.providers.pkg.crossplane.io_v1.yaml",
					"activate-ssop/provider-aws-ec2.providers.pkg.crossplane.io_v1.yaml",
					"activate-ssop/provider-aws-eks.providers.pkg.crossplane.io_v1.yaml",
				},
			},
		},
		"PlanForConfigurationPackageMigration": {
			fields: fields{
				source: newTestSource(map[string]Metadata{
					"testdata/plan/providerv1.yaml":         {},
					"testdata/plan/configurationv1.yaml":    {},
					"testdata/plan/configurationpkgv1.yaml": {},
					"testdata/plan/lockv1beta1.yaml":        {},
					"testdata/plan/sourcevpc.yaml":          {Category: CategoryManaged},
					"testdata/plan/sourcevpc2.yaml":         {Category: CategoryManaged},
				}),
				target: newTestTarget(),
				registry: getRegistry(
					withConfigurationMetadataConverter(configurationMetadataConverter{
						re:        AllConfigurations,
						converter: &configurationMetaTestConverter{},
					}),
					withConfigurationPackageConverter(configurationPackageConverter{
						re:        regexp.MustCompile(`xpkg.upbound.io/upbound/provider-ref-aws:.+`),
						converter: &configurationPackageTestConverter{},
					}),
					withProviderPackageConverter(providerPackageConverter{
						re:        regexp.MustCompile(`xpkg.upbound.io/upbound/provider-aws:.+`),
						converter: &monolithProviderToFamilyConfigConverter{},
					}),
					withProviderPackageConverter(providerPackageConverter{
						re:        regexp.MustCompile(`xpkg.upbound.io/upbound/provider-aws:.+`),
						converter: &monolithicProviderToSSOPConverter{},
					}),
					withPackageLockConverter(packageLockConverter{
						re:        CrossplaneLockName,
						converter: &lockConverter{},
					}),
				),
				opts: []PlanGeneratorOption{WithEnableConfigurationMigrationSteps()},
			},
			want: want{
				migrationPlanPath: "testdata/plan/generated/configurationv1_pkg_migration_plan.yaml",
				migratedResourceNames: []string{
					"disable-dependency-resolution/platform-ref-aws.configurations.pkg.crossplane.io_v1.yaml",
					"edit-configuration-package/platform-ref-aws.configurations.pkg.crossplane.io_v1.yaml",
					"enable-dependency-resolution/platform-ref-aws.configurations.pkg.crossplane.io_v1.yaml",
					"edit-configuration-metadata/platform-ref-aws.configurations.meta.pkg.crossplane.io_v1.yaml",
					"new-ssop/provider-family-aws.providers.pkg.crossplane.io_v1.yaml",
					"new-ssop/provider-aws-ec2.providers.pkg.crossplane.io_v1.yaml",
					"new-ssop/provider-aws-eks.providers.pkg.crossplane.io_v1.yaml",
					"activate-ssop/provider-family-aws.providers.pkg.crossplane.io_v1.yaml",
					"activate-ssop/provider-aws-ec2.providers.pkg.crossplane.io_v1.yaml",
					"activate-ssop/provider-aws-eks.providers.pkg.crossplane.io_v1.yaml",
					"edit-package-lock/lock.locks.pkg.crossplane.io_v1beta1.yaml",
					"deletion-policy-orphan/sample-vpc.vpcs.fakesourceapi_v1alpha1.yaml",
					"deletion-policy-delete/sample-vpc.vpcs.fakesourceapi_v1alpha1.yaml",
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pg := NewPlanGenerator(tt.fields.registry, tt.fields.source, tt.fields.target, tt.fields.opts...)
			err := pg.GeneratePlan()
			// compare error state
			if diff := cmp.Diff(tt.want.err, err, test.EquateErrors()); diff != "" {
				t.Fatalf("GeneratePlan(): -wantError, +gotError: %s", diff)
			}
			if err != nil {
				return
			}
			// compare preprocessor results
			for c, results := range tt.want.preProcessResults {
				pps := tt.fields.registry.unstructuredPreProcessors[c]
				if len(pps) != 1 {
					t.Fatalf("One pre-processor must have been registered for category: %s", c)
				}
				pp := pps[0].(*preProcessor)
				if diff := cmp.Diff(results, pp.results); diff != "" {
					t.Errorf("GeneratePlan(): -wantPreProcessorResults, +gotPreProcessorResults: %s", diff)
				}
			}
			// compare generated plan with the expected plan
			p, err := loadPlan(tt.want.migrationPlanPath)
			if err != nil {
				t.Fatalf("Failed to load plan file from path %s: %v", tt.want.migrationPlanPath, err)
			}
			if diff := cmp.Diff(p, &pg.Plan, cmpopts.IgnoreUnexported(Spec{})); diff != "" {
				t.Errorf("GeneratePlan(): -wantPlan, +gotPlan: %s", diff)
			}
			// compare generated migration files with the expected ones
			for _, name := range tt.want.migratedResourceNames {
				path := filepath.Join("testdata/plan/generated", name)
				buff, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("Failed to read a generated migration resource from path %s: %v", path, err)
				}
				u := unstructured.Unstructured{}
				if err := k8syaml.Unmarshal(buff, &u); err != nil {
					t.Fatalf("Failed to unmarshal a generated migration resource from path %s: %v", path, err)
				}
				gU, ok := tt.fields.target.targetManifests[name]
				if !ok {
					t.Errorf("GeneratePlan(): Expected generated migration resource file not found: %s", name)
					continue
				}
				removeNilValuedKeys(u.Object)
				if diff := cmp.Diff(u, gU.Object); diff != "" {
					t.Errorf("GeneratePlan(): -wantMigratedResource, +gotMigratedResource with name %q: %s", name, diff)
				}
				delete(tt.fields.target.targetManifests, name)
			}
			// check for unexpected generated migration files
			for name := range tt.fields.target.targetManifests {
				t.Errorf("GeneratePlan(): Unexpected generated migration file: %s", name)
			}
		})
	}
}

type testSource struct {
	sourceManifests map[string]Metadata
	paths           []string
	index           int
}

func newTestSource(sourceManifests map[string]Metadata) *testSource {
	result := &testSource{sourceManifests: sourceManifests}
	result.paths = make([]string, 0, len(result.sourceManifests))
	for k := range result.sourceManifests {
		result.paths = append(result.paths, k)
	}
	return result
}

func (f *testSource) HasNext() (bool, error) {
	return f.index <= len(f.paths)-1, nil
}

func (f *testSource) Next() (UnstructuredWithMetadata, error) {
	um := UnstructuredWithMetadata{
		Metadata: f.sourceManifests[f.paths[f.index]],
		Object:   unstructured.Unstructured{},
	}
	um.Metadata.Path = f.paths[f.index]
	buff, err := os.ReadFile(f.paths[f.index])
	if err != nil {
		return um, err
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(string(buff)), 1024)
	if err := decoder.Decode(&um.Object); err != nil {
		return um, err
	}
	f.index++
	return um, nil
}

func (f *testSource) Reset() error {
	f.index = 0
	return nil
}

type testTarget struct {
	targetManifests map[string]UnstructuredWithMetadata
}

func newTestTarget() *testTarget {
	return &testTarget{
		targetManifests: make(map[string]UnstructuredWithMetadata),
	}
}

func (f *testTarget) Put(o UnstructuredWithMetadata) error {
	f.targetManifests[o.Metadata.Path] = o
	return nil
}

func (f *testTarget) Delete(o UnstructuredWithMetadata) error {
	delete(f.targetManifests, o.Metadata.Path)
	return nil
}

// can be utilized to populate test artifacts
/*func (f *testTarget) dumpFiles(parentDir string) error {
	for f, u := range f.targetManifests {
		path := filepath.Join(parentDir, f)
		buff, err := k8syaml.Marshal(u.Object.Object)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, buff, 0o600); err != nil {
			return err
		}
	}
	return nil
}*/

type testConverter struct{}

func (f *testConverter) PatchSets(psMap map[string]*v1.PatchSet) error {
	psMap["ps1"].Patches[0].ToFieldPath = ptrFromString(`spec.forProvider.tags["key3"]`)
	psMap["ps6"].Patches[0].ToFieldPath = ptrFromString(`spec.forProvider.tags["key4"]`)
	return nil
}

func ptrFromString(s string) *string {
	return &s
}

type registryOption func(*Registry)

func withDelegatingConverter(gvk schema.GroupVersionKind, d delegatingConverter) registryOption {
	return func(r *Registry) {
		r.RegisterAPIConversionFunctions(gvk, d.rFn, d.cmpFn, nil)
	}
}

func withPatchSetConverter(c patchSetConverter) registryOption {
	return func(r *Registry) {
		r.RegisterPatchSetConverter(c.re, c.converter)
	}
}

func withConfigurationMetadataConverter(c configurationMetadataConverter) registryOption {
	return func(r *Registry) {
		r.RegisterConfigurationMetadataConverter(c.re, c.converter)
	}
}

func withConfigurationPackageConverter(c configurationPackageConverter) registryOption {
	return func(r *Registry) {
		r.RegisterConfigurationPackageConverter(c.re, c.converter)
	}
}

func withProviderPackageConverter(c providerPackageConverter) registryOption {
	return func(r *Registry) {
		r.RegisterProviderPackageConverter(c.re, c.converter)
	}
}

func withPackageLockConverter(c packageLockConverter) registryOption {
	return func(r *Registry) {
		r.RegisterPackageLockConverter(c.re, c.converter)
	}
}

func withPreProcessor(c Category, pp UnstructuredPreProcessor) registryOption {
	return func(r *Registry) {
		r.RegisterPreProcessor(c, pp)
	}
}

func getRegistry(opts ...registryOption) *Registry {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(fake.MigrationSourceGVK, &fake.MigrationSourceObject{})
	scheme.AddKnownTypeWithName(fake.MigrationTargetGVK, &fake.MigrationTargetObject{})
	r := NewRegistry(scheme)
	for _, o := range opts {
		o(r)
	}
	return r
}

func loadPlan(planPath string) (*Plan, error) {
	if planPath == "" {
		return emptyPlan(), nil
	}
	buff, err := os.ReadFile(planPath)
	if err != nil {
		return nil, err
	}
	p := &Plan{}
	return p, k8syaml.Unmarshal(buff, p)
}

func emptyPlan() *Plan {
	return &Plan{
		Version: versionV010,
	}
}

type configurationPackageTestConverter struct{}

func (c *configurationPackageTestConverter) ConfigurationPackageV1(pkg *xppkgv1.Configuration) error {
	pkg.Spec.Package = "xpkg.upbound.io/upbound/provider-ref-aws:v0.2.0-ssop"
	return nil
}

type configurationMetaTestConverter struct{}

func (cc *configurationMetaTestConverter) ConfigurationMetadataV1(c *xpmetav1.Configuration) error {
	c.Spec.DependsOn = []xpmetav1.Dependency{
		{
			Provider: ptrFromString("xpkg.upbound.io/upbound/provider-aws-eks"),
			Version:  ">=v0.17.0",
		},
	}
	return nil
}

func (cc *configurationMetaTestConverter) ConfigurationMetadataV1Alpha1(c *xpmetav1alpha1.Configuration) error {
	c.Spec.DependsOn = []xpmetav1alpha1.Dependency{
		{
			Provider: ptrFromString("xpkg.upbound.io/upbound/provider-aws-eks"),
			Version:  ">=v0.17.0",
		},
	}
	return nil
}

type monolithProviderToFamilyConfigConverter struct{}

func (c *monolithProviderToFamilyConfigConverter) ProviderPackageV1(_ xppkgv1.Provider) ([]xppkgv1.Provider, error) {
	ap := xppkgv1.ManualActivation
	return []xppkgv1.Provider{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "provider-family-aws",
			},
			Spec: xppkgv1.ProviderSpec{
				PackageSpec: xppkgv1.PackageSpec{
					Package:                  "xpkg.upbound.io/upbound/provider-family-aws:v0.37.0",
					RevisionActivationPolicy: &ap,
				},
			},
		},
	}, nil
}

type monolithicProviderToSSOPConverter struct{}

func (c *monolithicProviderToSSOPConverter) ProviderPackageV1(_ xppkgv1.Provider) ([]xppkgv1.Provider, error) {
	ap := xppkgv1.ManualActivation
	return []xppkgv1.Provider{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "provider-aws-ec2",
			},
			Spec: xppkgv1.ProviderSpec{
				PackageSpec: xppkgv1.PackageSpec{
					Package:                  "xpkg.upbound.io/upbound/provider-aws-ec2:v0.37.0",
					RevisionActivationPolicy: &ap,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "provider-aws-eks",
			},
			Spec: xppkgv1.ProviderSpec{
				PackageSpec: xppkgv1.PackageSpec{
					Package:                  "xpkg.upbound.io/upbound/provider-aws-eks:v0.37.0",
					RevisionActivationPolicy: &ap,
				},
			},
		},
	}, nil
}

type lockConverter struct{}

func (p *lockConverter) PackageLockV1Beta1(lock *xppkgv1beta1.Lock) error {
	lock.Packages = append(lock.Packages, xppkgv1beta1.LockPackage{
		Name:    "test-provider",
		Type:    ptr.To(xppkgv1beta1.ProviderPackageType),
		Source:  "xpkg.upbound.io/upbound/test-provider",
		Version: "vX.Y.Z",
	})
	return nil
}

type preProcessor struct {
	results []string
}

func (pp *preProcessor) PreProcess(u UnstructuredWithMetadata) error {
	pp.results = append(pp.results, getVersionedName(u.Object))
	return nil
}
