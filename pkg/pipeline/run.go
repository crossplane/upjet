/*
 Copyright 2021 The Crossplane Authors.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package pipeline

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crossplane-contrib/terrajet/pkg/config"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
)

// Run runs the Terrajet code generation pipelines.
func Run(pc *config.Provider, rootDir string) { // nolint:gocyclo
	// Note(turkenh): nolint reasoning - this is the main function of the code
	// generation pipeline. We didn't want to split it into multiple functions
	// for better readability considering the straightforward logic here.

	// Group resources based on their Group and API Versions.
	// An example entry in the tree would be:
	// ec2.tfaws.crossplane.io -> v1alpha1 -> aws_vpc
	resourcesGroups := map[string]map[string]map[string]*config.Resource{}
	for name, resource := range pc.Resources {
		group := pc.RootGroup
		if resource.ShortGroup != "" {
			group = strings.ToLower(resource.ShortGroup) + "." + pc.RootGroup
		}
		if len(resourcesGroups[group]) == 0 {
			resourcesGroups[group] = map[string]map[string]*config.Resource{}
		}
		if len(resourcesGroups[group][resource.Version]) == 0 {
			resourcesGroups[group][resource.Version] = map[string]*config.Resource{}
		}
		resourcesGroups[group][resource.Version][name] = resource
	}

	// Add ProviderConfig API package to the list of API version packages.
	apiVersionPkgList := make([]string, 0)
	for _, p := range pc.BasePackages.APIVersion {
		apiVersionPkgList = append(apiVersionPkgList, filepath.Join(pc.ModulePath, p))
	}
	// Add ProviderConfig controller package to the list of controller packages.
	controllerPkgList := make([]string, 0)
	for _, p := range pc.BasePackages.Controller {
		controllerPkgList = append(controllerPkgList, filepath.Join(pc.ModulePath, p))
	}
	count := 0
	for group, versions := range resourcesGroups {
		for version, resources := range versions {
			versionGen := NewVersionGenerator(rootDir, pc.ModulePath, group, version)
			crdGen := NewCRDGenerator(versionGen.Package(), rootDir, pc.ShortName, group, version)
			tfGen := NewTerraformedGenerator(versionGen.Package(), rootDir, group, version)
			ctrlGen := NewControllerGenerator(rootDir, pc.ModulePath, group)

			for _, name := range sortedResources(resources) {
				if err := crdGen.Generate(resources[name]); err != nil {
					panic(errors.Wrapf(err, "cannot generate crd for resource %s", name))
				}
				if err := tfGen.Generate(resources[name]); err != nil {
					panic(errors.Wrapf(err, "cannot generate terraformed for resource %s", name))
				}
				ctrlPkgPath, err := ctrlGen.Generate(resources[name], versionGen.Package().Path())
				if err != nil {
					panic(errors.Wrapf(err, "cannot generate controller for resource %s", name))
				}
				controllerPkgList = append(controllerPkgList, ctrlPkgPath)
				count++
			}

			if err := versionGen.Generate(); err != nil {
				panic(errors.Wrap(err, "cannot generate version files"))
			}
			apiVersionPkgList = append(apiVersionPkgList, versionGen.Package().Path())
		}
	}

	if err := NewRegisterGenerator(rootDir, pc.ModulePath).Generate(apiVersionPkgList); err != nil {
		panic(errors.Wrap(err, "cannot generate register file"))
	}
	if err := NewSetupGenerator(rootDir, pc.ModulePath).Generate(controllerPkgList); err != nil {
		panic(errors.Wrap(err, "cannot generate setup file"))
	}

	// NOTE(muvaf): gosec linter requires that the whole command is hard-coded.
	// So, we set the directory of the command instead of passing in the directory
	// as an argument to "find".
	apisCmd := exec.Command("bash", "-c", "goimports -w $(find . -iname 'zz_*')")
	apisCmd.Dir = filepath.Clean(filepath.Join(rootDir, "apis"))
	if out, err := apisCmd.CombinedOutput(); err != nil {
		panic(errors.Wrap(err, "cannot run goimports for apis folder: "+string(out)))
	}

	internalCmd := exec.Command("bash", "-c", "goimports -w $(find . -iname 'zz_*')")
	internalCmd.Dir = filepath.Clean(filepath.Join(rootDir, "internal"))
	if out, err := internalCmd.CombinedOutput(); err != nil {
		panic(errors.Wrap(err, "cannot run goimports for internal folder: "+string(out)))
	}

	fmt.Printf("\nGenerated %d resources!\n", count)
}

func sortedResources(m map[string]*config.Resource) []string {
	result := make([]string, len(m))
	i := 0
	for g := range m {
		result[i] = g
		i++
	}
	sort.Strings(result)
	return result
}
