package main

import (
	"strings"
	"testing"
)

const oldSchema = `{"provider_schemas":{"p":{
 "resource_schemas":{
  "stripe_a":{"block":{"attributes":{
    "id":{"type":"string","computed":true},
    "name":{"type":"string","optional":true},
    "gone":{"type":"string","optional":true},
    "count":{"type":"number","optional":true},
    "secret":{"type":"string","computed":true},
    "tags":{"type":["list","string"],"optional":true},
    "address":{"nested_type":{"nesting_mode":"single","attributes":{
       "zip":{"type":"string","optional":true},
       "line":{"type":"string","optional":true}}},"optional":true}
  }}},
  "stripe_b":{"block":{"attributes":{"id":{"type":"string","computed":true}}}}
 },
 "data_source_schemas":{"stripe_a":{"block":{"attributes":{"id":{"type":"string","optional":true,"computed":true}}}}}
}}}`

const newSchema = `{"provider_schemas":{"p":{
 "resource_schemas":{
  "stripe_a":{"block":{"attributes":{
    "id":{"type":"string","computed":true},
    "name":{"type":"string","required":true},
    "count":{"type":"string","optional":true},
    "secret":{"type":"string","computed":true,"sensitive":true},
    "tags":{"type":["list","string"],"optional":true},
    "fresh":{"type":"string","optional":true},
    "must":{"type":"string","required":true},
    "address":{"nested_type":{"nesting_mode":"single","attributes":{
       "zip":{"type":"string","computed":true},
       "extra":{"type":"bool","optional":true}}},"optional":true}
  }}},
  "stripe_c":{"block":{"attributes":{"id":{"type":"string","computed":true}}}}
 },
 "data_source_schemas":{"stripe_a":{"block":{"attributes":{"id":{"type":"string","optional":true,"computed":true}}}}}
}}}`

func TestDiff(t *testing.T) {
	old, err := ParseSchemas([]byte(oldSchema))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := ParseSchemas([]byte(newSchema))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, c := range Diff(old, updated) {
		lines = append(lines, strings.Join(strings.Fields(c.String()), " "))
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"BREAK removed resource stripe_b",
		"added resource stripe_c",
		"BREAK removed resource stripe_a.gone",
		"BREAK changed resource stripe_a.name: optional to required",
		"BREAK changed resource stripe_a.count: type \"number\" to \"string\"",
		"changed resource stripe_a.secret: sensitive false to true",
		"added resource stripe_a.fresh",
		"BREAK added resource stripe_a.must: required",
		"BREAK changed resource stripe_a.address.zip: settable to output-only",
		"BREAK removed resource stripe_a.address.line",
		"added resource stripe_a.address.extra",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "stripe_a.tags") || strings.Contains(got, "data source") {
		t.Errorf("unchanged attributes must not be listed:\n%s", got)
	}
	if !HasBreaking(Diff(old, updated)) || HasBreaking(Diff(old, old)) {
		t.Error("HasBreaking")
	}
}

func TestAdditiveChangeIsNotBreaking(t *testing.T) {
	old, _ := ParseSchemas([]byte(`{"provider_schemas":{"p":{"resource_schemas":{"r":{"block":{"attributes":{"a":{"type":"string","required":true}}}}}}}}`))
	updated, _ := ParseSchemas([]byte(`{"provider_schemas":{"p":{"resource_schemas":{"r":{"block":{"attributes":{"a":{"type":"string","optional":true},"b":{"type":"string","optional":true,"computed":true}}}}}}}}`))
	changes := Diff(old, updated)
	if len(changes) != 2 || HasBreaking(changes) {
		t.Errorf("changes: %v", changes)
	}
}

func TestParseRejectsBadJSON(t *testing.T) {
	if _, err := ParseSchemas([]byte("{")); err == nil {
		t.Error("expected an error")
	}
}
