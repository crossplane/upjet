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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/crossplane-contrib/terrajet/pkg/config"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/iancoleman/strcase"
)

func Run(pc config.Provider) { // nolint:gocyclo
	// Cyclomatic complexity of this function is above our goal of 10,
	// and it establishes a Terrajet code generation pipeline that's very similar
	// to other Terrajet based providers.
	// delete API dirs
	/*	deleteGenDirs("../apis", map[string]struct{}{
			"v1alpha1": {},
			"rconfig":  {},
		})
		// delete controller dirs
		deleteGenDirs("../internal/controller", map[string]struct{}{
			"config": {},
		})*/

	//genConfig.SetResourceConfigurations()
	wd, err := os.Getwd()
	if err != nil {
		panic(errors.Wrap(err, "cannot get working directory"))
	}
	fmt.Println(wd)
	groups := map[string]map[string]*schema.Resource{}
	for name, resource := range pc.Schema.ResourcesMap {
		if name == "azurerm_virtual_network" {
			if t, ok := resource.Schema["subnet"]; ok {
				t.Computed = true
				t.Optional = false
			}
		}

		if len(resource.Schema) == 0 {
			// There are resources with no schema, that we will address later.
			fmt.Printf("Skipping resource %s because it has no schema\n", name)
			continue
		}
		if _, ok := pc.SkipList[name]; ok {
			continue
		}

		match := false
		for _, r := range pc.IncludeList {
			ok, err := regexp.MatchString(r, name)
			if err != nil {
				panic(errors.Wrap(err, "cannot match regular expression"))
			}
			if ok {
				match = true
				break
			}
		}
		if !match {
			continue
		}

		fmt.Printf("Generating code for resource: %s\n", name)

		words := strings.Split(name, "_")
		groupName := words[1]
		if len(groups[groupName]) == 0 {
			groups[groupName] = map[string]*schema.Resource{}
		}
		groups[groupName][name] = resource
	}
	count := 0
	versionPkgList := []string{
		// TODO(turkenh): a parameter for v1alpha1 here?
		filepath.Join(pc.ModulePath, "apis", "v1alpha1"),
	}
	controllerPkgList := []string{
		filepath.Join(pc.ModulePath, "internal", "controller", "providerconfig"),
	}
	for group, resources := range groups {
		version := "v1alpha1"
		versionGen := NewVersionGenerator(wd, pc.ModulePath, strings.ToLower(group)+pc.GroupSuffix, version)

		crdGen := NewCRDGenerator(versionGen.Package(), versionGen.DirectoryPath(), strings.ToLower(group)+pc.GroupSuffix, pc.ShortName)
		tfGen := NewTerraformedGenerator(versionGen.Package(), versionGen.DirectoryPath())
		ctrlGen := NewControllerGenerator(wd, pc.ModulePath, strings.ToLower(group)+pc.GroupSuffix)

		keys := make([]string, len(resources))
		i := 0
		for k := range resources {
			keys[i] = k
			i++
		}
		sort.Strings(keys)

		for _, name := range keys {
			// We don't want Azurerm prefix in all kinds.
			kind := strcase.ToCamel(strings.TrimPrefix(strings.TrimPrefix(name, pc.ResourcePrefix), group))
			resource := resources[name]
			r := config.NewResource(version, kind, name)
			if err = r.OverrideConfig(pc.GetForResource(name)); err != nil {
				panic(errors.Wrap(err, "cannot override config"))
			}
			if err := crdGen.Generate(r, resource); err != nil {
				panic(errors.Wrap(err, "cannot generate crd"))
			}
			if err := tfGen.Generate(r, resource); err != nil {
				panic(errors.Wrap(err, "cannot generate terraformed"))
			}
			ctrlPkgPath, err := ctrlGen.Generate(r, versionGen.Package().Path())
			if err != nil {
				panic(errors.Wrap(err, "cannot generate controller"))
			}
			controllerPkgList = append(controllerPkgList, ctrlPkgPath)
			count++
		}

		if err := versionGen.Generate(); err != nil {
			panic(errors.Wrap(err, "cannot generate version files"))
		}
		versionPkgList = append(versionPkgList, versionGen.Package().Path())
	}

	if err := NewRegisterGenerator(wd, pc.ModulePath).Generate(versionPkgList); err != nil {
		panic(errors.Wrap(err, "cannot generate register file"))
	}
	if err := NewSetupGenerator(wd, pc.ModulePath).Generate(controllerPkgList); err != nil {
		panic(errors.Wrap(err, "cannot generate setup file"))
	}
	if out, err := exec.Command("bash", "-c", "goimports -w $(find apis -iname 'zz_*')").CombinedOutput(); err != nil {
		panic(errors.Wrap(err, "cannot run goimports for apis folder: "+string(out)))
	}
	if out, err := exec.Command("bash", "-c", "goimports -w $(find internal -iname 'zz_*')").CombinedOutput(); err != nil {
		panic(errors.Wrap(err, "cannot run goimports for internal folder: "+string(out)))
	}
	fmt.Printf("\nGenerated %d resources!\n", count)
}

// delete API subdirs for a clean start
func deleteGenDirs(rootDir string, keepMap map[string]struct{}) {
	files, err := ioutil.ReadDir(rootDir)
	if err != nil {
		panic(errors.Wrapf(err, "cannot list files under %s", rootDir))
	}

	for _, f := range files {
		if !f.IsDir() {
			continue
		}
		if _, ok := keepMap[f.Name()]; ok {
			continue
		}
		removeDir := filepath.Join(rootDir, f.Name())
		if err := os.RemoveAll(removeDir); err != nil {
			panic(errors.Wrapf(err, "cannot remove API dir: %s", removeDir))
		}
	}
}
