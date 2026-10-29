package main

import (
	"context"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
// singleton and a data source with lookup keys. withDefaults gives it the default statements of
// the full-attribute-coverage ADR's D4, one of each form, and an embedded copy.
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
				nested("last_part", "The part attached last.", tfprotov6.SchemaObjectNestingModeSingle, computed,
					str("label", "Label of the part.", computed),
					str("object", "The name of the object's type.", computed)),
				str("tier", "The pricing tier", optional),
				str("reference", "A reference only an update sets.", optional),
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
				str("label", "Label of the part.", computed),
				str("revision", "The revision whose parts to read. Defaults to the gadget's latest.", optionalComputed)),
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

// withDefaults puts on the fixture what the engine and the provider say about its attributes
// (ADR 2026-10-29-full-attribute-coverage, D3 and D4): a value Stripe applies, a rule Stripe
// applies with the value the suite observed, none, not sent, and the provider's own value and
// rule; and an embedded copy of a gadget part.
func withDefaults(t *testing.T, s *schemas) *schemas {
	t.Helper()
	for typeName, docs := range map[string]map[string]engine.AttributeDoc{
		"widget_gadget": {
			"note":                        {Default: "Default (Stripe): `draft`.", NeedsDefault: true},
			"tier":                        {Default: "Default (Stripe): the account's default tier (`basic` on a US account).", NeedsDefault: true},
			"legacy":                      {Default: "Default: none.", NeedsDefault: true},
			"reference":                   {Default: "Default: not sent.", NeedsDefault: true},
			"parts.options.finish.colour": {Default: "Default: none.", NeedsDefault: true},
			"last_part":                   {Copy: "widget_gadget_part"},
		},
	} {
		if err := attachDocs(byType(s.Resources, typeName), docs); err != nil {
			t.Fatal(err)
		}
	}
	at(s.Provider.Attrs, "api_key").Default = "Default (provider): the `WIDGET_API_KEY` environment variable."
	at(s.Provider.Attrs, "base_url").Default = "Default (provider): `https://api.example.com`."
	return s
}

// dataSources indexes the fixture's data sources, which an embedded copy links to.
func dataSources(s *schemas) map[string]*schema {
	out := map[string]*schema{}
	for _, d := range s.DataSources {
		out[d.Type] = d
	}
	return out
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
	s := withDefaults(t, fixture(t))
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
		Examples:    ex,
		Import:      &importExample{Name: "main", ID: "gad_123"},
		DataSources: dataSources(s),
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

	byType(s.DataSources, "widget_gadget_part").ListParams = []string{"revision"}
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
	split := 0
	for path := range all {
		if _, written := guides[path]; strings.HasPrefix(path, "guides/") && !written {
			split++
		}
	}
	if len(all) != len(real.Resources)+len(real.DataSources)+len(guides)+split+1 {
		t.Errorf("got %d pages for %d resources, %d data sources, %d guides, %d split guides and the index", len(all), len(real.Resources), len(real.DataSources), len(guides), split)
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

// Every shipped type needs a permissions entry, and an entry with no permission for a part of
// its type must say which endpoints the reference leaves out.
func TestPermissionTable(t *testing.T) {
	s := &schemas{
		Resources:   []*schema{{Type: "stripe_widget", MoneyNote: "Moves money."}},
		DataSources: []*schema{{Type: "stripe_widget"}, {Type: "stripe_gauge"}},
	}
	entries := map[string]permission{
		"stripe_widget": {Read: []string{"widget_read"}, Write: []string{"widget_write"}},
		"stripe_gauge":  {Unverified: []string{"GET /v1/gauges/{id}"}},
	}
	got, err := permissionTable(s, entries)
	if err != nil {
		t.Fatal(err)
	}
	want := "| Type | Data source | Resource | Notes |\n| --- | --- | --- | --- |\n" +
		"| `stripe_gauge` | Unverified | No resource | Unverified: the reference does not list `GET /v1/gauges/{id}`. |\n" +
		"| `stripe_widget` | `widget_read` | `widget_write`, `balance_read` | `balance_read` is for the live-mode check. |"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	for name, bad := range map[string]map[string]permission{
		"missing type":       {"stripe_widget": entries["stripe_widget"]},
		"unshipped type":     {"stripe_widget": entries["stripe_widget"], "stripe_gauge": entries["stripe_gauge"], "stripe_gone": {Read: []string{"gone_read"}}},
		"silent gap":         {"stripe_widget": {Read: []string{"widget_read"}}, "stripe_gauge": entries["stripe_gauge"]},
		"write, no resource": {"stripe_widget": entries["stripe_widget"], "stripe_gauge": {Read: []string{"gauge_read"}, Write: []string{"gauge_write"}}},
	} {
		if _, err := permissionTable(s, bad); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// A page over the budget moves its largest top-level nested block to a guide page of its own,
// which keeps each argument's default statement and is linked from the block's line.
func TestGuideSplit(t *testing.T) {
	s := withDefaults(t, fixture(t))
	gadget := byType(s.Resources, "widget_gadget")
	in := pageInput{
		Examples:    []example{{Code: "resource \"widget_gadget\" \"main\" {\n  name = \"Main\"\n}\n"}},
		Import:      &importExample{Name: "main", ID: "gad_123"},
		DataSources: dataSources(s),
	}
	render := func(s *schema, in pageInput) (string, error) { return renderResource(s, in, 20*time.Minute) }
	whole, err := render(gadget, in)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := fit(gadget, "resources", in, render, len(whole)-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("got pages %v, want the page and one guide", slices.Collect(maps.Keys(pages)))
	}
	golden(t, "resource_split", pages[filepath.Join("resources", "widget_gadget.md")])
	golden(t, "guide_split", pages[filepath.Join("guides", "widget_gadget-resource-parts.md")])

	if _, err := fit(gadget, "resources", in, render, 100); err == nil {
		t.Error("a page over the budget with every block moved must fail")
	}
}

// An optional argument the evidence verifies, and every argument of the provider, must state a
// default; in the total mode every optional argument must, but for a nested object with members.
func TestDefaultsAreChecked(t *testing.T) {
	if err := checkDefaults(withDefaults(t, fixture(t)), false); err != nil {
		t.Errorf("complete fixture: %v", err)
	}
	s := withDefaults(t, fixture(t))
	at(byType(s.Resources, "widget_gadget").Attrs, "tier").Default = ""
	at(s.Provider.Attrs, "base_url").Default = ""
	err := checkDefaults(s, false)
	if err == nil || !strings.Contains(err.Error(), "resources/widget_gadget.md: tier") || !strings.Contains(err.Error(), "index.md: base_url") {
		t.Errorf("err = %v, want it to name both arguments", err)
	}
	err = checkDefaults(withDefaults(t, fixture(t)), true)
	want := "optional arguments without a default statement (ADR 2026-10-29-full-attribute-coverage, D4):\n" +
		"  resources/widget_gadget.md: password\n  resources/widget_gadget.md: token"
	if err == nil || err.Error() != want {
		t.Errorf("total: err = %v, want %q", err, want)
	}
}

// An embedded copy links to the data source of the copied object only where that data source
// has every attribute the copy has.
func TestCopiesLinkOnlyToADataSourceThatHoldsThem(t *testing.T) {
	ds := []*attr{{Name: "label"}, {Name: "size", Nesting: nestSingle, Children: []*attr{{Name: "width"}}}}
	for name, copied := range map[string]struct {
		attrs []*attr
		want  bool
	}{
		"subset":       {[]*attr{{Name: "label"}}, true},
		"nested":       {[]*attr{{Name: "size", Nesting: nestSingle, Children: []*attr{{Name: "width"}}}}, true},
		"missing":      {[]*attr{{Name: "colour"}}, false},
		"nested extra": {[]*attr{{Name: "size", Nesting: nestSingle, Children: []*attr{{Name: "depth"}}}}, false},
		"shape":        {[]*attr{{Name: "label", Nesting: nestSingle}}, false},
	} {
		if got := holds(ds, copied.attrs); got != copied.want {
			t.Errorf("%s: holds %v, want %v", name, got, copied.want)
		}
	}
}
