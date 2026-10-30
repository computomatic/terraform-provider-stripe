package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/computomatic/terraform-provider-stripe/internal/engine"
	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/provider"
)

// The generator reads every declaration's `Defaults` from its source, and gets what the engine
// compiled: each rule found is the statement of that package's resource. A package with
// `Defaults` and no resource named after it fails, so none goes unchecked.
func TestDeclaredDefaultsMatchTheDeclarations(t *testing.T) {
	worded, err := declaredDefaults(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(worded) == 0 {
		t.Fatal("no package declares `Defaults`")
	}
	ctx := context.Background()
	compiled := map[string]map[string]engine.AttributeDoc{}
	for _, newResource := range provider.New("dev")().Resources(ctx) {
		r := newResource()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "stripe"}, &meta)
		if docs, ok := r.(interface {
			AttributeDocs() map[string]engine.AttributeDoc
		}); ok {
			compiled[meta.TypeName] = docs.AttributeDocs()
		}
	}
	for pkg, rules := range worded {
		docs, ok := compiled["stripe_"+pkg]
		if !ok {
			t.Errorf("%s declares `Defaults`, but the provider has no resource stripe_%s", pkg, pkg)
			continue
		}
		for path, rule := range rules {
			// The engine adds, after the rule, the value the suite observed where there is one.
			want := fields.Default{Kind: fields.DefaultRule, Rule: rule}.Statement()
			if got := docs[path].Default; got != want && !strings.HasPrefix(got, strings.TrimSuffix(want, ".")+" (") {
				t.Errorf("%s `%s`: the engine states %q, the source reads %q", pkg, path, got, want)
			}
		}
	}
}

// writeResource writes one file of a package under root's internal/resources.
func writeResource(t *testing.T, root, pkg, file, src string) {
	t.Helper()
	dir := filepath.Join(root, "internal", "resources", pkg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A rule the report cannot read must fail generation, not go uncounted.
func TestDeclaredDefaultsRefuseWhatTheyCannotRead(t *testing.T) {
	root := t.TempDir()
	writeResource(t, root, "widget", "resource.go", "package widget\n\nconst rule = \"x\"\n\nvar d = struct{ Defaults map[string]string }{Defaults: map[string]string{\"a\": rule}}\n")
	if _, err := declaredDefaults(root); err == nil {
		t.Error("a rule that is no string literal must fail")
	}
}

// The report keys `Defaults` by package, so a second in one package, in the same file or
// another, must fail, not replace the first.
func TestDeclaredDefaultsRefuseTwoInOnePackage(t *testing.T) {
	const declaration = "var %s = struct{ Defaults map[string]string }{Defaults: map[string]string{\"a\": \"x\"}}\n"
	for name, files := range map[string]map[string]string{
		"one file":  {"resource.go": "package widget\n\n" + strings.ReplaceAll(declaration, "%s", "a") + strings.ReplaceAll(declaration, "%s", "b")},
		"two files": {"resource.go": "package widget\n\n" + strings.ReplaceAll(declaration, "%s", "a"), "datasource.go": "package widget\n\n" + strings.ReplaceAll(declaration, "%s", "b")},
	} {
		root := t.TempDir()
		for file, src := range files {
			writeResource(t, root, "widget", file, src)
		}
		if _, err := declaredDefaults(root); err == nil || !strings.Contains(err.Error(), "second `Defaults`") {
			t.Errorf("%s: got %v, want the second `Defaults` refused", name, err)
		}
	}
}

// A worded argument counts as recorded, with the statement the docs show, and its source says
// so. The report's other arguments are left alone.
func TestDeclaredDefaultsCountAsRecorded(t *testing.T) {
	leaf := func(path string) Key { return Key{Side: SideRequest, Operation: "PostWidgets", Path: path} }
	worded, plain := leaf("worded"), leaf("plain")
	tc := &tableCoverage{
		leaves: []Leaf{{Key: worded, At: "worded"}, {Key: plain, At: "plain"}},
		records: []Record{
			{Table: "widget", Key: worded, State: StateExposed, Class: ClassVerified, Default: &DefaultRecord{Source: "evidence", Varies: true}},
			{Table: "widget", Key: plain, State: StateExposed, Class: ClassVerified, Default: &DefaultRecord{Source: "evidence", Varies: true}},
		},
		table: []fields.Field{
			{Name: "worded", Default: &fields.Default{Kind: fields.DefaultVaries}},
			{Name: "plain", Default: &fields.Default{Kind: fields.DefaultVaries}},
		},
	}
	tc.applyDeclaredDefaults(map[string]string{"worded": "the rule"})
	got := defaultsNotRecorded(tc.records)["widget"]
	if len(got.Verified) != 1 || got.Verified[0] != "plain" {
		t.Errorf("not recorded: %v, want only plain", got.Verified)
	}
	if d := tc.records[0].Default; d.Statement != "Default (Stripe): the rule." || d.Source != "declaration" {
		t.Errorf("worded record: %+v", d)
	}
}
