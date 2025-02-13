// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package pipeline

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/errors"

	"github.com/crossplane/upjet/pkg/config"
	"github.com/crossplane/upjet/pkg/examples"
	"github.com/crossplane/upjet/pkg/pipeline/templates"
)

type terraformedInput struct {
	*config.Resource
	ParametersTypeName string
}

// Run runs the Upjet code generation pipelines.
func Run(pc *config.Provider, rootDir string) {
	cluster := &PipelineRunner{
		DirAPIs:        filepath.Join(rootDir, "apis", "cluster"),
		DirControllers: filepath.Join(rootDir, "internal", "controller", "cluster"),
		DirExamples:    filepath.Join(rootDir, "examples-generated", "cluster"),
		DirHack:        filepath.Join(rootDir, "hack"),

		ModulePathAPIs:        filepath.Join(pc.ModulePath, "apis", "cluster"),
		ModulePathControllers: filepath.Join(pc.ModulePath, "internal", "controller", "cluster"),

		Scope: "Cluster",
	}

	namespaced := &PipelineRunner{
		DirAPIs:        filepath.Join(rootDir, "apis", "namespaced"),
		DirControllers: filepath.Join(rootDir, "internal", "controller", "namespaced"),
		DirExamples:    filepath.Join(rootDir, "examples-generated", "namespaced"),
		DirHack:        filepath.Join(rootDir, "hack"),

		ModulePathAPIs:        filepath.Join(pc.ModulePath, "apis", "namespaced"),
		ModulePathControllers: filepath.Join(pc.ModulePath, "internal", "controller", "namespaced"),

		Scope: "Namespaced",
	}

	// Map of service name (e.g. ec2) to resource controller packages. Should be
	// the same for cluster and namespaced, so we only save one.
	groups := cluster.Run(pc)
	_ = namespaced.Run(pc)

	if err := NewMainGenerator(filepath.Join(rootDir, "cmd", "provider"), pc.MainTemplate).Generate(groups); err != nil {
		panic(errors.Wrap(err, "cannot generate main.go"))
	}
}

type PipelineRunner struct {
	DirAPIs        string
	DirControllers string
	DirExamples    string
	DirHack        string

	ModulePathAPIs        string
	ModulePathControllers string

	// TODO(negz): Eventually I think we'll need a different template for
	// namespace scoped resources too, e.g. without namespaces in secret refs.
	Scope string
}

func (r *PipelineRunner) Run(pc *config.Provider) []string { //nolint:gocyclo
	// Note(turkenh): nolint reasoning - this is the main function of the code
	// generation pipeline. We didn't want to split it into multiple functions
	// for better readability considering the straightforward logic here.

	// Group resources based on their Group and API Versions.
	// An example entry in the tree would be:
	// ec2.aws.upbound.io -> v1beta1 -> aws_vpc
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

	exampleGen := examples.NewGenerator(r.DirExamples, r.ModulePathAPIs, pc.ShortName, pc.Resources)
	if err := exampleGen.SetReferenceTypes(pc.Resources); err != nil {
		panic(errors.Wrap(err, "cannot set reference types for resources"))
	}
	// Add ProviderConfig API package to the list of API version packages.
	apiVersionPkgList := make([]string, 0)
	for _, p := range pc.BasePackages.APIVersion {
		apiVersionPkgList = append(apiVersionPkgList, filepath.Join(r.ModulePathAPIs, p))
	}
	// Add ProviderConfig controller package to the list of controller packages.
	controllerPkgMap := make(map[string][]string)
	// new API takes precedence
	for p, g := range pc.BasePackages.ControllerMap {
		path := filepath.Join(r.ModulePathControllers, p)
		controllerPkgMap[g] = append(controllerPkgMap[g], path)
		controllerPkgMap[config.PackageNameMonolith] = append(controllerPkgMap[config.PackageNameMonolith], path)
	}
	//nolint:staticcheck
	for _, p := range pc.BasePackages.Controller {
		path := filepath.Join(r.ModulePathControllers, p)
		found := false
		for _, p := range controllerPkgMap[config.PackageNameConfig] {
			if path == p {
				found = true
				break
			}
		}
		if !found {
			controllerPkgMap[config.PackageNameConfig] = append(controllerPkgMap[config.PackageNameConfig], path)
		}
		found = false
		for _, p := range controllerPkgMap[config.PackageNameMonolith] {
			if path == p {
				found = true
				break
			}
		}
		if !found {
			controllerPkgMap[config.PackageNameMonolith] = append(controllerPkgMap[config.PackageNameMonolith], path)
		}
	}

	count := 0
	for group, versions := range resourcesGroups {
		shortGroup := strings.Split(group, ".")[0]
		for version, resources := range versions {
			versionGen := NewVersionGenerator(r.DirAPIs, r.DirHack, r.ModulePathAPIs, group, version)
			crdGen := NewCRDGenerator(versionGen.Package(), r.DirAPIs, r.DirHack, pc.ShortName, group, version, r.Scope)
			tfGen := NewTerraformedGenerator(versionGen.Package(), r.DirAPIs, r.DirHack, group, version)
			ctrlGen := NewControllerGenerator(r.DirControllers, r.DirHack, r.ModulePathControllers, group)

			if err := versionGen.InsertPreviousObjects(versions); err != nil {
				fmt.Println(errors.Wrapf(err, "cannot insert type definitions from the previous versions into the package scope for group %q", group))
			}

			var tfResources []*terraformedInput
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
				watchVersionGen := versionGen
				if len(resources[name].ControllerReconcileVersion) != 0 {
					watchVersionGen = NewVersionGenerator(r.DirAPIs, r.DirHack, r.ModulePathAPIs, group, resources[name].ControllerReconcileVersion)
				}
				ctrlPkgPath, err := ctrlGen.Generate(resources[name], watchVersionGen.Package().Path(), featuresPkgPath)
				if err != nil {
					panic(errors.Wrapf(err, "cannot generate controller for resource %s", name))
				}
				controllerPkgMap[shortGroup] = append(controllerPkgMap[shortGroup], ctrlPkgPath)
				controllerPkgMap[config.PackageNameMonolith] = append(controllerPkgMap[config.PackageNameMonolith], ctrlPkgPath)
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
			p := versionGen.Package().Path()
			apiVersionPkgList = append(apiVersionPkgList, p)
		}
		conversionHubGen := NewConversionNodeGenerator(r.DirAPIs, r.DirHack, r.ModulePathAPIs, group, "zz_generated.conversion_hubs.go", templates.ConversionHubTemplate,
			func(c *config.Resource, fileAPIVersion string) bool {
				// if this is the hub version, then mark it as a hub
				return c.CRDHubVersion() == fileAPIVersion
			})
		if err := conversionHubGen.Generate(versions); err != nil {
			panic(errors.Wrapf(err, "cannot generate the conversion.Hub function for the resource group %q", group))
		}
		conversionSpokeGen := NewConversionNodeGenerator(r.DirAPIs, r.DirHack, r.ModulePathAPIs, group, "zz_generated.conversion_spokes.go", templates.ConversionSpokeTemplate,
			func(c *config.Resource, fileAPIVersion string) bool {
				// if not the hub version, mark it as a spoke
				return c.CRDHubVersion() != fileAPIVersion
			})
		if err := conversionSpokeGen.Generate(versions); err != nil {
			panic(errors.Wrapf(err, "cannot generate the conversion.Convertible functions for the resource group %q", group))
		}

		base := filepath.Join(r.ModulePathAPIs, shortGroup)
		for _, versions := range resourcesGroups {
			for _, resources := range versions {
				for _, r := range resources {
					// if there are spoke versions for the given group.Kind
					if spokeVersions := conversionSpokeGen.nodeVersionsMap[fmt.Sprintf("%s.%s", r.ShortGroup, r.Kind)]; spokeVersions != nil {
						for _, sv := range spokeVersions {
							apiVersionPkgList = append(apiVersionPkgList, filepath.Join(base, sv))
						}
					}
					// if there are hub versions for the given group.Kind
					if hubVersions := conversionHubGen.nodeVersionsMap[fmt.Sprintf("%s.%s", r.ShortGroup, r.Kind)]; hubVersions != nil {
						for _, sv := range hubVersions {
							apiVersionPkgList = append(apiVersionPkgList, filepath.Join(base, sv))
						}
					}
				}
			}
		}
	}

	if err := exampleGen.StoreExamples(); err != nil {
		panic(errors.Wrapf(err, "cannot store examples"))
	}

	if err := NewRegisterGenerator(r.DirAPIs, r.DirHack, r.ModulePathAPIs).Generate(apiVersionPkgList); err != nil {
		panic(errors.Wrap(err, "cannot generate register file"))
	}

	if err := NewSetupGenerator(r.DirControllers, r.DirHack, r.ModulePathAPIs).Generate(controllerPkgMap); err != nil {
		panic(errors.Wrap(err, "cannot generate setup file"))
	}

	// NOTE(muvaf): gosec linter requires that the whole command is hard-coded.
	// So, we set the directory of the command instead of passing in the directory
	// as an argument to "find".
	apisCmd := exec.Command("bash", "-c", "goimports -w $(find . -iname 'zz_*')")
	apisCmd.Dir = filepath.Clean(r.DirAPIs)
	if out, err := apisCmd.CombinedOutput(); err != nil {
		panic(errors.Wrap(err, "cannot run goimports for apis folder: "+string(out)))
	}

	ctrlCmd := exec.Command("bash", "-c", "goimports -w $(find . -iname 'zz_*')")
	ctrlCmd.Dir = filepath.Clean(r.DirControllers)
	if out, err := ctrlCmd.CombinedOutput(); err != nil {
		panic(errors.Wrap(err, "cannot run goimports for controller folder: "+string(out)))
	}

	fmt.Printf("\nGenerated %d resources!\n", count)

	groups := make([]string, 0, len(controllerPkgMap))
	for g := range controllerPkgMap {
		groups = append(groups, g)
	}
	return groups
}

// TODO(negz): This could be slices.Sorted(maps.Keys(m)) with Go v1.24+

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
