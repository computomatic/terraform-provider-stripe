package engine

import (
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// docsTable is the fixture table with the evidence's defaults, an embedded copy and a list
// Stripe returns a page at a time.
func docsTable() []fields.Field {
	table := fixtureTable()
	for i := range table {
		switch table[i].Name {
		case "kind":
			table[i].Default, table[i].Verified = &fields.Default{Kind: fields.DefaultValue, Value: "a"}, true
		case "quantity":
			table[i].Default, table[i].Verified = &fields.Default{Kind: fields.DefaultVaries}, true
		case "ratio":
			table[i].Verified = true
		case "address":
			table[i].Fields[1].Default = &fields.Default{Kind: fields.DefaultRule, Rule: "the account's postal code", Observed: "`94103` on a US account"}
		}
	}
	return append(table,
		fields.Field{Name: "last_charge", Shape: fields.Object, Response: true, Copy: "stripe_charge", Fields: []fields.Field{
			{Name: "amount", Shape: fields.Int, Response: true},
		}},
		fields.Field{Name: "lines", Shape: fields.Object, Response: true, PagedBy: "stripe_widget_line", Description: "The lines.", Fields: []fields.Field{
			{Name: "has_more", Shape: fields.Bool, Response: true},
		}})
}

// The page of an optional argument states its default: the evidence's, or the declaration's
// wording of a rule the evidence records. A verified argument, and one only the update takes, is
// marked so that docgen refuses it without a statement, and an embedded copy names the data
// source of the copied object.
func TestAttributeDocs(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Defaults = map[string]string{"quantity": "one per line of the parent"}
	ctor, err := NewResource(decl, docsTable())
	if err != nil {
		t.Fatal(err)
	}
	r := ctor().(*Resource)
	got := r.AttributeDocs()
	for path, want := range map[string]AttributeDoc{
		"kind":          {Default: "Default (Stripe): `a`.", NeedsDefault: true},
		"quantity":      {Default: "Default (Stripe): one per line of the parent.", NeedsDefault: true},
		"ratio":         {NeedsDefault: true},
		"address.zip":   {Default: "Default (Stripe): the account's postal code (`94103` on a US account)."},
		"last_charge":   {Copy: "stripe_charge"},
		"name":          {},
		"lines":         {},
		"address.line1": {},
		"default_ref":   {NeedsDefault: true},
	} {
		if got[path] != want {
			t.Errorf("%s: %+v, want %+v", path, got[path], want)
		}
	}

	ds, err := NewDataSource(decl, docsTable())
	if err != nil {
		t.Fatal(err)
	}
	dsDocs := ds().(*DataSource).AttributeDocs()
	if len(dsDocs) != 1 || dsDocs["last_charge"] != (AttributeDoc{Copy: "stripe_charge"}) {
		t.Errorf("data source: %+v, want only the copy", dsDocs)
	}

	const paged = "The lines. Holds the first page of the list Stripe returns with the object; `has_more` is true when there are more. Read every element with the `stripe_widget_line` data source."
	if d := r.m.describeResource(kidNamed(&node{Kids: r.m.nodes}, "lines")); d != paged {
		t.Errorf("lines: %q", d)
	}
}

// A declaration words only a rule the evidence records, and only for a field the table has.
func TestDeclarationDefaultsAreChecked(t *testing.T) {
	for path, want := range map[string]string{
		"kind":   "words a rule the evidence does not record",
		"active": "words a rule the evidence does not record",
		"colour": "names no field",
	} {
		decl := fixtureDeclaration()
		decl.Defaults = map[string]string{path: "a rule"}
		if _, err := NewResource(decl, docsTable()); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", path, err, want)
		}
	}
}
