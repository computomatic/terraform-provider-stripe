package engine

import (
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// docsTable is the fixture table with the evidence's defaults, an embedded copy, a list Stripe
// returns a page at a time, and IDs of objects Stripe returns whole (rule X7).
func docsTable() []fields.Field {
	table := fixtureTable()
	for i := range table {
		switch table[i].Name {
		case "kind":
			table[i].Default = &fields.Default{Kind: fields.DefaultValue, Value: "a"}
		case "quantity":
			table[i].Default = &fields.Default{Kind: fields.DefaultVaries}
		case "ratio":
			table[i].ServerDefaulted, table[i].Stable = true, true
		case "items":
			table[i].Fields[1].ServerDefaulted, table[i].Fields[1].Stable = true, true
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
		}},
		fields.Field{Name: "price", Shape: fields.String, Response: true, ReadBy: "stripe_widget_price", Description: "The price."},
		fields.Field{Name: "tax_rates", Shape: fields.ScalarList, Elem: fields.String, Response: true, ReadBy: "stripe_tax_rate", Description: "The tax rates."},
		fields.Field{Name: "referrer", Shape: fields.String, Create: true, NotReturned: true},
		fields.Field{Name: "display", Shape: fields.String, Create: true, Response: true, NotReturned: true})
}

// The page of an optional argument states its default: the evidence's, or the declaration's
// wording of a rule the evidence records. An Optional and Computed argument says what removing it
// does, as the plan treats it: one the plan keeps (a stable one, E5) leaves the current value,
// one in a list element, stable or not, Stripe sets when the list is sent again, and any other
// the provider stops sending. An argument only the update takes is marked so that docgen
// refuses it without a statement, and an embedded copy names the data source of the copied
// object. An argument Stripe never returns says the provider keeps it from configuration, which
// it does as PreserveConfig keeps a value, and one the response holds says why it is kept.
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
		"kind":          {Default: "Default (Stripe): `a`."},
		"quantity":      {Default: "Default (Stripe): one per line of the parent."},
		"ratio":         {Removal: RemovalKeeps},
		"id":            {Removal: RemovalKeeps},
		"active":        {Removal: RemovalNotSent},
		"items.qty":     {Removal: RemovalInList},
		"address.zip":   {Default: "Default (Stripe): the account's postal code (`94103` on a US account)."},
		"last_charge":   {Copy: "stripe_charge"},
		"name":          {},
		"lines":         {},
		"address.line1": {},
		"default_ref":   {NeedsDefault: true, Removal: RemovalNotSent},
		"referrer":      {NotReturned: NotReturnedDoc},
		"display":       {NotReturned: NotProvenDoc},
	} {
		if got[path] != want {
			t.Errorf("%s: %+v, want %+v", path, got[path], want)
		}
	}

	if n, ok := r.m.top("referrer"); !ok || !n.Preserve {
		t.Errorf("referrer is not kept from configuration: %+v", n)
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

	for name, want := range map[string]string{
		"price":     "The price. Stripe returns the whole widget price here; the provider holds its ID. Read the widget price with the `stripe_widget_price` data source.",
		"tax_rates": "The tax rates. Stripe returns each tax rate whole here; the provider holds their IDs. Read each with the `stripe_tax_rate` data source.",
	} {
		n := kidNamed(&node{Kids: r.m.nodes}, name)
		if d := r.m.describeResource(n); d != want {
			t.Errorf("%s on the resource: %q", name, d)
		}
		if d := describe(n); d != want {
			t.Errorf("%s on the data source: %q", name, d)
		}
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

// An argument the evidence records Stripe taking in some requests and refusing in others needs
// the declaration's wording of where Stripe takes it, which its page gives before the recorded
// refusals, and the wording needs such an argument.
func TestDeclarationAnswers(t *testing.T) {
	const refused = "Elsewhere Stripe refused it on a US account on create: \"No.\"."
	answered := func() []fields.Field {
		table := docsTable()
		for i := range table {
			if table[i].Name == "kind" {
				table[i].Answer = refused
			}
		}
		return table
	}
	decl := fixtureDeclaration()
	decl.Answers = map[string]string{"kind": "Stripe takes it beside a `name`."}
	ctor, err := NewResource(decl, answered())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ctor().(*Resource).AttributeDocs()["kind"].Answer, "Stripe takes it beside a `name`. "+refused; got != want {
		t.Errorf("kind: %q, want %q", got, want)
	}
	for name, tc := range map[string]struct {
		answers map[string]string
		table   []fields.Field
		want    string
	}{
		"no wording": {nil, answered(), "needs an answers entry"},
		"no answers": {map[string]string{"kind": "a rule"}, docsTable(), "records no answers"},
		"no field":   {map[string]string{"colour": "a rule"}, docsTable(), "names no field"},
	} {
		decl := fixtureDeclaration()
		decl.Answers = tc.answers
		if _, err := NewResource(decl, tc.table); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

// A data source holds a field the resource takes in another shape in the response's, so the
// answers worded for the request's members compile into the resource alone.
func TestDataSourceKeepsTheAnswersOfItsFields(t *testing.T) {
	table := []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "code", Shape: fields.String, Create: true, Response: true, Answer: "Elsewhere Stripe refused it."},
		{Name: "discounts", Shape: fields.ObjectList, Create: true, Fields: []fields.Field{
			{Name: "coupon", Shape: fields.String, Create: true, Answer: "Elsewhere Stripe refused it."},
		}, DataSource: &fields.Field{Name: "discounts", Shape: fields.ScalarList, Elem: fields.String, Response: true}},
	}
	decl := Declaration{
		TypeName: "stripe_gadget", IDPrefix: "gad_", Delete: Delete,
		Paths:   Paths{Create: "/v1/gadgets", Retrieve: "/v1/gadgets/{id}", Delete: "/v1/gadgets/{id}"},
		Answers: map[string]string{"code": "Stripe takes a fresh code.", "discounts.coupon": "Stripe takes a live coupon."},
	}
	if _, err := NewResource(decl, table); err != nil {
		t.Fatalf("resource: %v", err)
	}
	if _, err := NewDataSource(decl, table); err != nil {
		t.Fatalf("data source: %v", err)
	}
	got := answersIn(decl.Answers, fields.ForDataSource(table))
	if len(got) != 1 || got["code"] == "" {
		t.Errorf("got %v, want only `code`", got)
	}
}
