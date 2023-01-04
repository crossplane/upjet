// Copyright 2022 Upbound Inc.
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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	xpresource "github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	v1 "github.com/crossplane/crossplane/apis/apiextensions/v1"
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/upbound/upjet/pkg/migration/fake"
)

func TestGeneratePlan(t *testing.T) {
	type fields struct {
		source   Source
		target   *testTarget
		registry *Registry
	}
	type want struct {
		err               error
		migrationPlanPath string
		// names of resource files to be loaded
		migratedResourceNames []string
	}
	tests := map[string]struct {
		fields fields
		want   want
	}{
		"PlanWithManagedResourceAndClaim": {
			fields: fields{
				source: newTestSource(map[string]Metadata{
					"testdata/plan/sourcevpc.yaml":   {},
					"testdata/plan/claim.yaml":       {Category: CategoryClaim},
					"testdata/plan/composition.yaml": {},
					"testdata/plan/xrd.yaml":         {},
					"testdata/plan/xr.yaml":          {Category: CategoryComposite}}),
				target: newTestTarget(),
				registry: getRegistryWithConverters(map[schema.GroupVersionKind]Converter{
					fake.MigrationSourceGVK: &testConverter{},
				}),
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
					"new-compositions/example-migrated.compositions.apiextensions.crossplane.io.yaml",
					"start-composites/my-resource-dwjgh.xmyresources.test.com.yaml",
					"create-new-managed/sample-vpc.vpcs.faketargetapi.yaml",
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pg := NewPlanGenerator(tt.fields.registry, tt.fields.source, tt.fields.target)
			err := pg.GeneratePlan()
			// compare error state
			if diff := cmp.Diff(tt.want.err, err, test.EquateErrors()); diff != "" {
				t.Fatalf("GeneratePlan(): -wantError, +gotError: %s", diff)
			}
			if err != nil {
				return
			}
			// compare generated plan with the expected plan
			p, err := loadPlan(tt.want.migrationPlanPath)
			if err != nil {
				t.Fatalf("Failed to load plan file from path %s: %v", tt.want.migrationPlanPath, err)
			}
			if diff := cmp.Diff(p, &pg.Plan); diff != "" {
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

func (f *testConverter) Resources(mg xpresource.Managed) ([]xpresource.Managed, error) {
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
}

func (f *testConverter) Composition(sourcePatchSets []v1.PatchSet, sourceTemplate v1.ComposedTemplate, convertedTemplates ...*v1.ComposedTemplate) ([]v1.PatchSet, error) {
	// convert patches in the migration target composed templates
	for i, cb := range convertedTemplates {
		for j, p := range cb.Patches {
			if p.ToFieldPath == nil || !strings.HasPrefix(*p.ToFieldPath, "spec.forProvider.tags") {
				continue
			}
			u, err := FromRawExtension(sourceTemplate.Base)
			if err != nil {
				return nil, err
			}
			paved := fieldpath.Pave(u.Object)
			key, err := paved.GetString(strings.ReplaceAll(*p.ToFieldPath, ".value", ".key"))
			if err != nil {
				return nil, err
			}
			s := fmt.Sprintf(`spec.forProvider.tags["%s"]`, key)
			convertedTemplates[i].Patches[j].ToFieldPath = &s
		}
	}
	// convert patch sets in the source
	targetPatchSets := make([]v1.PatchSet, 0, len(sourcePatchSets))
	for _, ps := range sourcePatchSets {
		tPs := ps.DeepCopy()
		for i, p := range tPs.Patches {
			if p.ToFieldPath == nil || *p.ToFieldPath != "spec.forProvider.tags[2].value" {
				continue
			}
			*tPs.Patches[i].ToFieldPath = `spec.forProvider.tags["key3"]`
		}
		targetPatchSets = append(targetPatchSets, *tPs)
	}
	return targetPatchSets, nil
}

func getRegistryWithConverters(converters map[schema.GroupVersionKind]Converter) *Registry {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(fake.MigrationSourceGVK, &fake.MigrationSourceObject{})
	r := NewRegistry(scheme)
	for gvk, c := range converters {
		r.RegisterConverter(gvk, c)
	}
	return r
}

func loadPlan(planPath string) (*Plan, error) {
	buff, err := os.ReadFile(planPath)
	if err != nil {
		return nil, err
	}
	p := &Plan{}
	return p, k8syaml.Unmarshal(buff, p)
}
