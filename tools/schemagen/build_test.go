package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

var update = flag.Bool("update", false, "rewrite the golden file")

func fixtureSpec(t *testing.T) *Spec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ParseSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func fixtureTable(t *testing.T) []fields.Field {
	t.Helper()
	table, err := Build(fixtureSpec(t), Resource{Package: "widget", CreatePath: "/v1/widgets", UpdatePath: "/v1/widgets/{id}"})
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func TestGoldenFixtureTable(t *testing.T) {
	src, err := RenderTable("widget", fixtureTable(t))
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "widget_fields.golden")
	if *update {
		if err := os.WriteFile(golden, src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(src) != string(want) {
		t.Errorf("generated table differs from %s; rerun with -update and review the diff", golden)
	}
}

func lookup(t *testing.T, table []fields.Field, path ...string) fields.Field {
	t.Helper()
	for i, name := range path {
		f, ok := fields.Find(table, name)
		if !ok {
			t.Fatalf("no field %v", path[:i+1])
		}
		if i == len(path)-1 {
			return f
		}
		table = f.Fields
	}
	return fields.Field{}
}

// Each rule of ADR D1 and D2 that the generator decides, checked on the fixture.
func TestFixtureRules(t *testing.T) {
	table := fixtureTable(t)

	for _, skipped := range []string{"expand", "object"} {
		if _, ok := fields.Find(table, skipped); ok {
			t.Errorf("%s must not be a field", skipped)
		}
	}
	shapes := map[string]fields.Shape{
		"name": fields.String, "active": fields.Bool, "count": fields.Int, "ratio": fields.Float,
		"address": fields.Object, "items": fields.ObjectList, "tags": fields.ScalarList,
		"options": fields.ObjectMap, "metadata": fields.StringMap,
		"tier_limit": fields.String, "owner": fields.String, "default_ref": fields.String,
	}
	for name, want := range shapes {
		if got := lookup(t, table, name).Shape; got != want {
			t.Errorf("%s: shape %d, want %d", name, got, want)
		}
	}

	if f := lookup(t, table, "kind"); !f.CreateOnly() || len(f.Enum) != 2 {
		t.Errorf("kind is create-only with an enum: %+v", f)
	}
	if f := lookup(t, table, "address", "zip"); !f.CreateOnly() || f.Update {
		t.Errorf("a nested field missing from the update schema is create-only: %+v", f)
	}
	if f := lookup(t, table, "address", "line1"); !f.Required || !f.Update {
		t.Errorf("address.line1: %+v", f)
	}
	if f := lookup(t, table, "name"); !f.Required || !f.Update || f.ServerDefaulted || f.Description != "The display name." {
		t.Errorf("name: %+v", f)
	}
	if f := lookup(t, table, "default_ref"); f.Create || !f.Update || !f.Response {
		t.Errorf("default_ref is update-only: %+v", f)
	}
	if f := lookup(t, table, "created"); f.Create || f.Update || !f.Response {
		t.Errorf("created is response-only: %+v", f)
	}
	for _, name := range []string{"active", "metadata"} {
		if f := lookup(t, table, name); !f.ServerDefaulted {
			t.Errorf("%s: an optional request field the response always carries is server-defaulted", name)
		}
	}
	for _, name := range []string{"count", "ratio", "kind", "name"} {
		if f := lookup(t, table, name); f.ServerDefaulted {
			t.Errorf("%s: not server-defaulted (nullable or required in the request)", name)
		}
	}
	if f := lookup(t, table, "tags"); !f.ServerDefaulted || f.Elem != fields.String {
		t.Errorf("tags: %+v", f)
	}
	if f := lookup(t, table, "tier_limit"); f.Update || !f.Create || len(f.Enum) != 0 {
		t.Errorf("a field that takes a word or a number is a plain string: %+v", f)
	}
	if f := lookup(t, table, "owner"); f.Create || !f.Response {
		t.Errorf("an expandable reference is the ID string: %+v", f)
	}
	if f := lookup(t, table, "options", "amount"); f.Shape != fields.Int {
		t.Errorf("options.amount: %+v", f)
	}
}

func TestReadOnlyFieldsAreResponseOnly(t *testing.T) {
	spec := fixtureSpec(t)
	table, err := Build(spec, Resource{Package: "widget", CreatePath: "/v1/widgets", UpdatePath: "/v1/widgets/{id}", ReadOnly: []string{"kind", "address.zip"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range [][]string{{"kind"}, {"address", "zip"}} {
		if f := lookup(t, table, path...); f.Writable() || !f.Response {
			t.Errorf("%v must be response-only: %+v", path, f)
		}
	}
	if _, err := Build(spec, Resource{Package: "widget", CreatePath: "/v1/widgets", ReadOnly: []string{"nothing"}}); err == nil {
		t.Error("a read-only entry that names no field must fail")
	}
}

func TestRequireMakesACreateFieldRequired(t *testing.T) {
	spec := fixtureSpec(t)
	base := Resource{Package: "widget", CreatePath: "/v1/widgets", UpdatePath: "/v1/widgets/{id}"}
	table, err := Build(spec, base)
	if err != nil {
		t.Fatal(err)
	}
	if lookup(t, table, "address", "zip").Required {
		t.Fatal("the fixture's address.zip must be optional for this test")
	}
	base.Require = []string{"address.zip"}
	if table, err = Build(spec, base); err != nil {
		t.Fatal(err)
	}
	if !lookup(t, table, "address", "zip").Required {
		t.Error("address.zip must be required")
	}
	base.Require = []string{"nothing"}
	if _, err := Build(spec, base); err == nil {
		t.Error("a require entry that names no field must fail")
	}
}

func TestBuildErrors(t *testing.T) {
	spec := fixtureSpec(t)
	if _, err := Build(spec, Resource{Package: "x", CreatePath: "/v1/nope"}); err == nil {
		t.Error("an unknown path must fail")
	}
	if _, err := Build(spec, Resource{Package: "x", CreatePath: "/v1/widgets", UpdatePath: "/v1/nope/{id}"}); err == nil {
		t.Error("an unknown update path must fail")
	}
	bad := *spec
	bad.Components.Schemas = map[string]*Schema{"widget": {Type: "object", Properties: map[string]*Schema{"x": {Type: "weird"}}}}
	if _, err := Build(&bad, Resource{Package: "x", CreatePath: "/v1/widgets"}); err == nil {
		t.Error("an unsupported schema type must fail loudly, not be skipped")
	}
	empty := *spec
	empty.Components.Schemas = map[string]*Schema{"widget": {Type: "object", Properties: map[string]*Schema{"x": {Type: "object"}}}}
	if _, err := Build(&empty, Resource{Package: "x", CreatePath: "/v1/widgets"}); err != nil {
		t.Errorf("a response object with no keys is skipped: %v", err)
	}
}

const readOnlySpec = `{
  "openapi": "3.0.0",
  "info": {"version": "v"},
  "paths": {
    "/v1/gauge": {"get": {"operationId": "GetGauge", "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/gauge"}}}}}}},
    "/v1/gauges": {"post": {
      "operationId": "PostGauges",
      "requestBody": {"content": {"application/x-www-form-urlencoded": {"schema": {"type": "object", "properties": {
        "limit": {"anyOf": [{"type": "string", "enum": ["inf"]}, {"type": "integer"}]},
        "owner": {"type": "string"}
      }}}}},
      "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/gauge"}}}}}
    }}
  },
  "components": {"schemas": {"gauge": {"type": "object", "properties": {
    "by_currency": {"type": "object", "additionalProperties": {"type": "integer"}},
    "limit": {"type": "integer", "nullable": true},
    "marker": {"type": "object"},
    "owner": {"type": "object", "properties": {"id": {"type": "string"}, "name": {"type": "string"}}},
    "sources": {"type": "array", "items": {"anyOf": [{"type": "object", "properties": {"a": {"type": "string"}}}, {"type": "object", "properties": {"b": {"type": "string"}}}]}}
  }}}}
}`

func TestReadPathIntMapUnionAndOmit(t *testing.T) {
	spec, err := ParseSpec([]byte(readOnlySpec))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(spec, Resource{Package: "gauge", ReadPath: "/v1/gauge"}); err == nil {
		t.Fatal("an array of alternative objects is not modelled and must fail unless omitted")
	}
	table, err := Build(spec, Resource{Package: "gauge", ReadPath: "/v1/gauge", Omit: []string{"sources"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := lookup(t, table, "by_currency").Shape; got != fields.IntMap {
		t.Errorf("a map of integers is an IntMap, got %d", got)
	}
	if _, ok := fields.Find(table, "sources"); ok {
		t.Error("an omitted field must not be in the table")
	}
	if _, ok := fields.Find(table, "marker"); ok {
		t.Error("a response object with no keys must not be in the table")
	}
	if f := lookup(t, table, "limit"); f.Create || f.Shape != fields.Int {
		t.Errorf("a read-only table has response fields only: %+v", f)
	}

	if _, err := Build(spec, Resource{Package: "gauge", ReadPath: "/v1/gauge", Omit: []string{"sources", "nothing"}}); err == nil {
		t.Error("an omit entry that names no field must fail")
	}

	// An integer in the response and `inf` or an integer in the request is a string.
	table, err = Build(spec, Resource{Package: "gauge", CreatePath: "/v1/gauges", Omit: []string{"sources"}})
	if err != nil {
		t.Fatal(err)
	}
	if f := lookup(t, table, "limit"); f.Shape != fields.String || !f.Create || !f.Response {
		t.Errorf("limit: %+v", f)
	}
	// A reference sent as an ID and returned as the whole object is a string.
	if f := lookup(t, table, "owner"); f.Shape != fields.String || !f.Create || !f.Response || len(f.Fields) != 0 {
		t.Errorf("owner: %+v", f)
	}
}

const v2Spec = `{"paths":{
 "/v2/core/things":{"post":{"operationId":"PostThings","requestBody":{"content":{"application/json":{"schema":{
   "type":"object","required":["name","kind"],"properties":{
    "name":{"type":"string"},"kind":{"type":"string","enum":["a","b"]},"note":{"type":"string"},
    "include":{"type":"array","items":{"type":"string"}},
    "target":{"type":"object","required":["url"],"properties":{"url":{"type":"string"},"secret":{"type":"string"}}}}}}}},
   "responses":{"200":{"content":{"application/json":{"schema":{"type":"object",
   "required":["id","name","kind","note","created"],"properties":{
    "id":{"type":"string"},"object":{"type":"string"},"name":{"type":"string"},"kind":{"type":"string"},"note":{"type":"string"},
    "created":{"type":"string","format":"date-time"},
    "target":{"type":"object","properties":{"url":{"type":"string"},"secret":{"type":"string"}}}}}}}}}}},
 "/v2/core/things/{id}":{"post":{"operationId":"PostThingsId","requestBody":{"content":{"application/json":{"schema":{
   "type":"object","properties":{"name":{"type":"string"},"note":{"type":"string"},"include":{"type":"array","items":{"type":"string"}}}}}}}}},
 "/v2/core/forms":{"post":{"operationId":"PostForms","requestBody":{"content":{"application/x-www-form-urlencoded":{"schema":{"type":"object","properties":{"a":{"type":"string"}}}}}},
   "responses":{"200":{"content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}}}}},
 "/v1/jsonly":{"post":{"operationId":"PostJsonly","requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"a":{"type":"string"}}}}}},
   "responses":{"200":{"content":{"application/json":{"schema":{"type":"object","properties":{"id":{"type":"string"}}}}}}}}}
}}`

func TestV2TableFromJSONBodies(t *testing.T) {
	spec, err := ParseSpec([]byte(v2Spec))
	if err != nil {
		t.Fatal(err)
	}
	table, err := Build(spec, Resource{
		Package: "thing", CreatePath: "/v2/core/things", UpdatePath: "/v2/core/things/{id}",
		Omit: []string{"include", "target.secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"include", "object"} {
		if _, ok := fields.Find(table, absent); ok {
			t.Errorf("%s must not be a field", absent)
		}
	}
	if _, ok := fields.Find(lookup(t, table, "target").Fields, "secret"); ok {
		t.Error("an omitted nested field must not be in the table")
	}
	if kind := lookup(t, table, "kind"); !kind.Required || !kind.CreateOnly() || !kind.Response {
		t.Errorf("kind: %+v", kind)
	}
	// No /v2 schema says nullable, so an optional field the response lists as required is
	// server-defaulted, which keeps it Optional and Computed.
	if note := lookup(t, table, "note"); !note.ServerDefaulted || !note.Update {
		t.Errorf("note: %+v", note)
	}
	if created := lookup(t, table, "created"); created.Shape != fields.String || created.Writable() {
		t.Errorf("created: %+v", created)
	}
	if url := lookup(t, table, "target", "url"); !url.Required || url.Update {
		t.Errorf("target.url: %+v", url)
	}
}

func TestV2AndV1MustKeepTheirBodyEncodings(t *testing.T) {
	spec, err := ParseSpec([]byte(v2Spec))
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range []Resource{
		{Package: "forms", CreatePath: "/v2/core/forms"},
		{Package: "jsonly", CreatePath: "/v1/jsonly"},
	} {
		if _, err := Build(spec, res); err == nil {
			t.Errorf("%s: a body in the wrong encoding for its namespace must fail", res.Package)
		}
	}
}

const listSpec = `{"paths":{
 "/v1/orders":{"post":{"operationId":"PostOrders","requestBody":{"content":{"application/x-www-form-urlencoded":{"schema":{
   "type":"object","properties":{
    "items":{"type":"array","items":{"type":"object","properties":{"price":{"type":"string"},"quantity":{"type":"integer"}}}},
    "tax_rates":{"anyOf":[{"type":"array","items":{"type":"string"}},{"type":"string","enum":[""]}]},
    "discounts":{"type":"array","items":{"type":"object","properties":{"coupon":{"type":"string"}}}},
    "anchor":{"type":"integer"}}}}}},
   "responses":{"200":{"content":{"application/json":{"schema":{"type":"object","required":["id"],"properties":{
    "id":{"type":"string"},
    "items":{"type":"object","properties":{"object":{"type":"string","enum":["list"]},"data":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"price":{"type":"string"},"quantity":{"type":"integer"}}}}}},
    "tax_rates":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"percentage":{"type":"number"}}}},
    "discounts":{"type":"array","items":{"type":"string"}},
    "anchor":{"type":"integer"}}}}}}}}},
 "/v1/orders/{id}":{"post":{"operationId":"PostOrdersId","requestBody":{"content":{"application/x-www-form-urlencoded":{"schema":{
   "type":"object","properties":{"anchor":{"type":"object","properties":{"type":{"type":"string"}}}}}}}}}}
}}`

func TestListShapes(t *testing.T) {
	spec, err := ParseSpec([]byte(listSpec))
	if err != nil {
		t.Fatal(err)
	}
	res := Resource{Package: "order", CreatePath: "/v1/orders", UpdatePath: "/v1/orders/{id}", Omit: []string{"discounts"}, CreateOnly: []string{"anchor"}}
	table, err := Build(spec, res)
	if err != nil {
		t.Fatal(err)
	}
	// The envelope in the response becomes the list inside it, with its fields merged into
	// the request's.
	items := lookup(t, table, "items")
	if items.Shape != fields.ObjectList || !items.Create || !items.Response {
		t.Errorf("items: %+v", items)
	}
	if id := lookup(t, items.Fields, "id"); !id.Response || id.Create {
		t.Errorf("items.id comes from the response element: %+v", id)
	}
	if price := lookup(t, items.Fields, "price"); !price.Create || !price.Response {
		t.Errorf("items.price: %+v", price)
	}
	// IDs are sent and the objects come back: a list of strings.
	if f := lookup(t, table, "tax_rates"); f.Shape != fields.ScalarList || f.Elem != fields.String || !f.Create || !f.Response {
		t.Errorf("tax_rates: %+v", f)
	}
	// An update form of another shape makes the field create-only.
	if f := lookup(t, table, "anchor"); f.Shape != fields.Int || !f.CreateOnly() {
		t.Errorf("anchor: %+v", f)
	}

	// Objects sent and IDs returned have no one shape, so the field must be omitted.
	if _, err := Build(spec, Resource{Package: "order", CreatePath: "/v1/orders", CreateOnly: []string{"anchor"}}); err == nil {
		t.Error("a list of objects against a list of IDs must fail unless omitted")
	}
	if _, err := Build(spec, Resource{Package: "order", CreatePath: "/v1/orders", Omit: []string{"discounts"}, CreateOnly: []string{"nothing"}}); err == nil {
		t.Error("a create-only entry that names no field must fail")
	}
}

// Only a list envelope is unwrapped: an object that merely has a `data` array is a nested
// object of its own.
func TestListEnvelopeNeedsTheListObject(t *testing.T) {
	spec, err := ParseSpec([]byte(strings.Replace(listSpec, `"enum":["list"]`, `"enum":["wrapper"]`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	res := Resource{Package: "order", CreatePath: "/v1/orders", UpdatePath: "/v1/orders/{id}", Omit: []string{"discounts"}, CreateOnly: []string{"anchor"}}
	if _, err := Build(spec, res); err == nil {
		t.Error("an object with a `data` array that is not a list must not stand for the request's list of objects")
	}
}
