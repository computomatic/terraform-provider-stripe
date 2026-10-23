package engine

import (
	"context"
	"strings"
	"testing"

	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// kindLineDeclaration is a line listed by the widget's `kind`, which defaults to the widget's.
func kindLineDeclaration() Declaration {
	decl := lineDeclaration()
	decl.ListParams = []ListParam{{
		Name:        "kind",
		ParentPath:  "/v1/widgets/{widget}",
		Description: "The kind whose lines to read. Defaults to the widget's.",
		Unset:       "give the widget a kind first.",
	}}
	return decl
}

// kindStub serves a widget whose `kind` is widgetKind and its lines of kind `a` and `b`, in
// pages of one, and logs each request with its `kind` parameter.
func kindStub(widgetKind wire.Value, log *[]string) clientFunc {
	lines := []wire.Object{
		{{Name: "id", Value: wire.String("li_1")}, {Name: "kind", Value: wire.String("a")}},
		{{Name: "id", Value: wire.String("li_2")}, {Name: "kind", Value: wire.String("a")}},
		{{Name: "id", Value: wire.String("li_3")}, {Name: "kind", Value: wire.String("b")}},
	}
	return func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		kind, _ := req.Params.GetString("kind")
		*log = append(*log, req.Path+"?kind="+kind)
		if req.Path == "/v1/widgets/w_1" {
			return &stripeapi.Response{Body: wire.Object{{Name: "id", Value: wire.String("w_1")}, {Name: "kind", Value: widgetKind}}}, nil
		}
		after, _ := req.Params.GetString("starting_after")
		var page []wire.Object
		for _, l := range lines {
			if k, _ := l.GetString("kind"); k == kind {
				page = append(page, l)
			}
		}
		for i, l := range page {
			if id, _ := l.GetString("id"); id == after {
				page = page[i+1:]
				break
			}
		}
		var data wire.Array
		if len(page) > 0 {
			data = wire.Array{page[0]}
		}
		return &stripeapi.Response{Body: wire.Object{{Name: "data", Value: data}, {Name: "has_more", Value: wire.Bool(len(page) > 1)}}}, nil
	}
}

// A configured list parameter goes with every page of the list, and the parent is not read.
func TestListParamConfiguredIsSentWithEveryPage(t *testing.T) {
	var log []string
	ds := dataSource(t, kindLineDeclaration(), kindStub(wire.String("b"), &log))
	state, resp := ds.read(t, ds.config(map[string]string{"widget": "w_1", "id": "li_2", "kind": "a"}))
	noErrors(t, resp.Diagnostics)
	if str(t, state, "id") != "li_2" || str(t, state, "kind") != "a" {
		t.Errorf("state: %v", state)
	}
	if got := strings.Join(log, ";"); got != "/v1/widgets/w_1/lines?kind=a;/v1/widgets/w_1/lines?kind=a" {
		t.Errorf("requests: %s", got)
	}
}

// An unset list parameter takes the parent's value, as an ID or an expanded object, and the
// state holds it.
func TestListParamUnsetReadsTheParent(t *testing.T) {
	for name, kind := range map[string]wire.Value{
		"an ID":              wire.String("b"),
		"an expanded object": wire.Object{{Name: "id", Value: wire.String("b")}},
	} {
		var log []string
		ds := dataSource(t, kindLineDeclaration(), kindStub(kind, &log))
		state, resp := ds.read(t, ds.config(map[string]string{"widget": "w_1", "id": "li_3"}))
		noErrors(t, resp.Diagnostics)
		if str(t, state, "id") != "li_3" || str(t, state, "kind") != "b" {
			t.Errorf("%s: state: %v", name, state)
		}
		if got := strings.Join(log, ";"); got != "/v1/widgets/w_1?kind=;/v1/widgets/w_1/lines?kind=b" {
			t.Errorf("%s: requests: %s", name, got)
		}
	}
}

// A parent with no value fails the read with the declaration's guidance, and lists nothing.
func TestListParamUnsetOnTheParentFails(t *testing.T) {
	var log []string
	ds := dataSource(t, kindLineDeclaration(), kindStub(wire.Null{}, &log))
	_, resp := ds.read(t, ds.config(map[string]string{"widget": "w_1", "id": "li_3"}))
	if !resp.Diagnostics.HasError() || resp.Diagnostics.Errors()[0].Detail() != "widget w_1 has no `kind`: give the widget a kind first." {
		t.Errorf("want the unset error: %v", resp.Diagnostics)
	}
	if got := strings.Join(log, ";"); got != "/v1/widgets/w_1?kind=" {
		t.Errorf("requests: %s", got)
	}
}

// An unknown parent fails the read with Stripe's error, and lists nothing.
func TestListParamUnknownParentFails(t *testing.T) {
	var log []string
	ds := dataSource(t, kindLineDeclaration(), clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		log = append(log, req.Path)
		return nil, &stripeapi.APIError{Status: 404, Type: "invalid_request_error", Code: "resource_missing", Message: "No such widget: 'w_9'"}
	}))
	_, resp := ds.read(t, ds.config(map[string]string{"widget": "w_9", "id": "li_3"}))
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "No such widget") {
		t.Errorf("want Stripe's error: %v", resp.Diagnostics)
	}
	if got := strings.Join(log, ";"); got != "/v1/widgets/w_9" {
		t.Errorf("requests: %s", got)
	}
}

func TestListParamIsOptionalAndComputed(t *testing.T) {
	ds := dataSource(t, kindLineDeclaration(), noCalls(t))
	a, ok := ds.schema.Attributes["kind"].(dschema.StringAttribute)
	if !ok || !a.Optional || !a.Computed || a.MarkdownDescription != "The kind whose lines to read. Defaults to the widget's." {
		t.Errorf("kind: %#v", ds.schema.Attributes["kind"])
	}
}

func TestListParamDeclarationRules(t *testing.T) {
	for name, edit := range map[string]func(*Declaration){
		"not found in a list": func(d *Declaration) {
			d.FindInList, d.Paths = false, Paths{Retrieve: "/v1/widgets/{widget}/lines/{id}"}
		},
		"no such field":      func(d *Declaration) { d.ListParams[0].Name = "nothing" },
		"not a string":       func(d *Declaration) { d.ListParams[0].Name = "quantity" },
		"the id":             func(d *Declaration) { d.ListParams[0].Name = "id" },
		"no parent path":     func(d *Declaration) { d.ListParams[0].ParentPath = "" },
		"not under a parent": func(d *Declaration) { d.ListParams[0].ParentPath = "/v1/widgets" },
		"no description":     func(d *Declaration) { d.ListParams[0].Description = "" },
		"no unset error":     func(d *Declaration) { d.ListParams[0].Unset = "" },
	} {
		decl := kindLineDeclaration()
		edit(&decl)
		if _, err := NewDataSource(decl, fixtureTable()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
