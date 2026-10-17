package main

import (
	"flag"
	"os"
	"path/filepath"
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
}

const readOnlySpec = `{
  "openapi": "3.0.0",
  "info": {"version": "v"},
  "paths": {
    "/v1/gauge": {"get": {"operationId": "GetGauge", "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/gauge"}}}}}}},
    "/v1/gauges": {"post": {
      "operationId": "PostGauges",
      "requestBody": {"content": {"application/x-www-form-urlencoded": {"schema": {"type": "object", "properties": {
        "limit": {"anyOf": [{"type": "string", "enum": ["inf"]}, {"type": "integer"}]}
      }}}}},
      "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/gauge"}}}}}
    }}
  },
  "components": {"schemas": {"gauge": {"type": "object", "properties": {
    "by_currency": {"type": "object", "additionalProperties": {"type": "integer"}},
    "limit": {"type": "integer", "nullable": true},
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
}
