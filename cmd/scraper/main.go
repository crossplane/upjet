/*
Copyright 2022 Upbound Inc.
*/

package main

import (
	"os"
	"path/filepath"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/upbound/upjet/pkg/registry"
)

func main() {
	var (
		app            = kingpin.New(filepath.Base(os.Args[0]), "Terraform Registry provider metadata scraper.").DefaultEnvars()
		outFile        = app.Flag("out", "Provider metadata output file path").Short('o').Default("provider-metadata.yaml").OpenFile(os.O_CREATE, 0644)
		providerName   = app.Flag("name", "Provider name").Short('n').Required().String()
		resourcePrefix = app.Flag("resource-prefix", `Terraform resource name prefix for the Terraform provider. For example, this is "google" for the google Terraform provider.`).String()
		codeXPath      = app.Flag("code-xpath", "Code XPath expression").Default(`//code[@class="language-terraform" or @class="language-hcl"]/text()`).String()
		preludeXPath   = app.Flag("prelude-xpath", "Prelude XPath expression").Default(`//text()[contains(., "description") and contains(., "page_title")]`).String()
		fieldXPath     = app.Flag("field-xpath", "Field documentation XPath expression").Default(`//ul/li//code[1]/text()`).String()
		importXPath    = app.Flag("import-xpath", "Import statements XPath expression").Default(`//code[@class="language-shell"]/text()`).String()
		repoPath       = app.Flag("repo", "Terraform provider repo path").Short('r').Required().ExistingDir()
		debug          = app.Flag("debug", "Output debug messages").Short('d').Default("false").Bool()
		fileExtensions = app.Flag("extensions", "Extensions of the files to be scraped").Short('e').Default(".md", ".markdown").Strings()
	)
	kingpin.MustParse(app.Parse(os.Args[1:]))

	pm := registry.NewProviderMetadata(*providerName)
	kingpin.FatalIfError(pm.ScrapeRepo(&registry.ScrapeConfiguration{
		Debug:          *debug,
		RepoPath:       *repoPath,
		CodeXPath:      *codeXPath,
		PreludeXPath:   *preludeXPath,
		FieldDocXPath:  *fieldXPath,
		ImportXPath:    *importXPath,
		FileExtensions: *fileExtensions,
		ResourcePrefix: *resourcePrefix,
	}), "Failed to scrape Terraform provider metadata")
	kingpin.FatalIfError(pm.Store((*outFile).Name()), "Failed to store Terraform provider metadata to file: %s", (*outFile).Name())
}
