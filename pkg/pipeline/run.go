/*
Copyright 2022 Upbound Inc.
*/

package pipeline

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/errors"

	"github.com/upbound/upjet/pkg/config"
	"github.com/upbound/upjet/pkg/examples"
)

type terraformedInput struct {
	*config.Resource
	ParametersTypeName string
}

// Run runs the Upjet code generation pipelines.
func Run(pc *config.Provider, rootDir string) { // nolint:gocyclo
	// Note(turkenh): nolint reasoning - this is the main function of the code
	// generation pipeline. We didn't want to split it into multiple functions
	// for better readability considering the straightforward logic here.

	// Group resources based on their Group and API Versions.
	// An example entry in the tree would be:
	// ec2.awsjet.crossplane.io -> v1alpha1 -> aws_vpc
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

	exampleGen := examples.NewGenerator(rootDir, pc.ModulePath, pc.ShortName, pc.Resources)
	if err := exampleGen.SetReferenceTypes(pc.Resources); err != nil {
		panic(errors.Wrap(err, "cannot set reference types for resources"))
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
			var tfResources []*terraformedInput
			versionGen := NewVersionGenerator(rootDir, pc.ModulePath, group, version)
			crdGen := NewCRDGenerator(versionGen.Package(), rootDir, pc.ShortName, group, version)
			tfGen := NewTerraformedGenerator(versionGen.Package(), rootDir, group, version)
			ctrlGen := NewControllerGenerator(rootDir, pc.ModulePath, group)

			for _, name := range sortedResources(resources) {
				paramTypeName, err := crdGen.Generate(resources[name])
				if err != nil {
					panic(errors.Wrapf(err, "cannot generate crd for resource %s", name))
				}
				tfResources = append(tfResources, &terraformedInput{
					Resource:           resources[name],
					ParametersTypeName: paramTypeName,
				})

				featuresPkgPath := ""
				if pc.FeaturesPackage != "" {
					featuresPkgPath = filepath.Join(pc.ModulePath, pc.FeaturesPackage)
				}
				ctrlPkgPath, err := ctrlGen.Generate(resources[name], versionGen.Package().Path(), featuresPkgPath)
				if err != nil {
					panic(errors.Wrapf(err, "cannot generate controller for resource %s", name))
				}
				controllerPkgList = append(controllerPkgList, ctrlPkgPath)
				if err := exampleGen.Generate(group, version, resources[name]); err != nil {
					panic(errors.Wrapf(err, "cannot generate example manifest for resource %s", name))
				}
				count++
			}

			if err := tfGen.Generate(tfResources, version); err != nil {
				panic(errors.Wrapf(err, "cannot generate terraformed for resource %s", group))
			}

			if err := versionGen.Generate(); err != nil {
				panic(errors.Wrap(err, "cannot generate version files"))
			}
			apiVersionPkgList = append(apiVersionPkgList, versionGen.Package().Path())
		}
	}

	if err := exampleGen.StoreExamples(); err != nil {
		panic(errors.Wrapf(err, "cannot store examples"))
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
