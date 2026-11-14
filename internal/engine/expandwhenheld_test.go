package engine

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// heldDeclaration is a widget whose `items` Stripe returns only expanded, and expands only on a
// write that sends them or a retrieve of a widget that holds them, as a payment intent's line
// items.
func heldDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.ExpandWhenHeld = []string{"items"}
	return decl
}

// heldTable is the fixture's, with `items.qty` filled in by Stripe, as a line item's optional
// members are, so that the plan of an update leaves it unknown.
func heldTable() []fields.Field {
	table := fixtureTable()
	for i, f := range table {
		if f.Name == "items" {
			table[i].Fields = slices.Clone(f.Fields)
			table[i].Fields[1].ServerDefaulted = true
		}
	}
	return table
}

func expandsItems(req stripeapi.Request) bool {
	v, _ := req.Params.Get("expand")
	paths, _ := v.(wire.Array)
	return slices.Contains(paths, wire.Value(wire.String("items")))
}

// A create or an update asks for the field when it sends it and a retrieve when the state holds
// it; an update that sends none keeps the state's value, though the plan leaves a member unknown,
// and a retrieve of a state that holds none does not ask.
func TestExpandWhenHeldIsAskedForWhereSentOrHeld(t *testing.T) {
	stub := newStub()
	client := clientFunc(func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		resp, err := stub.Do(ctx, req)
		if err != nil || expandsItems(req) {
			return resp, err
		}
		body, _ := resp.Body.(wire.Object)
		return &stripeapi.Response{Status: resp.Status, Body: drop(body, "items")}, nil
	})
	r := configured(t, heldDeclaration(), heldTable(), client)
	elemType := r.objType.AttrTypes["items"].(types.ListType).ElemType
	items := func(qty attr.Value, skus ...string) attr.Value {
		elems := make([]attr.Value, len(skus))
		for i, sku := range skus {
			elems[i] = object(elemType, map[string]attr.Value{"sku": types.StringValue(sku), "qty": qty})
		}
		return types.ListValueMust(elemType, elems)
	}
	skus := func(obj basetypes.ObjectValue) []string {
		var out []string
		l, _ := obj.Attributes()["items"].(basetypes.ListValue)
		for _, e := range l.Elements() {
			out = append(out, e.(basetypes.ObjectValue).Attributes()["sku"].(basetypes.StringValue).ValueString())
		}
		return out
	}
	expanded := func(want ...bool) {
		t.Helper()
		reqs := stub.requests()
		if len(reqs) != len(want) {
			t.Fatalf("requests: %+v, want %d", reqs, len(want))
		}
		for i, req := range reqs {
			if expandsItems(req) != want[i] {
				t.Errorf("%s %s expanded items: %t, want %t", req.Method, req.Path, !want[i], want[i])
			}
		}
		stub.reset()
	}

	state, _ := r.create(t, r.fill(with(createPlan(r), map[string]attr.Value{"items": items(types.Int64Null(), "a")})))
	// The create sends the items; its follow-up update sends none.
	expanded(true, false)
	if got := skus(state); !slices.Equal(got, []string{"a"}) {
		t.Errorf("items after create: %v", got)
	}

	state, _ = r.read(t, state)
	expanded(true)
	if got := skus(state); !slices.Equal(got, []string{"a"}) {
		t.Errorf("items after read: %v", got)
	}

	// The configuration leaves `qty` unset, which the framework then plans unknown.
	set := map[string]attr.Value{"name": types.StringValue("renamed"), "items": items(types.Int64Null(), "a")}
	config := r.fill(with(state, set))
	set["items"] = items(types.Int64Unknown(), "a")
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
	r.Update(context.Background(), resource.UpdateRequest{
		Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)},
		Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, r.fill(with(state, set)))},
		State:  tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
	}, &resp)
	noErrors(t, resp.Diagnostics)
	var renamed basetypes.ObjectValue
	noErrors(t, resp.State.Get(context.Background(), &renamed))
	expanded(false)
	if got := skus(renamed); !slices.Equal(got, []string{"a"}) || !renamed.Attributes()["items"].(basetypes.ListValue).Elements()[0].(basetypes.ObjectValue).Attributes()["qty"].IsNull() {
		t.Errorf("an update that sends no items must keep the state's: %v", renamed.Attributes()["items"])
	}

	changed := r.update(t, r.fill(with(renamed, map[string]attr.Value{"items": items(types.Int64Null(), "b", "c")})), renamed)
	expanded(true)
	if got := skus(changed); !slices.Equal(got, []string{"b", "c"}) {
		t.Errorf("items after the update that sends them: %v", got)
	}

	r.read(t, r.fill(with(changed, map[string]attr.Value{"items": types.ListNull(elemType)})))
	expanded(false)

	t.Run("below the top level, across a create's follow-up and its last retrieve", func(t *testing.T) {
		nestedHeldOnCreate(t)
	})
}

// nestedHeldOnCreate creates a widget whose held `details.lines` sit below the top level, with
// a member Stripe fills in that the plan leaves unknown. The create's follow-up update and its
// last retrieve (for `provider`, expanded only on a retrieve) send no lines, so they do not ask for
// them, and their `details`, laid over the create's whole, holds none: the state still holds the
// lines the create read.
func nestedHeldOnCreate(t *testing.T) {
	decl := heldDeclaration()
	decl.Lookups = nil
	decl.ExpandWhenHeld = []string{"details.lines"}
	decl.ExpandOnRetrieve = []string{"provider"}
	table := append(fixtureTable(), fields.Field{Name: "details", Shape: fields.Object, Create: true, Update: true, Response: true, ServerDefaulted: true, Fields: []fields.Field{
		{Name: "lines", Shape: fields.ObjectList, Create: true, Update: true, Response: true, ServerDefaulted: true, Fields: []fields.Field{
			{Name: "sku", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
			{Name: "ref", Shape: fields.String, Response: true},
		}},
		{Name: "note", Shape: fields.String, Create: true, Update: true, Response: true},
	}})
	stub := newStub()
	var calls []string
	client := clientFunc(func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		resp, err := stub.Do(ctx, req)
		if err != nil {
			return resp, err
		}
		v, _ := req.Params.Get("expand")
		paths, _ := v.(wire.Array)
		asked := slices.Contains(paths, wire.Value(wire.String("details.lines")))
		calls = append(calls, req.Method+":"+strconv.FormatBool(asked))
		body, _ := resp.Body.(wire.Object)
		details, held := body.GetObject("details")
		if !held {
			return resp, nil
		}
		shown := drop(details, "lines")
		if l, has := details.Get("lines"); has && asked {
			var lines wire.Array
			for _, e := range l.(wire.Array) {
				line := e.(wire.Object)
				sku, _ := line.GetString("sku")
				lines = append(lines, append(append(wire.Object(nil), line...), wire.Field{Name: "ref", Value: wire.String("ref_" + sku)}))
			}
			shown = append(shown, wire.Field{Name: "lines", Value: lines})
		}
		return &stripeapi.Response{Status: resp.Status, Body: put(append(wire.Object(nil), body...), "details", shown)}, nil
	})
	r := configured(t, decl, table, client)
	detailsType := r.objType.AttrTypes["details"]
	linesType := detailsType.(types.ObjectType).AttrTypes["lines"].(types.ListType)
	line := object(linesType.ElemType, map[string]attr.Value{"sku": types.StringValue("a"), "ref": types.StringUnknown()})
	plan := with(createPlan(r), map[string]attr.Value{"details": object(detailsType, map[string]attr.Value{
		"lines": types.ListValueMust(linesType.ElemType, []attr.Value{line}),
		"note":  types.StringValue("n"),
	})})
	state, _ := r.create(t, r.fill(plan))
	if got := strings.Join(calls, ","); got != "POST:true,POST:false,GET:false" {
		t.Errorf("calls, each with whether it asked for the lines: %s", got)
	}
	lines, _ := state.Attributes()["details"].(basetypes.ObjectValue).Attributes()["lines"].(basetypes.ListValue)
	if len(lines.Elements()) != 1 || lines.Elements()[0].(basetypes.ObjectValue).Attributes()["ref"].(basetypes.StringValue).ValueString() != "ref_a" {
		t.Errorf("the state must hold the lines the create read: %v", state.Attributes()["details"])
	}
}

// The data source cannot know whether the object holds the field before it reads, so it neither
// has the attribute nor asks for it.
func TestExpandWhenHeldIsLeftOutOfTheDataSource(t *testing.T) {
	stub := newStub()
	r := configured(t, heldDeclaration(), fixtureTable(), stub)
	state, _ := r.create(t, createPlan(r))
	stub.reset()
	ds := dataSource(t, heldDeclaration(), stub)
	if _, has := ds.objType.AttrTypes["items"]; has {
		t.Error("the data source must leave the field out")
	}
	_, resp := ds.read(t, ds.config(map[string]string{"id": str(t, state, "id")}))
	noErrors(t, resp.Diagnostics)
	for _, req := range stub.requests() {
		if expandsItems(req) {
			t.Errorf("the data source must not ask for the field: %+v", req)
		}
	}
}

// An action's response, which never asks for the field, keeps the value the write before it read,
// at any depth; one that returns the field keeps its own.
func TestExpandWhenHeldKeepsTheWrittenValueAcrossActions(t *testing.T) {
	decl := fixtureDeclaration()
	decl.ExpandWhenHeld = []string{"items", "address.zip"}
	r := configured(t, decl, fixtureTable(), newStub())
	written := wire.Object{
		{Name: "items", Value: wire.Array{wire.Object{{Name: "sku", Value: wire.String("a")}}}},
		{Name: "address", Value: wire.Object{{Name: "line1", Value: wire.String("1 Main")}, {Name: "zip", Value: wire.String("10001")}}},
	}
	acted := wire.Object{{Name: "address", Value: wire.Object{{Name: "line1", Value: wire.String("2 Main")}}}, {Name: "name", Value: wire.String("n")}}
	if got := wireOf(t, r.m.keepExpandedWhenHeld(written, acted)); got != `{"address":{"line1":"2 Main","zip":"10001"},"name":"n","items":[{"sku":"a"}]}` {
		t.Errorf("kept: %s", got)
	}
	own := wire.Object{{Name: "items", Value: wire.Array{}}}
	if got := wireOf(t, r.m.keepExpandedWhenHeld(written, own)); got != `{"items":[],"address":{"zip":"10001"}}` {
		t.Errorf("a returned field keeps its own value: %s", got)
	}
}

func TestExpandWhenHeldMustNameAResponseFieldOutsideAList(t *testing.T) {
	for name, entry := range map[string]string{
		"no field":     "nothing",
		"request only": "passcode",
		"in a list":    "items.sku",
	} {
		decl := heldDeclaration()
		decl.ExpandWhenHeld = []string{entry}
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("%s: the declaration must fail", name)
		}
	}
	decl := heldDeclaration()
	decl.Expand = []string{"items"}
	if _, err := NewResource(decl, fixtureTable()); err == nil {
		t.Error("an entry also in expand must fail")
	}
	if _, err := NewResource(heldDeclaration(), fixtureTable()); err != nil {
		t.Errorf("the control must compile: %v", err)
	}
}
