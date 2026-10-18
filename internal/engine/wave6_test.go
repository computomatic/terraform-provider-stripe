package engine

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func attachDeclaration() Declaration {
	return Declaration{
		TypeName: "stripe_attached",
		IDPrefix: "wid_",
		Parent:   "owner",
		Attach:   "target",
		Paths: Paths{
			Create:   "/v1/widgets/{target}/attach",
			Retrieve: "/v1/owners/{owner}/widgets/{id}",
			Delete:   "/v1/widgets/{id}/detach",
		},
		Delete: Archive,
		Archive: &ArchiveAction{
			Field: "owner",
			Value: wire.Null{},
			Path:  "/v1/widgets/{id}/detach",
		},
	}
}

func attachTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "label", Shape: fields.String, Response: true},
	}
}

// An attachment names the existing object in the create path and the parent in the body. The
// state keeps the configured object ID although Stripe answers with the ID of a new object.
func TestAttachNamesTheObjectInThePathAndTheParentInTheBody(t *testing.T) {
	var seen []stripeapi.Request
	client := clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		seen = append(seen, req)
		return &stripeapi.Response{Status: http.StatusOK, Body: wire.Object{{Name: "id", Value: wire.String("wid_new")}, {Name: "label", Value: wire.String("x")}}}, nil
	})
	r := configured(t, attachDeclaration(), attachTable(), client)
	plan := r.fill(map[string]attr.Value{"owner": types.StringValue("own_1"), "target": types.StringValue("wid_test/1")})

	state, _ := r.create(t, plan)

	if len(seen) != 1 || seen[0].Method != "POST" || seen[0].Path != "/v1/widgets/wid_test%2F1/attach" {
		t.Fatalf("requests = %+v, want one POST to the attach path with the object ID escaped", seen)
	}
	if got := wireOf(t, seen[0].Params); got != `{"owner":"own_1"}` {
		t.Errorf("body = %s, want the parent only", got)
	}
	if got := str(t, state, "id"); got != "wid_new" {
		t.Errorf("id = %q, want the ID Stripe returned", got)
	}
	if got := str(t, state, "target"); got != "wid_test/1" {
		t.Errorf("target = %q, want the configured value, which a later plan compares", got)
	}
}

func TestAttachNeedsAParentAndAPathThatNamesTheObject(t *testing.T) {
	noParent := attachDeclaration()
	noParent.Parent = ""
	badPath := attachDeclaration()
	badPath.Paths.Create = "/v1/widgets/attach"
	for name, decl := range map[string]Declaration{"no parent": noParent, "no placeholder in the create path": badPath} {
		if _, err := NewResource(decl, attachTable()); err == nil || !strings.Contains(err.Error(), "attachment") {
			t.Errorf("%s: err = %v, want an attachment error", name, err)
		}
	}
}

// fixedTable is listTable with a price that only a create takes, as a payment link's line
// items have.
func fixedTable() []fields.Field {
	table := listTable()
	for i := range table {
		if table[i].Name != "items" {
			continue
		}
		for j := range table[i].Fields {
			if table[i].Fields[j].Name == "price" {
				table[i].Fields[j].Update = false
			}
		}
	}
	return table
}

func fixedDeclaration() Declaration {
	d := listDeclaration()
	d.MatchBy = map[string]string{"items": "price"}
	d.FixedKeys = []string{"items"}
	return d
}

func fixedRow(price string, quantity int64) map[string]attr.Value {
	return map[string]attr.Value{"id": types.StringUnknown(), "price": sv(price), "quantity": types.Int64Value(quantity), "currency": types.StringUnknown()}
}

// fixedPlan builds the object value of a resource holding the items.
func fixedPlan(r *Resource, rows []map[string]attr.Value) basetypes.ObjectValue {
	return r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, rows...)})
}

func fixedCfg(price string, quantity int64) map[string]attr.Value {
	return map[string]attr.Value{"price": sv(price), "quantity": types.Int64Value(quantity)}
}

func fixedState(id, price string, quantity int64) map[string]attr.Value {
	return map[string]attr.Value{"id": sv(id), "price": sv(price), "quantity": types.Int64Value(quantity), "currency": sv("usd")}
}

// A FixedKeys list is sent whole when one element changes, each element with its `id` and the
// fields that change in place; a reorder or no change sends nothing.
func TestFixedKeysListIsSentWholeWhenAnElementChanges(t *testing.T) {
	r := configured(t, fixedDeclaration(), fixedTable(), newStub())
	state := []map[string]attr.Value{fixedState("li_A", "pa", 1), fixedState("li_B", "pb", 1)}
	cfg := func(a, b int64) []map[string]attr.Value {
		return []map[string]attr.Value{fixedCfg("pa", a), fixedCfg("pb", b)}
	}
	plan := func(a, b int64) []map[string]attr.Value {
		return []map[string]attr.Value{fixedRow("pa", a), fixedRow("pb", b)}
	}
	for name, tc := range map[string]struct {
		plan, config []map[string]attr.Value
		want         string
	}{
		"change one quantity": {plan(1, 4), cfg(1, 4), `{"items":[{"quantity":1,"id":"li_A"},{"quantity":4,"id":"li_B"}]}`},
		"no change":           {plan(1, 1), cfg(1, 1), `{}`},
		"reorder": {
			[]map[string]attr.Value{fixedRow("pb", 1), fixedRow("pa", 1)},
			[]map[string]attr.Value{fixedCfg("pb", 1), fixedCfg("pa", 1)},
			`{}`,
		},
	} {
		params, err := r.m.encodeUpdate(fixedPlan(r, tc.plan), fixedPlan(r, state), raw(t, fixedPlan(r, tc.config)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := wireOf(t, params); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tc.want)
		}
	}
}

// What Stripe cannot change in place replaces the resource: another set of keys, or a
// create-only field of an element the configuration sets differently. A reorder, a quantity
// and a value the configuration leaves to Stripe do not.
func TestFixedKeysChangedFindsWhatNeedsReplacement(t *testing.T) {
	r := configured(t, fixedDeclaration(), fixedTable(), newStub())
	items, ok := r.m.top("items")
	if !ok {
		t.Fatal("no items node")
	}
	state := []map[string]attr.Value{fixedState("li_A", "pa", 1), fixedState("li_B", "pb", 1)}
	withCurrency := func(price, currency string) map[string]attr.Value {
		row := fixedRow(price, 1)
		row["currency"] = sv(currency)
		return row
	}
	for name, tc := range map[string]struct {
		plan []map[string]attr.Value
		want bool
	}{
		"same":                         {[]map[string]attr.Value{fixedRow("pa", 1), fixedRow("pb", 1)}, false},
		"reordered":                    {[]map[string]attr.Value{fixedRow("pb", 1), fixedRow("pa", 1)}, false},
		"another quantity":             {[]map[string]attr.Value{fixedRow("pa", 5), fixedRow("pb", 1)}, false},
		"an element added":             {[]map[string]attr.Value{fixedRow("pa", 1), fixedRow("pb", 1), fixedRow("pc", 1)}, true},
		"an element dropped":           {[]map[string]attr.Value{fixedRow("pa", 1)}, true},
		"a price swapped":              {[]map[string]attr.Value{fixedRow("pa", 1), fixedRow("pc", 1)}, true},
		"a create-only field changed":  {[]map[string]attr.Value{fixedRow("pa", 1), withCurrency("pb", "eur")}, true},
		"a create-only field the same": {[]map[string]attr.Value{fixedRow("pa", 1), withCurrency("pb", "usd")}, false},
	} {
		plan, st := fixedPlan(r, tc.plan), fixedPlan(r, state)
		got := r.m.fixedKeysChanged(items, plan.Attributes()["items"], st.Attributes()["items"], raw(t, plan))
		if got != tc.want {
			t.Errorf("%s: fixedKeysChanged = %v, want %v", name, got, tc.want)
		}
	}
}

func TestCompileValidatesFixedKeys(t *testing.T) {
	d := listDeclaration()
	d.FixedKeys = []string{"items"}
	if _, err := NewResource(d, fixedTable()); err == nil || !strings.Contains(err.Error(), "fixed_keys") {
		t.Errorf("a fixed_keys list that is not a match_by list: err = %v", err)
	}
}

// The resource manages every element of a FixedKeys list, and the check by element stands in
// for RequiresReplace on a field inside it, which would fire on a reorder.
func TestFixedKeysListManagesEveryElementAndChecksFieldsByElement(t *testing.T) {
	r := configured(t, fixedDeclaration(), fixedTable(), newStub())
	state := fixedPlan(r, []map[string]attr.Value{fixedState("li_A", "pa", 1)})
	if keep := r.m.managedKeys(state); keep["items"] != nil {
		t.Errorf("managedKeys = %+v, want no limit on a fixed list", keep)
	}
	items, _ := r.m.top("items")
	currency := kidNamed(items, "currency")
	if currency == nil || !currency.Fixed || currency.Replace || currency.ReplaceIfSet {
		t.Errorf("currency = %+v, want Fixed and neither Replace nor ReplaceIfSet", currency)
	}
}

// Through the framework: another set of prices in a FixedKeys list replaces the resource, and
// the same prices in another order do not.
func TestPlanReplacesWhenTheKeysOfAFixedListChange(t *testing.T) {
	p := newPlanner(t, fixedDeclaration(), fixedTable())
	itemType := p.attrTypes("items")
	listType := p.typ.(tftypes.Object).AttributeTypes["items"]
	item := func(price string, quantity int64, computed bool) tftypes.Value {
		set := map[string]tftypes.Value{"price": tfString(price), "quantity": tftypes.NewValue(tftypes.Number, quantity)}
		if computed {
			set["id"], set["currency"] = tfString("li_"+price), tfString("usd")
		}
		return obj(itemType, set)
	}
	list := func(computed bool, prices ...string) tftypes.Value {
		var items []tftypes.Value
		for _, price := range prices {
			items = append(items, item(price, 1, computed))
		}
		return tftypes.NewValue(listType, items)
	}
	state := p.value(map[string]tftypes.Value{"id": tfString("lst_1"), "name": tfString("n"), "items": list(true, "pa", "pb")})
	for name, tc := range map[string]struct {
		prices []string
		want   bool
	}{
		"reordered":   {[]string{"pb", "pa"}, false},
		"a new price": {[]string{"pa", "pc"}, true},
		"one dropped": {[]string{"pa"}, true},
	} {
		config := p.value(map[string]tftypes.Value{"name": tfString("n"), "items": list(false, tc.prices...)})
		proposed := p.value(map[string]tftypes.Value{"id": tfString("lst_1"), "name": tfString("n"), "items": list(false, tc.prices...)})
		_, replace := p.plan(state, proposed, config)
		if got := len(replace) > 0; got != tc.want {
			t.Errorf("%s: replaces = %v (%v), want %v", name, got, replace, tc.want)
		}
	}
}

// A follow-up update that fails after the attach leaves a tainted resource that still knows
// its parent and the attached object, so a refresh or destroy can address it.
func TestFailedFollowUpKeepsTheParentInTheEarlyState(t *testing.T) {
	decl := attachDeclaration()
	decl.Paths.Update = "/v1/widgets/{id}"
	table := []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "label", Shape: fields.String, Update: true, Response: true},
	}
	client := clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		if req.Path == "/v1/widgets/wid_test/attach" {
			return &stripeapi.Response{Status: http.StatusOK, Body: wire.Object{{Name: "id", Value: wire.String("wid_new")}}}, nil
		}
		return nil, &stripeapi.APIError{Status: http.StatusBadRequest, Message: "refused"}
	})
	r := configured(t, decl, table, client)
	plan := r.fill(map[string]attr.Value{"owner": types.StringValue("own_1"), "target": types.StringValue("wid_test"), "label": types.StringValue("x")})
	ctx := context.Background()
	resp := resource.CreateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("the failed follow-up must fail the create")
	}
	var state basetypes.ObjectValue
	noErrors(t, resp.State.Get(ctx, &state))
	for name, want := range map[string]string{"id": "wid_new", "owner": "own_1", "target": "wid_test"} {
		if got := str(t, state, name); got != want {
			t.Errorf("early state %s = %q, want %q", name, got, want)
		}
	}
}

// An element of a FixedKeys list that cannot be matched by key is an error, not a guess:
// the positional fallback of an ordinary keyed list would give it another element's id.
func TestFixedKeysListNeverMatchesByPosition(t *testing.T) {
	items := wire.Array{wireItem("li_A", "pa", "usd"), wireItem("li_B", "pb", "usd")}
	keyless := map[string]attr.Value{"id": types.StringUnknown(), "price": types.StringUnknown(), "quantity": types.Int64Value(1), "currency": types.StringUnknown()}
	body := wire.Object{
		{Name: "id", Value: wire.String("lst_1")},
		{Name: "name", Value: wire.String("n")},
		{Name: "items", Value: items},
	}
	for name, decl := range map[string]Declaration{"fixed": fixedDeclaration(), "ordinary": keyedDeclaration()} {
		r := configured(t, decl, fixedTable(), newStub())
		prior := fixedPlan(r, []map[string]attr.Value{keyless, fixedCfg("pb", 1)})
		_, diags := r.m.decodeState(body, prior, r.objType, false, nil)
		if got := diags.HasError(); got != (name == "fixed") {
			t.Errorf("%s list: error = %v (%v)", name, got, diags)
		}
	}
}

// A field of an element that the plan check guards and Stripe takes on update (a payment
// link line item's `adjustable_quantity`) is restated from the state when the list is sent,
// so a list Stripe reads as a statement keeps it. A field only a create takes is not sent.
func TestFixedKeysListRestatesGuardedFieldsFromTheState(t *testing.T) {
	decl := fixedDeclaration()
	decl.ReplaceOn = []string{"items.note"}
	r := configured(t, decl, fixedTable(), newStub())
	withNote := func(row map[string]attr.Value, note attr.Value) map[string]attr.Value {
		row["note"] = note
		return row
	}
	state := []map[string]attr.Value{withNote(fixedState("li_A", "pa", 1), sv("keep")), fixedState("li_B", "pb", 1)}
	cfg := []map[string]attr.Value{fixedCfg("pa", 1), fixedCfg("pb", 4)}
	for name, note := range map[string]attr.Value{"planned as the state has it": sv("keep"), "left unknown": types.StringUnknown()} {
		plan := []map[string]attr.Value{withNote(fixedRow("pa", 1), note), fixedRow("pb", 4)}
		params, err := r.m.encodeUpdate(fixedPlan(r, plan), fixedPlan(r, state), raw(t, fixedPlan(r, cfg)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := `{"items":[{"quantity":1,"note":"keep","id":"li_A"},{"quantity":4,"id":"li_B"}]}`
		if got := wireOf(t, params); got != want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, want)
		}
	}
}
