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

const (
	defaultAPIVersion = "v1alpha1"
)

func Run(pc config.Provider) { // nolint:gocyclo
	// Cyclomatic complexity of this function is above our goal of 10,
	// and it establishes a Terrajet code generation pipeline that's very similar
	// to other Terrajet based providers.
	// delete API dirs

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

		if matches(name, pc.SkipList) || !matches(name, pc.IncludeList) {
			continue
		}

		fmt.Printf("Generating code for resource: %s\n", name)

		words := strings.Split(name, "_")
		// As group name we default to the second element if resource name
		// has at least 3 elements, otherwise, we took the first element as
		// default group name, examples:
		// - aws_rds_cluster => rds
		// - aws_rds_cluster_parameter_group => rds
		// - kafka_topic => kafka
		groupName := words[1]
		if len(words) < 3 {
			groupName = words[0]
		}
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
		// Todo(turkenh): Handle APIVersions other than "v1alpha1"
		versionGen := NewVersionGenerator(wd, pc.ModulePath, strings.ToLower(group)+pc.GroupSuffix, defaultAPIVersion)

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
			resourceSchema := resources[name]
			resourceConfig := pc.GetResource(name)
			resourceConfig.TerraformResourceName = name
			if resourceConfig.Version == "" {
				resourceConfig.Version = defaultAPIVersion
			}
			if resourceConfig.Kind == "" {
				resourceConfig.Kind = kind
			}
			if err := crdGen.Generate(&resourceConfig, resourceSchema); err != nil {
				panic(errors.Wrap(err, "cannot generate crd"))
			}
			if err := tfGen.Generate(&resourceConfig, resourceSchema); err != nil {
				panic(errors.Wrap(err, "cannot generate terraformed"))
			}
			ctrlPkgPath, err := ctrlGen.Generate(&resourceConfig, versionGen.Package().Path())
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

func matches(name string, regexList []string) bool {
	for _, r := range regexList {
		ok, err := regexp.MatchString(r, name)
		if err != nil {
			panic(errors.Wrap(err, "cannot match regular expression"))
		}
		if ok {
			return true
		}
	}
	return false
}
