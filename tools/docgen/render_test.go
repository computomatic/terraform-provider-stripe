package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func str(name, desc string, set func(*tfprotov6.SchemaAttribute)) *tfprotov6.SchemaAttribute {
	a := &tfprotov6.SchemaAttribute{Name: name, Type: tftypes.String, Description: desc}
	set(a)
	return a
}

func required(a *tfprotov6.SchemaAttribute) { a.Required = true }
func optional(a *tfprotov6.SchemaAttribute) { a.Optional = true }
func computed(a *tfprotov6.SchemaAttribute) { a.Computed = true }

func optionalComputed(a *tfprotov6.SchemaAttribute) { a.Optional, a.Computed = true, true }

func nested(name, desc string, mode tfprotov6.SchemaObjectNestingMode, set func(*tfprotov6.SchemaAttribute), children ...*tfprotov6.SchemaAttribute) *tfprotov6.SchemaAttribute {
	a := &tfprotov6.SchemaAttribute{
		Name:        name,
		Description: desc,
		NestedType:  &tfprotov6.SchemaObject{Nesting: mode, Attributes: children},
	}
	set(a)
	return a
}

func timeoutsAttr() *tfprotov6.SchemaAttribute {
	return nested("timeouts", "", tfprotov6.SchemaObjectNestingModeSingle, optional,
		str("create", "", optional), str("update", "", optional), str("delete", "", optional))
}

func block(desc string, attrs ...*tfprotov6.SchemaAttribute) *tfprotov6.Schema {
	return &tfprotov6.Schema{Block: &tfprotov6.SchemaBlock{Description: desc, Attributes: attrs}}
}

func identity(attrs ...*tfprotov6.ResourceIdentitySchemaAttribute) *tfprotov6.ResourceIdentitySchema {
	return &tfprotov6.ResourceIdentitySchema{IdentityAttributes: attrs}
}

func idAttr(name string) *tfprotov6.ResourceIdentitySchemaAttribute {
	return &tfprotov6.ResourceIdentitySchemaAttribute{Name: name, Type: tftypes.String, RequiredForImport: true}
}

// fixture covers every shape of D1 to D4: required and optional arguments, a sensitive and a
// write-only one, nesting at three depths, a read-only nested attribute, a child resource, a
// singleton and a data source with lookup keys.
func fixture(t *testing.T) *schemas {
	t.Helper()
	resp := &tfprotov6.GetProviderSchemaResponse{
		Provider: block("Manages widgets. Every request is sent with API version `v1`.",
			str("api_key", "A secret key. Defaults to the `WIDGET_API_KEY` environment variable.", func(a *tfprotov6.SchemaAttribute) { a.Optional, a.Sensitive = true, true }),
			str("base_url", "Base URL of the API.", optional)),
		ResourceSchemas: map[string]*tfprotov6.Schema{
			"widget_gadget": block("A gadget. `terraform destroy` deletes it.",
				str("id", "Unique identifier for the object.", computed),
				str("name", "Name of the gadget.", required),
				str("note", "A note. Changing it replaces the resource.", optionalComputed),
				str("token", "A token that stays in state.", func(a *tfprotov6.SchemaAttribute) { a.Optional, a.Sensitive = true, true }),
				str("password", "A password the provider never stores.", func(a *tfprotov6.SchemaAttribute) { a.Optional, a.WriteOnly = true, true }),
				str("legacy", "An old field.", func(a *tfprotov6.SchemaAttribute) { a.Optional, a.Deprecated = true, true }),
				str("status", "The status of the gadget.", computed),
				nested("parts", "The parts of the gadget.", tfprotov6.SchemaObjectNestingModeList, optional,
					str("label", "Label of the part.", required),
					str("serial", "Serial number, set by the API.", computed),
					nested("options", "Options of the part.", tfprotov6.SchemaObjectNestingModeMap, optional,
						str("size", "Size of the option.", required),
						nested("finish", "Finish of the option.", tfprotov6.SchemaObjectNestingModeSingle, optional,
							str("colour", "Colour of the finish.", optional))),
				),
				nested("history", "What happened to the gadget.", tfprotov6.SchemaObjectNestingModeSet, computed,
					str("at", "When it happened.", computed),
					str("secret", "A secret detail.", func(a *tfprotov6.SchemaAttribute) { a.Computed, a.Sensitive = true, true })),
				timeoutsAttr()),
			"widget_gadget_part": block("A part attached to a gadget.",
				str("id", "Unique identifier for the object.", computed),
				str("gadget", "The ID of the parent object.", required)),
			"widget_settings": block("The account's settings.",
				str("theme", "The theme.", required)),
		},
		DataSourceSchemas: map[string]*tfprotov6.Schema{
			"widget_gadget": block("A gadget.",
				str("id", "Unique identifier for the object.", optionalComputed),
				str("lookup_key", "Lookup key of the gadget.", optionalComputed),
				str("name", "Name of the gadget.", computed),
				nested("parts", "The parts of the gadget.", tfprotov6.SchemaObjectNestingModeList, computed,
					str("label", "Label of the part.", computed))),
			"widget_gadget_part": block("A part attached to a gadget.",
				str("gadget", "The ID of the parent object.", required),
				str("id", "The ID of the gadget part to read.", optionalComputed),
				str("label", "Label of the part.", computed)),
			"widget_settings": block("The account's settings.",
				str("theme", "The theme.", computed)),
		},
	}
	ids := &tfprotov6.GetResourceIdentitySchemasResponse{IdentitySchemas: map[string]*tfprotov6.ResourceIdentitySchema{
		"widget_gadget":      identity(idAttr("id")),
		"widget_gadget_part": identity(idAttr("gadget"), idAttr("id")),
	}}
	s, err := convert(resp, ids)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func byType(list []*schema, typeName string) *schema {
	for _, s := range list {
		if s.Type == typeName {
			return s
		}
	}
	return nil
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.md")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s (run with -update to rewrite it)\n--- got\n%s", name, path, got)
	}
}

func TestGoldenPages(t *testing.T) {
	s := fixture(t)
	timeout := 20 * time.Minute
	ex := []example{
		{Code: "resource \"widget_gadget\" \"main\" {\n  name = \"Main\"\n}\n"},
		{Title: "With parts", Code: "resource \"widget_gadget\" \"parts\" {\n  name = \"Parts\"\n}\n"},
	}
	byType(s.Resources, "widget_gadget").ImportNote = "An imported gadget has no `token`."
	byType(s.Resources, "widget_gadget").ImportNoteLevel = engine.NoteWarning
	byType(s.Resources, "widget_gadget").MoneyNote = "A gadget can move real money. In a live-mode account, creating or changing one fails at plan time."
	byType(s.Resources, "widget_gadget").DestroyNote = "Gadgets cannot be deleted. `terraform destroy`, and a change that would replace the gadget, fail at plan time. To stop managing a gadget, remove it from the state as [Removing from State](#removing-from-state) shows; it stays in the account."
	byType(s.Resources, "widget_gadget_part").ImportNote = "Parts are matched by serial."
	gadget, err := renderResource(byType(s.Resources, "widget_gadget"), pageInput{
		Examples: ex,
		Import:   &importExample{Name: "main", ID: "gad_123"},
	}, timeout)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "resource", gadget)

	child, err := renderResource(byType(s.Resources, "widget_gadget_part"), pageInput{
		Examples: ex[:1],
		Import:   &importExample{Lead: "The ID is the gadget ID and the part ID, separated by a slash.", Name: "main", ID: "gad_123/part_456"},
	}, timeout)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "resource_child", child)

	singleton, err := renderResource(byType(s.Resources, "widget_settings"), pageInput{
		Examples: ex[:1],
		Import:   &importExample{Name: "example", ID: "default"},
	}, timeout)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "resource_singleton", singleton)

	ds, err := renderDataSource(byType(s.DataSources, "widget_gadget"), pageInput{Examples: ex[:1]})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "data_source", ds)

	dsChild, err := renderDataSource(byType(s.DataSources, "widget_gadget_part"), pageInput{Examples: ex[:1]})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "data_source_child", dsChild)

	dsSingleton, err := renderDataSource(byType(s.DataSources, "widget_settings"), pageInput{Examples: ex[:1]})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "data_source_singleton", dsSingleton)

	prose, err := readProviderProse(filepath.Join("testdata", "provider.md"))
	if err != nil {
		t.Fatal(err)
	}
	index, err := renderProvider(s.Provider, prose, []example{{Code: "provider \"widget\" {}\n"}})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "index", index)
}

func TestConvertRejectsUnknownNesting(t *testing.T) {
	resp := &tfprotov6.GetProviderSchemaResponse{
		Provider: block("p"),
		ResourceSchemas: map[string]*tfprotov6.Schema{
			"widget_x": block("x", nested("odd", "", tfprotov6.SchemaObjectNestingMode(99), optional)),
		},
	}
	if _, err := convert(resp, &tfprotov6.GetResourceIdentitySchemasResponse{}); err == nil {
		t.Fatal("want an error for a nesting mode the renderer does not know")
	}
}

// A guide is copied as written, and one without a page title is refused.
func TestGuidesAreCopied(t *testing.T) {
	dir := t.TempDir()
	guide := "---\npage_title: \"Managing Widgets\"\n---\n\n# Managing Widgets\n"
	if err := os.WriteFile(filepath.Join(dir, "widgets.md"), []byte(guide), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readGuides(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[filepath.Join("guides", "widgets.md")] != guide {
		t.Errorf("guides: %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "untitled.md"), []byte("# Untitled\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readGuides(dir); err == nil {
		t.Error("a guide without a page title must be refused")
	}
}

func TestStalePagesAreDeleted(t *testing.T) {
	out := t.TempDir()
	stale := []string{filepath.Join(out, "resources", "gone.md"), filepath.Join(out, "guides", "gone.md")}
	for _, path := range stale {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writePages(out, map[string]string{filepath.Join("resources", "kept.md"): "new\n"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range stale {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("stale page %s still exists: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "resources", "kept.md")); err != nil {
		t.Error(err)
	}
}

// Half the Registry's limit, so that growth is caught long before it truncates a page.
const pageBudget = 250_000

func TestPagesStayUnderBudget(t *testing.T) {
	check := func(name string, pages map[string]string) {
		for path, page := range pages {
			if len(page) > pageBudget {
				t.Errorf("%s: %s is %d bytes, over the budget of %d (the Registry truncates at 500KB)", name, path, len(page), pageBudget)
			}
		}
	}

	// The fixture.
	page, err := renderResource(byType(fixture(t).Resources, "widget_gadget"), pageInput{
		Examples: []example{{Code: "x\n"}},
		Import:   &importExample{Name: "main", ID: "a"},
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	check("fixture", map[string]string{"widget_gadget": page})

	// The real provider.
	real, err := loadSchemas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	all, err := renderAll(real, filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	guides, err := readGuides(filepath.Join("..", "..", "docs-src", "guides"))
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(real.Resources)+len(real.DataSources)+len(guides)+1 {
		t.Errorf("got %d pages for %d resources, %d data sources, %d guides and the index", len(all), len(real.Resources), len(real.DataSources), len(guides))
	}
	check("provider", all)
}

var terraformFence = regexp.MustCompile("(?s)```terraform\n(.*?)```")

// Every HCL sample in an Import section is what `terraform fmt` writes. `terraform fmt`
// aligns the `=` of consecutive single-line attributes only, so `to` and a multi-line
// `identity = {` stay apart while the attributes inside the identity line up.
func TestImportSamplesAreFormatted(t *testing.T) {
	check := func(name, page string) {
		_, imp, ok := strings.Cut(page, "\n## Import\n")
		if !ok {
			t.Errorf("%s: no Import section", name)
			return
		}
		for _, m := range terraformFence.FindAllStringSubmatch(imp, -1) {
			if got := string(hclwrite.Format([]byte(m[1]))); got != m[1] {
				t.Errorf("%s: sample is not formatted\n--- got\n%s--- want\n%s", name, m[1], got)
			}
		}
	}
	for _, name := range []string{"resource", "resource_child", "resource_singleton"} {
		page, err := os.ReadFile(filepath.Join("testdata", name+".golden.md"))
		if err != nil {
			t.Fatal(err)
		}
		check(name, string(page))
	}
	real, err := loadSchemas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	all, err := renderAll(real, filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for path, page := range all {
		if strings.HasPrefix(path, "resources") {
			check(path, page)
		}
	}
}

// A data source destroys, replaces and imports nothing, so its lead must not say what a
// resource does (Declaration.DataSourceDescription).
func TestDataSourceLeadsHaveNoLifecycleSentences(t *testing.T) {
	real, err := loadSchemas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range real.DataSources {
		for _, word := range []string{"terraform destroy", "replaces", "imported", "Importing", "Import with"} {
			if strings.Contains(d.Description, word) {
				t.Errorf("%s: the lead mentions %q: %s", d.Type, word, d.Description)
			}
		}
	}
}
