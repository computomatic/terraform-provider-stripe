package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// v2Declaration is the fixture declaration moved to the /v2 namespace, with the `include`
// value the stub ignores.
func v2Declaration() Declaration {
	decl := fixtureDeclaration()
	decl.Paths = Paths{
		Create:   "/v2/core/widgets",
		Retrieve: "/v2/core/widgets/{id}",
		Update:   "/v2/core/widgets/{id}",
		Delete:   "/v2/core/widgets/{id}",
		List:     "/v2/core/widgets",
	}
	decl.Include = []string{"url"}
	return decl
}

// A /v2 type name ends in `_v2`, which messages and docs put first (architecture ADR D7).
func TestV2NounPutsTheVersionFirst(t *testing.T) {
	for typeName, want := range map[string]string{
		"stripe_product_feature":   "product feature",
		"stripe_account_v2":        "v2 account",
		"stripe_account_person_v2": "v2 account person",
	} {
		if got := Noun(typeName); got != want {
			t.Errorf("Noun(%q) = %q, want %q", typeName, got, want)
		}
	}
}

func TestV2DeclarationRules(t *testing.T) {
	cases := map[string]func(*Declaration){
		"mixed namespaces":      func(d *Declaration) { d.Paths.List = "/v1/widgets" },
		"neither namespace":     func(d *Declaration) { d.Paths.Create = "/widgets" },
		"expand on v2":          func(d *Declaration) { d.Expand = []string{"options"} },
		"clears an unknown":     func(d *Declaration) { d.ClearsWithNull = []string{"nope"} },
		"clears a create-only":  func(d *Declaration) { d.ClearsWithNull = []string{"kind"} },
		"clears a required one": func(d *Declaration) { d.ClearsWithNull = []string{"name"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			decl := v2Declaration()
			mutate(&decl)
			if _, err := NewResource(decl, fixtureTable()); err == nil {
				t.Error("expected the declaration to be rejected")
			}
		})
	}
	v1 := fixtureDeclaration()
	v1.Include = []string{"url"}
	if _, err := NewResource(v1, fixtureTable()); err == nil {
		t.Error("include belongs to /v2 declarations")
	}
	v1 = fixtureDeclaration()
	v1.ClearsWithNull = []string{"quantity"}
	if _, err := NewResource(v1, fixtureTable()); err == nil {
		t.Error("clears-with-null belongs to /v2 declarations")
	}
	if _, err := NewResource(v2Declaration(), fixtureTable()); err != nil {
		t.Errorf("a /v2 declaration must be accepted: %v", err)
	}
}

func hasInclude(req stripeapi.Request, want string) bool {
	v, ok := req.Params.Get("include")
	if !ok {
		return false
	}
	b, err := wire.EncodeJSON(v)
	return err == nil && string(b) == want
}

// A declared include list rides on every request that returns the object, and `expand` is
// never sent.
func TestV2IncludeIsSentOnEveryRequestThatReturnsTheObject(t *testing.T) {
	stub := newStubV2()
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	state, _ := r.create(t, createPlan(r))
	r.read(t, state)
	attrs := createPlan(r).Attributes()
	for k, v := range state.Attributes() {
		attrs[k] = v
	}
	attrs["name"] = types.StringValue("renamed")
	r.update(t, r.fill(attrs), state)
	r.update(t, state, state)
	ds := dataSource(t, v2Declaration(), stub)
	_, resp := ds.read(t, ds.config(map[string]string{"kind": "a"}))
	noErrors(t, resp.Diagnostics)
	noErrors(t, r.destroy(t, state))

	for _, req := range stub.requests() {
		if _, sent := req.Params.Get("expand"); sent {
			t.Errorf("%s %s sent expand", req.Method, req.Path)
		}
		switch req.Method {
		case "DELETE":
			if _, sent := req.Params.Get("include"); sent {
				t.Errorf("DELETE must not send include: %+v", req)
			}
		default:
			if !hasInclude(req, `["url"]`) {
				t.Errorf("%s %s sent no include: %+v", req.Method, req.Path, req.Params)
			}
		}
	}
}

// Create leaves out an empty collection unless Stripe requires the field, at every depth.
func TestV2CreateOmitsEmptyValues(t *testing.T) {
	stub := newStubV2()
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	itemType := r.objType.AttrTypes["items"].(basetypes.ListType).ElemType
	plan := r.fill(map[string]attr.Value{
		"name":     types.StringValue("w"),
		"events":   types.SetValueMust(types.StringType, nil),
		"tags":     types.ListValueMust(types.StringType, nil),
		"items":    types.ListValueMust(itemType, nil),
		"metadata": types.MapValueMust(types.StringType, nil),
		"options":  types.MapValueMust(r.objType.AttrTypes["options"].(basetypes.MapType).ElemType, nil),
	})
	r.create(t, plan)
	if got := wireOf(t, stub.requests()[0].Params); got != `{"name":"w","include":["url"]}` {
		t.Errorf("create body: %s", got)
	}
}

func TestV2CreateKeepsAnEmptyRequiredCollection(t *testing.T) {
	table := fixtureTable()
	for i := range table {
		if table[i].Name == "events" {
			table[i].Required = true
		}
	}
	stub := newStubV2()
	r := configured(t, v2Declaration(), table, stub)
	r.create(t, r.fill(map[string]attr.Value{
		"name":   types.StringValue("w"),
		"events": types.SetValueMust(types.StringType, nil),
	}))
	if got := wireOf(t, stub.requests()[0].Params); got != `{"name":"w","events":[],"include":["url"]}` {
		t.Errorf("create body: %s", got)
	}
}

// Update sends a changed list or object whole, and metadata as a key diff with null for a
// removed key.
func TestV2UpdateSendsWholeValuesAndMetadataNulls(t *testing.T) {
	stub := newStubV2()
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	state, _ := r.create(t, createPlan(r))
	itemType := r.objType.AttrTypes["items"].(basetypes.ListType).ElemType
	attrs := state.Attributes()
	attrs["events"] = types.SetValueMust(types.StringType, strs("a"))
	attrs["items"] = types.ListValueMust(itemType, []attr.Value{
		object(itemType, map[string]attr.Value{"sku": types.StringValue("s1"), "qty": types.Int64Value(2)}),
	})
	attrs["metadata"] = stringMapValue(map[string]string{"new": "v"})
	stub.reset()
	r.update(t, types.ObjectValueMust(r.objType.AttrTypes, attrs), state)

	got := wireOf(t, stub.requests()[0].Params)
	want := `{"items":[{"sku":"s1","qty":2}],"events":["a"],"metadata":{"a":null,"new":"v"},"include":["url"]}`
	if got != want {
		t.Errorf("update body:\n got %s\nwant %s", got, want)
	}
}

func updateResponse(t *testing.T, r *Resource, plan, state basetypes.ObjectValue) resource.UpdateResponse {
	t.Helper()
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)},
		State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
	}, &resp)
	return resp
}

// Removing a scalar from the configuration fails on /v2 unless the declaration says Stripe
// unsets it with null; the failure happens before any request.
func TestV2RefusesToClearAScalar(t *testing.T) {
	stub := newStubV2()
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	plan := createPlan(r)
	attrs := plan.Attributes()
	attrs["quantity"] = types.Int64Value(5)
	state, _ := r.create(t, types.ObjectValueMust(r.objType.AttrTypes, attrs))

	cleared := state.Attributes()
	cleared["quantity"] = types.Int64Null()
	stub.reset()
	resp := updateResponse(t, r, types.ObjectValueMust(r.objType.AttrTypes, cleared), state)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "clearing `quantity` is not supported") {
		t.Fatalf("expected a refusal, got %v", resp.Diagnostics)
	}
	if len(stub.requests()) != 0 {
		t.Errorf("a refused update must not call Stripe: %+v", stub.requests())
	}

	decl := v2Declaration()
	decl.ClearsWithNull = []string{"quantity"}
	allowed := configured(t, decl, fixtureTable(), stub)
	state2, _ := allowed.create(t, types.ObjectValueMust(allowed.objType.AttrTypes, attrs))
	cleared = state2.Attributes()
	cleared["quantity"] = types.Int64Null()
	stub.reset()
	allowed.update(t, types.ObjectValueMust(allowed.objType.AttrTypes, cleared), state2)
	if got := wireOf(t, stub.requests()[0].Params); got != `{"quantity":null,"include":["url"]}` {
		t.Errorf("update body: %s", got)
	}
}

// A /v1 declaration still unsets a removed scalar with null, which the form codec renders
// as an empty value.
func TestV1StillClearsAScalarWithNull(t *testing.T) {
	stub := newStub()
	r := fixtureResource(t, stub)
	attrs := createPlan(r).Attributes()
	attrs["quantity"] = types.Int64Value(5)
	state, _ := r.create(t, types.ObjectValueMust(r.objType.AttrTypes, attrs))
	cleared := state.Attributes()
	cleared["quantity"] = types.Int64Null()
	stub.reset()
	r.update(t, types.ObjectValueMust(r.objType.AttrTypes, cleared), state)
	if got := wireOf(t, stub.requests()[0].Params); got != `{"quantity":null}` {
		t.Errorf("update body: %s", got)
	}
}

// A /v2 lookup asks for 20, and a non-null next_page_url is what makes a single match
// ambiguous, because /v2 has no has_more.
func TestV2LookupUsesTheNextPageURL(t *testing.T) {
	stub := newStubV2()
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	r.create(t, createPlan(r))
	ds := dataSource(t, v2Declaration(), stub)

	stub.reset()
	got, resp := ds.read(t, ds.config(map[string]string{"kind": "a"}))
	noErrors(t, resp.Diagnostics)
	if str(t, got, "id") != "wid_1" {
		t.Errorf("by key: %v", got)
	}
	limit, _ := stub.requests()[0].Params.Get("limit")
	if limit != wire.Int(20) {
		t.Errorf("limit = %v, want 20", limit)
	}

	stub.morePages = true
	_, resp = ds.read(t, ds.config(map[string]string{"kind": "a"}))
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "matches more than 1 objects") {
		t.Errorf("a next page means more than one match: %v", resp.Diagnostics)
	}
}

// A retrieve that answers 404 not_found removes the resource, and a second delete is done.
func TestV2NotFoundIsGone(t *testing.T) {
	stub := newStubV2()
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	state, _ := r.create(t, createPlan(r))
	stub.mu.Lock()
	delete(stub.objects, "wid_1")
	stub.mu.Unlock()
	if _, found := r.read(t, state); found {
		t.Error("a deleted object must leave the state")
	}
	noErrors(t, r.destroy(t, state))
}

func emptyCollectionsPlan(r *Resource) basetypes.ObjectValue {
	return r.fill(map[string]attr.Value{
		"name":     types.StringValue("w"),
		"metadata": types.MapValueMust(types.StringType, nil),
		"tags":     types.ListValueMust(types.StringType, nil),
		"events":   types.SetValueMust(types.StringType, nil),
	})
}

func isEmpty(v attr.Value) bool { return emptyCollection(v) }

// Stripe may omit an empty collection or return null for it. The planned empty value is
// what the user configured, so create, read and update keep it and a plan stays empty.
func TestV2ConfiguredEmptyCollectionsSurviveAnOmittingResponse(t *testing.T) {
	stub := newStubV2()
	stub.omitEmpty = true
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	plan := emptyCollectionsPlan(r)

	state, _ := r.create(t, plan)
	for _, name := range []string{"metadata", "tags", "events"} {
		if !isEmpty(state.Attributes()[name]) {
			t.Errorf("create: %s = %v, want the planned empty collection", name, state.Attributes()[name])
		}
	}
	if body, _ := stub.objects["wid_1"].Get("metadata"); body != nil {
		t.Fatalf("the stub must omit metadata to test this: %v", body)
	}
	read, _ := r.read(t, state)
	if !read.Equal(state) {
		t.Errorf("a read must leave the state as it is:\n%v\n%v", read, state)
	}

	// The plan does not change, so an update sends nothing and keeps the empty values.
	stub.reset()
	next := r.update(t, state, state)
	for _, req := range stub.requests() {
		if req.Method == "POST" {
			t.Errorf("an unchanged plan must not POST: %+v", req)
		}
	}
	if !next.Equal(state) {
		t.Errorf("update changed the state:\n%v\n%v", next, state)
	}
}

// Changing between no value and an empty collection sends nothing on /v2, in either
// direction, and the state follows the plan.
func TestV2UpdateBetweenNoValueAndEmptyCollectionSendsNothing(t *testing.T) {
	stub := newStubV2()
	stub.omitEmpty = true
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	unset := r.fill(map[string]attr.Value{"name": types.StringValue("w")})
	state, _ := r.create(t, unset)
	if !state.Attributes()["tags"].IsNull() {
		t.Fatalf("tags: %v", state.Attributes()["tags"])
	}

	stub.reset()
	toEmpty := r.update(t, emptyCollectionsPlan(r), state)
	for _, name := range []string{"metadata", "tags", "events"} {
		if !isEmpty(toEmpty.Attributes()[name]) {
			t.Errorf("to empty: %s = %v", name, toEmpty.Attributes()[name])
		}
	}
	back := r.update(t, r.fill(map[string]attr.Value{"name": types.StringValue("w")}), toEmpty)
	for _, name := range []string{"metadata", "tags", "events"} {
		if !back.Attributes()[name].IsNull() {
			t.Errorf("back to unset: %s = %v", name, back.Attributes()[name])
		}
	}
	for _, req := range stub.requests() {
		if req.Method == "POST" {
			t.Errorf("neither change may POST: %+v", req)
		}
	}
}

// A response that returns the member as null is the same as one that omits it.
func TestV2NullResponseKeepsTheEmptyCollection(t *testing.T) {
	stub := newStubV2()
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	state, _ := r.create(t, emptyCollectionsPlan(r))
	stub.mu.Lock()
	obj := stub.objects["wid_1"]
	for _, name := range []string{"metadata", "tags", "events"} {
		obj = put(obj, name, wire.Null{})
	}
	stub.objects["wid_1"] = obj
	stub.mu.Unlock()
	read, _ := r.read(t, state)
	if !read.Equal(state) {
		t.Errorf("a null member must read as the configured empty collection:\n%v\n%v", read, state)
	}
}

// A data source has no configuration to honour, so an omitted collection is null there.
func TestV2DataSourceReadsAnOmittedCollectionAsNull(t *testing.T) {
	stub := newStubV2()
	stub.omitEmpty = true
	r := configured(t, v2Declaration(), fixtureTable(), stub)
	r.create(t, emptyCollectionsPlan(r))
	ds := dataSource(t, v2Declaration(), stub)
	got, resp := ds.read(t, ds.config(map[string]string{"id": "wid_1"}))
	noErrors(t, resp.Diagnostics)
	if !got.Attributes()["metadata"].IsNull() || !got.Attributes()["tags"].IsNull() {
		t.Errorf("data source: %v", got)
	}
}
