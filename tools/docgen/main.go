// Command docgen renders the provider's documentation pages from its schema, the examples
// under examples/ and the provider prose under docs-src/. The layout is the one in
// adrs/1-pending/2026-10-22-documentation-layout/ADR.md.
//
//	docgen -root . -out docs
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/provider"
)

// maxPageBytes is the Registry's limit of 500KB per document, read as 500,000 bytes.
// Longer documents are truncated without notice, so the renderer refuses to write one.
const maxPageBytes = 500_000

func main() {
	root := flag.String("root", ".", "repository root, which holds examples/ and docs-src/")
	out := flag.String("out", "", "directory to write the pages to")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: docgen [-root dir] -out dir")
		os.Exit(64)
	}
	if err := run(*root, *out); err != nil {
		fmt.Fprintln(os.Stderr, "docgen:", err)
		os.Exit(1)
	}
}

func run(root, out string) error {
	s, err := loadSchemas(context.Background())
	if err != nil {
		return err
	}
	pages, err := renderAll(s, root)
	if err != nil {
		return err
	}
	return writePages(out, pages)
}

// loadSchemas reads the provider's schemas and attaches the import notes of its
// declarations to the resources.
func loadSchemas(ctx context.Context) (*schemas, error) {
	p := provider.New("dev")()
	server, err := providerserver.NewProtocol6WithError(p)()
	if err != nil {
		return nil, err
	}
	s, err := readSchemas(ctx, server)
	if err != nil {
		return nil, err
	}
	for _, newResource := range p.Resources(ctx) {
		r := newResource()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "stripe"}, &meta)
		noted, ok := r.(interface {
			ImportNote() (string, engine.NoteLevel)
			DestroyNote() string
			MoneyNote() string
			AttributeDocs() map[string]engine.AttributeDoc
		})
		if !ok {
			continue
		}
		for _, rs := range s.Resources {
			if rs.Type == meta.TypeName {
				rs.ImportNote, rs.ImportNoteLevel = noted.ImportNote()
				rs.DestroyNote = noted.DestroyNote()
				rs.MoneyNote = noted.MoneyNote()
				if err := attachDocs(rs, noted.AttributeDocs()); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, newDataSource := range p.DataSources(ctx) {
		d := newDataSource()
		var meta datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "stripe"}, &meta)
		listed, ok := d.(interface {
			ListParamNames() []string
			AttributeDocs() map[string]engine.AttributeDoc
		})
		if !ok {
			continue
		}
		for _, ds := range s.DataSources {
			if ds.Type == meta.TypeName {
				ds.ListParams = listed.ListParamNames()
				if err := attachDocs(ds, listed.AttributeDocs()); err != nil {
					return nil, err
				}
			}
		}
	}
	for name, d := range provider.ArgumentDefaults() {
		a := at(s.Provider.Attrs, name)
		if a == nil {
			return nil, fmt.Errorf("provider: a default for %s, which is no argument", name)
		}
		a.Default = d.Statement()
	}
	return s, nil
}

// attachDocs puts what the engine says about each attribute on the page model.
func attachDocs(s *schema, docs map[string]engine.AttributeDoc) error {
	for path, d := range docs {
		a := at(s.Attrs, path)
		if a == nil {
			return fmt.Errorf("%s: the engine documents %s, which the schema does not have", s.Type, path)
		}
		a.Default, a.NeedsDefault, a.Removal, a.Refused, a.Copy = d.Default, d.NeedsDefault, d.Removal, d.Refused, d.Copy
	}
	return nil
}

// renderAll renders every page, keyed by its path relative to the output directory. It fails on
// an optional argument without the default statement it must have.
func renderAll(s *schemas, root string) (map[string]string, error) {
	if err := checkDefaults(s, everyArgumentHasADefault); err != nil {
		return nil, err
	}
	dataSources := map[string]*schema{}
	for _, d := range s.DataSources {
		dataSources[d.Type] = d
	}
	pages := map[string]string{}
	add := func(fitted map[string]string) error {
		for path, page := range fitted {
			if _, dup := pages[path]; dup {
				return fmt.Errorf("two pages at %s", path)
			}
			pages[path] = page
		}
		return nil
	}
	for _, r := range s.Resources {
		ex, err := readExamples(filepath.Join(root, "examples", "resources", r.Type), "resource")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.Type, err)
		}
		imp, err := readImport(filepath.Join(root, "examples", "resources", r.Type, "import.sh"), r)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.Type, err)
		}
		fitted, err := fit(r, "resources", pageInput{Examples: ex, Import: imp, DataSources: dataSources}, func(s *schema, in pageInput) (string, error) {
			return renderResource(s, in, engine.DefaultOperationTimeout)
		}, pageBudget)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.Type, err)
		}
		if err := add(fitted); err != nil {
			return nil, err
		}
	}
	for _, d := range s.DataSources {
		ex, err := readExamples(filepath.Join(root, "examples", "data-sources", d.Type), "data-source")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.Type, err)
		}
		fitted, err := fit(d, "data-sources", pageInput{Examples: ex, DataSources: dataSources}, renderDataSource, pageBudget)
		if err != nil {
			return nil, fmt.Errorf("%s data source: %w", d.Type, err)
		}
		if err := add(fitted); err != nil {
			return nil, err
		}
	}
	prose, err := readProviderProse(filepath.Join(root, "docs-src", "provider.md"))
	if err != nil {
		return nil, err
	}
	ex, err := readExamples(filepath.Join(root, "examples", "provider"), "provider")
	if err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	if pages["index.md"], err = renderProvider(s.Provider, prose, ex); err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	guides, err := readGuides(filepath.Join(root, "docs-src", "guides"))
	if err != nil {
		return nil, err
	}
	table, err := permissionTable(s, permissions)
	if err != nil {
		return nil, err
	}
	placed := false
	for path, page := range guides {
		if strings.Contains(page, permissionsMarker) {
			page = strings.ReplaceAll(page, permissionsMarker, table)
			placed = true
		}
		if _, dup := pages[path]; dup {
			return nil, fmt.Errorf("the guide %s has the name of a generated guide", path)
		}
		pages[path] = page
	}
	if !placed {
		return nil, fmt.Errorf("no guide holds %s, so the permissions table has no page", permissionsMarker)
	}
	for path, page := range pages {
		if len(page) > maxPageBytes {
			return nil, fmt.Errorf("%s is %d bytes, over the Registry limit of %d", path, len(page), maxPageBytes)
		}
	}
	return pages, nil
}

func pagePath(dir, typeName string) string {
	return filepath.Join(dir, strings.TrimPrefix(typeName, "stripe_")+".md")
}

// readGuides reads the hand-written guides, keyed by their path under docs/. A guide is copied
// as it is, but for the permissions table renderAll puts in place of permissionsMarker, and must open with front matter that gives its `page_title`, which the Registry
// requires of guides (documentation layout ADR, D3a).
func readGuides(dir string) (map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	guides := map[string]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(string(data), "---\npage_title: ") {
			return nil, fmt.Errorf("guide %s must open with front matter whose first line is page_title", filepath.Base(f))
		}
		guides[filepath.Join("guides", filepath.Base(f))] = string(data)
	}
	return guides, nil
}

// writePages writes the pages and deletes any other page under resources/, data-sources/ and
// guides/.
func writePages(out string, pages map[string]string) error {
	for path, page := range pages {
		full := filepath.Join(out, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(page), 0o644); err != nil {
			return err
		}
	}
	for _, dir := range []string{"resources", "data-sources", "guides"} {
		entries, err := os.ReadDir(filepath.Join(out, dir))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, e := range entries {
			if _, ok := pages[filepath.Join(dir, e.Name())]; !ok {
				if err := os.RemoveAll(filepath.Join(out, dir, e.Name())); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// readExamples reads <prefix>.tf and the variants <prefix>*.tf. A variant starts with a
// comment line `# Title`, which becomes its heading.
func readExamples(dir, prefix string) ([]example, error) {
	main, err := os.ReadFile(filepath.Join(dir, prefix+".tf"))
	if err != nil {
		return nil, err
	}
	out := []example{{Code: string(main)}}
	variants, err := filepath.Glob(filepath.Join(dir, prefix+"*.tf"))
	if err != nil {
		return nil, err
	}
	sort.Strings(variants)
	for _, path := range variants {
		if filepath.Base(path) == prefix+".tf" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		first, rest, _ := strings.Cut(string(data), "\n")
		title, ok := strings.CutPrefix(first, "# ")
		if !ok {
			return nil, fmt.Errorf("%s: the first line must be a comment `# Title`", path)
		}
		out = append(out, example{Title: strings.TrimSpace(title), Code: rest})
	}
	return out, nil
}

var importCommand = regexp.MustCompile(`^terraform import (\S+) (\S+)$`)

// readImport reads import.sh: optional comment lines, then `terraform import <type>.<name> <id>`.
// A resource with no `id` attribute is a singleton, whose ID is the constant `default`.
func readImport(path string, s *schema) (*importExample, error) {
	if s.find("id") == nil {
		return &importExample{Name: "example", ID: "default"}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	imp := &importExample{}
	var lead []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if c, ok := strings.CutPrefix(l, "# "); ok {
			lead = append(lead, c)
			continue
		}
		m := importCommand.FindStringSubmatch(l)
		if m == nil {
			return nil, fmt.Errorf("%s: want `terraform import <type>.<name> <id>`, got %q", path, l)
		}
		typ, name, ok := strings.Cut(m[1], ".")
		if !ok || typ != s.Type {
			return nil, fmt.Errorf("%s: address %q is not of type %s", path, m[1], s.Type)
		}
		imp.Name, imp.ID = name, m[2]
	}
	if imp.ID == "" {
		return nil, fmt.Errorf("%s: no import command", path)
	}
	imp.Lead = strings.Join(lead, " ")
	return imp, nil
}

// readProviderProse reads the provider page's text: front matter, then a lead, then the
// `##` sections that follow the example.
func readProviderProse(path string) (providerProse, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return providerProse{}, err
	}
	text := string(data)
	fm, body, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !strings.HasPrefix(text, "---\n") || !ok {
		return providerProse{}, fmt.Errorf("%s: no front matter", path)
	}
	lead, sections, ok := strings.Cut(body, "\n## ")
	if !ok {
		return providerProse{}, fmt.Errorf("%s: no `##` section", path)
	}
	return providerProse{
		FrontMatter: "---\n# generated by tools/docgen\n" + fm + "\n---",
		Lead:        lead,
		Sections:    "## " + sections,
	}, nil
}
