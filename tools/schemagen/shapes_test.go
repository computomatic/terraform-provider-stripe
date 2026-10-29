package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// The shapes of ADR 2026-10-29-full-attribute-coverage, D3, on the fixture
// testdata/shapes_spec.json. The engine's side of each is in internal/engine/shapes_test.go,
// and the CLIs' acceptance of a hash with no members in internal/resources/empty_hash_test.go.

// shapesSpec loads the fixture, with each pair of edits replacing text in it.
func shapesSpec(t *testing.T, edits ...string) *Spec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "shapes_spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for i := 0; i+1 < len(edits); i += 2 {
		if !strings.Contains(text, edits[i]) {
			t.Fatalf("the fixture has no %q", edits[i])
		}
		text = strings.Replace(text, edits[i], edits[i+1], 1)
	}
	spec, err := ParseSpec([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// shape is the fixture table. `clash`, whose variants give `value` two shapes, is omitted.
func shape() Resource {
	return Resource{Package: "shape", CreatePath: "/v1/shapes", UpdatePath: "/v1/shapes/{id}", Omit: []string{"clash"},
		PagedLists: map[string]string{"lines": "stripe_shape"}}
}

func buildShape(t *testing.T, spec *Spec, res Resource, g *gate) []fields.Field {
	t.Helper()
	table, err := buildGated(spec, res, g)
	if err != nil {
		t.Fatal(err)
	}
	return table
}

// field returns the field at a dotted attribute path.
func field(t *testing.T, table []fields.Field, dotted string) fields.Field {
	t.Helper()
	var f fields.Field
	level := table
	for _, name := range strings.Split(dotted, ".") {
		i := slices.IndexFunc(level, func(f fields.Field) bool { return f.AttrName() == name })
		if i < 0 {
			t.Fatalf("no field %s", dotted)
		}
		f, level = level[i], level[i].Fields
	}
	return f
}

func names(fs []fields.Field) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.AttrName())
	}
	return out
}

// A response union of objects is one object with every variant's members, merged by name; a
// member both give, such as `object`, takes both variants' values.
func TestShapeResponseUnionIsMerged(t *testing.T) {
	payment := field(t, buildShape(t, shapesSpec(t), shape(), nil), "payment")
	if payment.Shape != fields.Object || payment.Create || !payment.Response {
		t.Fatalf("payment: %+v", payment)
	}
	if got := names(payment.Fields); !slices.Equal(got, []string{"bank_name", "brand", "expires", "last4", "object"}) {
		t.Errorf("payment holds %v", got)
	}
	// The integer-or-string rule stays inside a merged union.
	if expires := field(t, payment.Fields, "expires"); expires.Shape != fields.String {
		t.Errorf("payment.expires: %+v", expires)
	}
	if object := field(t, payment.Fields, "object"); !slices.Equal(object.Enum, []string{"card", "bank_account"}) {
		t.Errorf("payment.object takes %v", object.Enum)
	}
	got := byKey(coverShape(t, shape(), nil))
	for _, k := range []string{"response/shape/payment.brand@card", "response/shape/payment.bank_name@bank_account", "response/shape/payment.last4@card", "response/shape/payment.last4@bank_account"} {
		if got[k].State != StateExposed {
			t.Errorf("%s: %s", k, got[k].State)
		}
	}
}

// Two variants that give one member different shapes fail, naming it.
func TestShapeUnionWithClashingMembersFails(t *testing.T) {
	res := shape()
	res.Omit = nil
	_, err := Build(shapesSpec(t), res)
	if err == nil || !strings.Contains(err.Error(), "clash.value: the variants of a union give it different shapes") {
		t.Errorf("err = %v", err)
	}
}

// A request union of two hashes merges as a response union does, and requires a member only
// where every variant does.
func TestShapeRequestUnionOfTwoHashesIsMerged(t *testing.T) {
	method := field(t, buildShape(t, shapesSpec(t), shape(), nil), "method")
	if method.Shape != fields.Object || !method.Create || method.Response {
		t.Fatalf("method: %+v", method)
	}
	if got := names(method.Fields); !slices.Equal(got, []string{"bank", "card", "type"}) {
		t.Errorf("method holds %v", got)
	}
	if typ := field(t, method.Fields, "type"); !typ.Required || !slices.Equal(typ.Enum, []string{"card", "bank"}) {
		t.Errorf("method.type: %+v", typ)
	}
	if card := field(t, method.Fields, "card"); card.Required {
		t.Errorf("one variant's member is not required: %+v", card)
	}
}

// The walk stops at a schema met again on its own path: the repeat is held as its ID, or
// left out where it has none.
func TestShapeRecursionIsCutAtTheRepeat(t *testing.T) {
	table := buildShape(t, shapesSpec(t), shape(), nil)
	if parent := field(t, table, "parent"); parent.Shape != fields.String || !parent.Response || len(parent.Fields) != 0 {
		t.Errorf("parent is the repeat's ID: %+v", parent)
	}
	if loop := field(t, table, "loop"); !slices.Equal(names(loop.Fields), []string{"name"}) {
		t.Errorf("a repeat with no ID is left out: loop holds %v", names(loop.Fields))
	}
	got := byKey(coverShape(t, shape(), nil))
	if r := got["response/shape/parent"]; r.State != StateExposed || r.Rule != "" {
		t.Errorf("parent: %+v", r)
	}
	if r := got["response/shape/loop.loop"]; r.State != StateExcluded || r.Rule != RuleRecursion {
		t.Errorf("loop.loop: %+v", r)
	}
}

// An embedded copy of another object is read in full; the report holds it as one leaf.
func TestShapeEmbeddedCopyIsReadInFull(t *testing.T) {
	charge := field(t, buildShape(t, shapesSpec(t), shape(), nil), "last_error.charge")
	if got := names(charge.Fields); !slices.Equal(got, []string{"amount", "id", "outcome"}) || len(field(t, charge.Fields, "outcome").Fields) != 1 {
		t.Errorf("last_error.charge holds %v", got)
	}
	got := byKey(coverShape(t, shape(), nil))
	if r := got["response/shape/last_error.charge"]; r.State != StateExposed || r.Shape != "embedded" || r.Target != "charge" {
		t.Errorf("last_error.charge: %+v", r)
	}
}

// A request hash with no members is a nested attribute with none.
func TestShapeRequestHashWithNoMembers(t *testing.T) {
	options := field(t, buildShape(t, shapesSpec(t), shape(), nil), "options")
	affirm := field(t, options.Fields, "affirm")
	if affirm.Shape != fields.Object || !affirm.Create || affirm.Response || len(affirm.Fields) != 0 {
		t.Errorf("options.affirm: %+v", affirm)
	}
	got := byKey(coverShape(t, shape(), nil))
	if r := got["request/PostShapes/options.affirm"]; r.State != StateExposed || r.Shape != "empty_hash" {
		t.Errorf("options.affirm: %+v", r)
	}
	// One the response returns with members is an object of the response's members; the
	// request still sends it as `{}`.
	if zip := field(t, options.Fields, "zip"); !zip.Create || !zip.Response || !slices.Equal(names(zip.Fields), []string{"status"}) {
		t.Errorf("options.zip: %+v", zip)
	}
	if r := got["request/PostShapes/options.zip"]; r.State != StateExposed {
		t.Errorf("options.zip: %+v", r)
	}
	// A response hash with no keys that the request writes is the same attribute, not a type
	// marker that rule X2 leaves out.
	if r := got["response/shape/options.klarna"]; r.State != StateExposed || r.Rule != "" {
		t.Errorf("response options.klarna: %+v", r)
	}
}

// A field the response gives another shape takes the request's in the resource, which keeps
// the configured value, and the response's in the data source.
func TestShapeSameNameDifferentShapeSplits(t *testing.T) {
	a := field(t, buildShape(t, shapesSpec(t), shape(), nil), "caps.a")
	if a.Shape != fields.Object || !a.Create || !a.Update || a.Response || a.DataSource == nil {
		t.Fatalf("caps.a: %+v", a)
	}
	if ds := a.DataSource; ds.Shape != fields.String || !ds.Response || ds.Create || !slices.Equal(ds.Enum, []string{"active", "inactive"}) {
		t.Errorf("caps.a in the data source: %+v", ds)
	}
	got := byKey(coverShape(t, shape(), nil))
	for _, k := range []string{"request/PostShapes/caps.a.requested", "request/PostShapesId/caps.a.requested", "response/shape/caps.a"} {
		if got[k].State != StateExposed {
			t.Errorf("%s: %s", k, got[k].State)
		}
	}
}

// The hash form of a token-or-hash parameter is `<name>_data`, built only where the gate
// verifies a leaf of it and holding only verified members; the token form stays as it is.
func TestShapeTokenOrHashHasAHashForm(t *testing.T) {
	spec := shapesSpec(t)
	if _, ok := index(buildShape(t, spec, shape(), nil)).request["source_data"]; ok {
		t.Error("source_data without a verified leaf")
	}
	tc := coverShape(t, shape(), map[string]string{"request/PostShapes/source.number@object": ClassVerified})
	data := field(t, tc.table, "source_data")
	if data.Name != "source" || data.Shape != fields.Object || !data.Create || data.Update || data.Response || data.Required {
		t.Errorf("source_data: %+v", data)
	}
	if got := names(data.Fields); !slices.Equal(got, []string{"number"}) {
		t.Errorf("source_data holds %v, want only the verified member", got)
	}
	if token := field(t, tc.table, "source"); token.Shape != fields.String || !token.Create {
		t.Errorf("source: %+v", token)
	}
	got := byKey(tc)
	for k, want := range map[string]State{
		"request/PostShapes/source@string":        StateExposed,
		"request/PostShapes/source.number@object": StateExposed,
		"request/PostShapes/source.cvc@object":    StateAwaiting,
	} {
		if got[k].State != want {
			t.Errorf("%s: %s, want %s", k, got[k].State, want)
		}
	}
}

// The generator fails when `<name>_data` is already a field at that level.
func TestShapeHashFormRefusesACollision(t *testing.T) {
	spec := shapesSpec(t, `"name": {"type": "string"},
            "tax_id"`, `"name": {"type": "string"},
            "source_data": {"type": "string"},
            "tax_id"`)
	_, err := buildGated(spec, shape(), &gate{create: []string{"source_data.number"}})
	if err == nil || !strings.Contains(err.Error(), "`source_data`, the hash form of `source`, is already a field") {
		t.Errorf("err = %v", err)
	}
}

// A value Stripe reports only through `<name>_provided` is Sensitive where the gate exposes
// it; today's such arguments are Sensitive by their declarations.
func TestShapeSensitiveFromProvided(t *testing.T) {
	spec := shapesSpec(t)
	if f := field(t, buildShape(t, spec, shape(), nil), "tax_id"); f.Sensitive {
		t.Error("tax_id is marked without the gate")
	}
	tc := coverShape(t, shape(), map[string]string{"request/PostShapes/tax_id": ClassVerified})
	if f := field(t, tc.table, "tax_id"); !f.Sensitive || !f.Create || f.Response {
		t.Errorf("tax_id: %+v", f)
	}
	if f := field(t, tc.table, "name"); f.Sensitive {
		t.Error("name has no `name_provided`")
	}
}

// Every list envelope the object requires names the type that reads every element; the report
// records it, and a stale entry or an unknown type fails.
func TestShapePagedListsAreNamed(t *testing.T) {
	if r := byKey(coverShape(t, shape(), nil))["response/shape/lines"]; r.PagedBy != "stripe_shape" || r.State != StateExposed {
		t.Errorf("lines: %+v", r)
	}
	res := shape()
	res.PagedLists = nil
	if _, err := coverTable(shapesSpec(t), res, nil); err == nil || !strings.Contains(err.Error(), "name in PagedLists the type that reads every element of each list Stripe returns a page at a time: lines") {
		t.Errorf("missing: err = %v", err)
	}
	res.PagedLists = map[string]string{"lines": "stripe_shape", "loop": "stripe_shape"}
	if _, err := coverTable(shapesSpec(t), res, nil); err == nil || !strings.Contains(err.Error(), `paged-lists entry "loop" names no list the object requires`) {
		t.Errorf("stale: err = %v", err)
	}
	res.PagedLists = map[string]string{"lines": "stripe_shape_line"}
	if err := checkPagedReaders([]Resource{res}); err == nil || !strings.Contains(err.Error(), "stripe_shape_line, which is no table's type") {
		t.Errorf("unknown type: err = %v", err)
	}
}

// A leaf in a union branch is exposed only where the field at the union holds that branch.
func TestExposedHonoursTheUnionBranch(t *testing.T) {
	table := map[string]held{"flag": {shape: fields.Bool}, "note": {shape: fields.String}, "card": {shape: fields.Object, nested: true}}
	for _, tc := range []struct {
		branch, at string
		want       bool
	}{
		{"boolean", "flag", true},
		{"string", "flag", false},
		{"integer", "note", true},
		{"string", "note", true},
		{"object", "note", false},
		{"bank_account", "note", false},
		{"bank_account", "card", true},
		{"array", "card", false},
	} {
		l := Leaf{Key: Key{Branch: tc.branch}, BranchAt: tc.at, PointAt: tc.at}
		if got := holdsBranch(l, table); got != tc.want {
			t.Errorf("%s at %s: %v, want %v", tc.branch, tc.at, got, tc.want)
		}
	}
	// A string field holds the token form of a token-or-hash parameter, not its hash.
	got := byKey(coverShape(t, shape(), nil))
	if got["request/PostShapes/source@string"].State != StateExposed || got["request/PostShapes/source.cvc@object"].State != StateAwaiting {
		t.Errorf("source: %+v, %+v", got["request/PostShapes/source@string"], got["request/PostShapes/source.cvc@object"])
	}
}

// coverShape covers the fixture table, with evidence that verifies the given leaves.
func coverShape(t *testing.T, res Resource, verified map[string]string) *tableCoverage {
	t.Helper()
	spec := shapesSpec(t)
	var ev *Pinned
	if verified != nil {
		ev = pinnedIn(t, spec, res, verified)
	}
	tc, err := coverTable(spec, res, ev)
	if err != nil {
		t.Fatal(err)
	}
	return tc
}

// A verified leaf of the hash form keeps its field when the trims leave out the token: the
// table holds `<name>_data` and no `<name>`.
func TestShapeVerifiedHashFormSurvivesATrimmedToken(t *testing.T) {
	res := shape()
	res.Omit = append(res.Omit, "source")
	tc := coverShape(t, res, map[string]string{"request/PostShapes/source.number@object": ClassVerified})
	if _, ok := index(tc.table).request["source"]; ok {
		t.Error("the trimmed token is in the table")
	}
	if data := field(t, tc.table, "source_data"); !data.Create || !slices.Equal(names(data.Fields), []string{"number"}) {
		t.Errorf("source_data: %+v", data)
	}
	if r := byKey(tc)["request/PostShapes/source.number@object"]; r.State != StateExposed {
		t.Errorf("source.number@object: %s", r.State)
	}
}

// The hash form of a parameter the create request requires is marked Required too: the
// engine reads the pair as one of the two required.
func TestShapeHashFormOfARequiredParameter(t *testing.T) {
	spec := shapesSpec(t, `"properties": {
            "name": {"type": "string"},
            "tax_id"`, `"required": ["source"],
          "properties": {
            "name": {"type": "string"},
            "tax_id"`)
	table := buildShape(t, spec, shape(), &gate{create: []string{"source_data.number"}})
	if token, data := field(t, table, "source"), field(t, table, "source_data"); !token.Required || !data.Required {
		t.Errorf("source required %v, source_data required %v", token.Required, data.Required)
	}
	if data := field(t, buildShape(t, shapesSpec(t), shape(), &gate{create: []string{"source_data.number"}}), "source_data"); data.Required {
		t.Error("the hash form of an optional parameter is required")
	}
}
