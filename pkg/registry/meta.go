/*
Copyright 2022 Upbound Inc.
*/

package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/antchfx/htmlquery"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pkg/errors"
	"github.com/tmccombs/hcl2json/convert"
	"github.com/yuin/goldmark"
	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

const (
	extMarkdown    = ".markdown"
	blockResource  = "resource"
	keySubCategory = "subcategory"
	keyDescription = "description"
	keyPageTitle   = "page_title"
)

var (
	regexConfigurationBlock = regexp.MustCompile(`block.*support`)
)

// NewProviderMetadata initializes a new ProviderMetadata for
// extracting metadata from the Terraform registry.
func NewProviderMetadata(name string) *ProviderMetadata {
	return &ProviderMetadata{
		Name:      name,
		Resources: make(map[string]*Resource),
	}
}

func (r *Resource) addExampleManifest(file *hcl.File, body *hclsyntax.Block) error {
	refs, err := r.findReferences("", file, body)
	if err != nil {
		return err
	}
	r.Examples = append(r.Examples, ResourceExample{
		Name:       body.Labels[1],
		References: refs,
	})
	return nil
}

func (r *Resource) scrapeExamples(doc *html.Node, codeElXPath string, debug bool) error { // nolint: gocyclo
	resourceName := r.Title
	nodes := htmlquery.Find(doc, codeElXPath)
	for _, n := range nodes {
		parser := hclparse.NewParser()
		f, diag := parser.ParseHCL([]byte(n.Data), "example.hcl")
		if debug && diag != nil && diag.HasErrors() {
			fmt.Println(errors.Wrapf(diag, "failed to parse example Terraform configuration for %q: Configuration:\n%s", resourceName, n.Data))
		}
		if f == nil {
			continue
		}
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			return errors.Errorf("not an HCL Body: %s", n.Data)
		}
		trimmed := make(hclsyntax.Blocks, 0, len(body.Blocks))
		for _, b := range body.Blocks {
			if b.Type == blockResource {
				trimmed = append(trimmed, b)
			}
		}
		body.Blocks = trimmed
		// first try an exact match to find the example
		if err := r.findExampleBlock(f, body.Blocks, &resourceName, true); err != nil {
			return err
		}
		r.Name = resourceName
	}

	if r.Name == "" {
		r.Name = resourceName
	}
	return nil
}

func (r *Resource) findReferences(parentPath string, file *hcl.File, b *hclsyntax.Block) (map[string]string, error) { // nolint: gocyclo
	refs := make(map[string]string)
	if parentPath == "" && b.Labels[0] != r.Name {
		return refs, nil
	}
	for name, attr := range b.Body.Attributes {
		e, ok := attr.Expr.(*hclsyntax.ScopeTraversalExpr)
		if !ok {
			continue
		}
		refName := name
		if parentPath != "" {
			refName = fmt.Sprintf("%s.%s", parentPath, refName)
		}
		ref := string(file.Bytes[e.Range().Start.Byte:e.Range().End.Byte])
		if v, ok := refs[refName]; ok && v != ref {
			return nil, errors.Errorf("attribute %s.%s refers to %s. New reference: %s", r.Name, refName, v, ref)
		}
		refs[refName] = ref
	}
	for _, nestedBlock := range b.Body.Blocks {
		path := nestedBlock.Type
		if parentPath != "" {
			path = fmt.Sprintf("%s.%s", parentPath, path)
		}
		nestedRefs, err := r.findReferences(path, file, nestedBlock)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot find references in nested block: %s", path)
		}
		for k, v := range nestedRefs {
			refs[k] = v
		}
	}
	return refs, nil
}

func suffixMatch(label, resourceName string, limit int) bool {
	suffixParts := strings.Split(resourceName, "_")
	for i := 0; i < len(suffixParts) && (limit == -1 || i <= limit); i++ {
		s := strings.Join(suffixParts[i:], "_")
		if strings.Contains(label, s) {
			return true
		}
	}
	return false
}

func convertManifest2JSON(file *hcl.File, b *hclsyntax.Block) (string, error) {
	buff, err := convert.File(&hcl.File{
		Body:  b.Body,
		Bytes: file.Bytes,
	}, convert.Options{})
	if err != nil {
		return "", errors.Wrap(err, "failed to format as JSON")
	}
	out := bytes.Buffer{}
	err = json.Indent(&out, buff, "", "  ")
	if err != nil {
		return "", errors.Wrap(err, "unable to format JSON example manifest")
	}
	return out.String(), nil
}

func (r *Resource) findExampleBlock(file *hcl.File, blocks hclsyntax.Blocks, resourceName *string, exactMatch bool) error {
	dependencies := make(map[string]string)
	for _, b := range blocks {
		depKey := fmt.Sprintf("%s.%s", b.Labels[0], b.Labels[1])
		m, err := convertManifest2JSON(file, b)
		if err != nil {
			return errors.Wrap(err, "failed to convert example manifest to JSON")
		}
		if b.Labels[0] != *resourceName {
			if exactMatch {
				dependencies[depKey] = m
				continue
			}

			if suffixMatch(b.Labels[0], *resourceName, 1) {
				*resourceName = b.Labels[0]
				exactMatch = true
			} else {
				dependencies[depKey] = m
				continue
			}
		}
		r.Name = *resourceName
		err = r.addExampleManifest(file, b)
		r.Examples[len(r.Examples)-1].Manifest = m
		r.Examples[len(r.Examples)-1].Dependencies = dependencies
		if err != nil {
			return errors.Wrap(err, "failed to add example manifest to resource")
		}
	}

	if len(r.Examples) == 0 && exactMatch {
		return r.findExampleBlock(file, blocks, resourceName, false)
	}
	return nil
}

func (r *Resource) scrapePrelude(doc *html.Node, preludeXPath string) error {
	// parse prelude
	nodes := htmlquery.Find(doc, preludeXPath)
	if len(nodes) == 0 {
		return errors.Errorf("failed to extract subcategory and title name of the resource")
	}

	n := nodes[0]
	rawData := n.Data
	lines := strings.Split(n.Data, "\n")
	descIndex := -1
	for i, l := range lines {
		kv := strings.Split(l, ":")
		if len(kv) < 2 {
			continue
		}
		switch kv[0] {
		case keyPageTitle:
			r.Title = strings.TrimSpace(strings.ReplaceAll(kv[len(kv)-1], `"`, ""))

		case keyDescription:
			r.Description = kv[1]
			descIndex = i

		case keySubCategory:
			r.SubCategory = strings.TrimSpace(strings.ReplaceAll(kv[1], `"`, ""))
		}
	}

	if descIndex > -1 {
		r.Description += strings.Join(lines[descIndex+1:], " ")
	}
	r.Description = strings.TrimSpace(strings.Replace(r.Description, "|-", "", 1))

	if r.SubCategory == "" || r.Title == "" {
		return errors.Errorf("failed to parse prelude. Description: %s, Subcategory: %s, Title name: %s. Raw data:%s\n",
			r.Description, r.SubCategory, r.Title, rawData)
	}
	return nil
}

func (r *Resource) scrapeFieldDocs(doc *html.Node, fieldXPath string) {
	conflictedFields := make(map[string]bool)
	processed := make(map[*html.Node]struct{})
	codeNodes := htmlquery.Find(doc, fieldXPath)
	for _, n := range codeNodes {
		attrName := ""
		docStr := r.scrapeDocString(n, &attrName, processed)
		if docStr == "" {
			continue
		}
		if r.ArgumentDocs == nil {
			r.ArgumentDocs = make(map[string]string)
		}
		if r.ArgumentDocs[attrName] != "" && r.ArgumentDocs[attrName] != strings.TrimSpace(docStr) {
			conflictedFields[attrName] = true
			continue
		}
		r.ArgumentDocs[attrName] = docStr
	}

	// Remove descriptions for repeating fields in the registry.
	for cf := range conflictedFields {
		delete(r.ArgumentDocs, cf)
	}
}

func getRootPath(n *html.Node) string { // nolint: gocyclo
	var ulNode, pNode, codeNode *html.Node
	for ulNode = n.Parent; ulNode != nil && ulNode.Data != "ul"; ulNode = ulNode.Parent {
	}
	if ulNode == nil {
		return ""
	}
	for pNode = ulNode; pNode != nil && (pNode.Data != "p" || !checkBlockParagraph(pNode)); pNode = pNode.PrevSibling {
	}
	if pNode == nil {
		return ""
	}
	for codeNode = pNode.FirstChild; codeNode != nil && codeNode.Data != "code"; codeNode = codeNode.NextSibling {
	}
	if codeNode == nil || codeNode.FirstChild == nil {
		return ""
	}
	prevLiNode := getPrevLiWithCodeText(codeNode.FirstChild.Data, pNode)
	if prevLiNode == nil {
		return codeNode.FirstChild.Data
	}
	root := getRootPath(prevLiNode)
	if len(root) == 0 {
		return codeNode.FirstChild.Data
	}
	return fmt.Sprintf("%s.%s", root, codeNode.FirstChild.Data)
}

// returns the list item node (in an UL) with a code child with text `codeText`
func getPrevLiWithCodeText(codeText string, pNode *html.Node) *html.Node {
	var ulNode, liNode *html.Node
	for ulNode = pNode.PrevSibling; ulNode != nil && ulNode.Data != "ul"; ulNode = ulNode.PrevSibling {
	}
	if ulNode == nil {
		return nil
	}
	for liNode = ulNode.FirstChild; liNode != nil; liNode = liNode.NextSibling {
		if liNode.Data != "li" || liNode.FirstChild == nil || liNode.FirstChild.Data != "code" || liNode.FirstChild.FirstChild.Data != codeText {
			continue
		}
		return liNode
	}
	return nil
}

func checkBlockParagraph(p *html.Node) bool {
	// traverse children of the paragraph node
	for c := p.FirstChild; c != nil; c = c.NextSibling {
		if regexConfigurationBlock.MatchString(c.Data) {
			return true
		}
	}
	return false
}

func (r *Resource) scrapeDocString(n *html.Node, attrName *string, processed map[*html.Node]struct{}) string {
	if _, ok := processed[n]; ok {
		return ""
	}
	processed[n] = struct{}{}

	if n.Type == html.ElementNode {
		return r.scrapeDocString(n.FirstChild, attrName, processed)
	}

	sb := strings.Builder{}
	if *attrName == "" {
		*attrName = n.Data
		if root := getRootPath(n); len(root) != 0 {
			*attrName = fmt.Sprintf("%s.%s", root, *attrName)
		}
	} else {
		sb.WriteString(n.Data)
	}
	s := n.Parent
	for s = s.NextSibling; s != nil; s = s.NextSibling {
		if _, ok := processed[s]; ok {
			continue
		}
		processed[s] = struct{}{}

		switch s.Type { // nolint:exhaustive
		case html.TextNode:
			sb.WriteString(s.Data)
		case html.ElementNode:
			if s.FirstChild == nil {
				continue
			}
			sb.WriteString(r.scrapeDocString(s.FirstChild, attrName, processed))
		}
	}
	return sb.String()
}

func (r *Resource) scrapeImportStatements(doc *html.Node, importXPath string) {
	nodes := htmlquery.Find(doc, importXPath)
	for _, n := range nodes {
		r.ImportStatements = append(r.ImportStatements, strings.TrimSpace(n.Data))
	}
}

// scrape scrapes resource metadata from the specified HTML doc.
// filename is not always the precise resource name, hence,
// it returns the resource name scraped from the doc.
func (r *Resource) scrape(path string, config *ScrapeConfiguration) error {
	source, err := ioutil.ReadFile(filepath.Clean(path))
	if err != nil {
		return errors.Wrap(err, "failed to read markdown file")
	}

	var buff bytes.Buffer
	if err := goldmark.Convert(source, &buff); err != nil {
		return errors.Wrap(err, "failed to convert markdown")
	}

	doc, err := htmlquery.Parse(&buff)
	if err != nil {
		return errors.Wrap(err, "failed to parse HTML")
	}

	if err := r.scrapePrelude(doc, config.PreludeXPath); err != nil {
		return err
	}

	r.scrapeFieldDocs(doc, config.FieldDocXPath)
	r.scrapeImportStatements(doc, config.ImportXPath)

	return r.scrapeExamples(doc, config.CodeXPath, config.Debug)
}

// ScrapeConfiguration is a configurator for the scraper
type ScrapeConfiguration struct {
	// Debug Output debug messages
	Debug bool
	// RepoPath is the path of the Terraform native provider repo
	RepoPath string
	// CodeXPath Code XPath expression
	CodeXPath string
	// PreludeXPath Prelude XPath expression
	PreludeXPath string
	// FieldDocXPath Field documentation XPath expression
	FieldDocXPath string
	// ImportXPath Import statements XPath expression
	ImportXPath string
}

// ScrapeRepo scrape metadata from the configured Terraform native provider repo
func (pm *ProviderMetadata) ScrapeRepo(config *ScrapeConfiguration) error {
	return errors.Wrap(filepath.WalkDir(config.RepoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "failed to traverse Terraform registry")
		}
		if d.IsDir() || filepath.Ext(d.Name()) != extMarkdown {
			return nil
		}
		r := &Resource{}
		if err := r.scrape(path, config); err != nil {
			return errors.Wrapf(err, "failed to scrape resource metadata from path: %s", path)
		}

		pm.Resources[r.Name] = r
		return nil
	}), "cannot scrape Terraform registry")
}

// Store stores this scraped ProviderMetadata at the specified path
func (pm *ProviderMetadata) Store(path string) error {
	out, err := yaml.Marshal(pm)
	if err != nil {
		return errors.Wrap(err, "failed to marshal provider metadata to YAML")
	}
	return errors.Wrapf(ioutil.WriteFile(path, out, 0600), "failed to write provider metada file: %s", path)
}
