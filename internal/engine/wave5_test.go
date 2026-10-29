package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

func listTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
		{Name: "items", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "id", Shape: fields.String, Update: true, Response: true},
			{Name: "price", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "quantity", Shape: fields.Int, Create: true, Update: true, Response: true, ServerDefaulted: true},
			{Name: "note", Shape: fields.String, Create: true, Update: true},
			{Name: "price_data", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
				{Name: "product", Shape: fields.String, Create: true, Update: true},
				{Name: "unit_amount", Shape: fields.Int, Create: true, Update: true},
				{Name: "unit_amount_decimal", Shape: fields.String, Create: true, Update: true},
				{Name: "tax_behavior", Shape: fields.String, Create: true, Update: true},
			}},
			{Name: "start_date", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "currency", Shape: fields.String, Create: true, Response: true, ServerDefaulted: true},
		}},
		{Name: "region", Shape: fields.String, Create: true, Response: true, ServerDefaulted: true},
		{Name: "proration_behavior", Shape: fields.String, Create: true, Update: true},
		{Name: "payment_behavior", Shape: fields.String, Create: true, Update: true},
		{Name: "metadata", Shape: fields.StringMap, Create: true, Update: true, Response: true},
	}
}

func listDeclaration() Declaration {
	return Declaration{
		TypeName: "stripe_listed",
		IDPrefix: "lst_",
		Paths:    Paths{Create: "/v1/listed", Retrieve: "/v1/listed/{id}", Update: "/v1/listed/{id}", Delete: "/v1/listed/{id}"},
		Delete:   Delete,
	}
}

func keyedDeclaration() Declaration {
	d := listDeclaration()
	d.MatchBy = map[string]string{"items": "price"}
	d.MatchData = map[string]string{"items": "price_data"}
	d.SendWithUpdate = []string{"proration_behavior", "payment_behavior"}
	d.SendWithUpdateOn = []string{"items"}
	return d
}

func elements(r *Resource, rows ...map[string]attr.Value) attr.Value {
	itemType := r.objType.AttrTypes["items"].(interface{ ElementType() attr.Type }).ElementType()
	elems := make([]attr.Value, len(rows))
	for i, row := range rows {
		elems[i] = object(itemType, row)
	}
	return types.ListValueMust(itemType, elems)
}

func sv(s string) attr.Value { return types.StringValue(s) }

var priceDataType = map[string]attr.Type{
	"product": types.StringType, "unit_amount": types.Int64Type, "unit_amount_decimal": types.StringType, "tax_behavior": types.StringType,
}

// pdata is the `price_data` of an item: a product named by name and a fixed amount.
func pdata(name string) attr.Value { return pdataTax(name, "") }

// pdataTax is pdata with a tax behaviour.
func pdataTax(name, tax string) attr.Value {
	var behaviour attr.Value = types.StringNull()
	if tax != "" {
		behaviour = sv(tax)
	}
	return types.ObjectValueMust(priceDataType, map[string]attr.Value{
		"product": sv(name), "unit_amount": types.Int64Value(100), "unit_amount_decimal": types.StringNull(), "tax_behavior": behaviour,
	})
}

// wireItemData is an item Stripe built from price_data(name): its price is an object that
// carries the data.
func wireItemData(id, price, name string) wire.Object {
	return wireItemTax(id, price, name, "unspecified")
}

// wireItemTax is wireItemData with the tax behaviour Stripe reports.
func wireItemTax(id, price, name, tax string) wire.Object {
	o := wireItem(id, price, "usd")
	for i := range o {
		if o[i].Name == "price" {
			o[i].Value = wire.Object{{Name: "id", Value: wire.String(price)}, {Name: "product", Value: wire.String(name)},
				{Name: "unit_amount", Value: wire.Int(100)}, {Name: "tax_behavior", Value: wire.String(tax)}}
		}
	}
	return o
}

// stateRow is an item as Stripe returned it, planRow as the plan has it (what the
// configuration leaves out is unknown), and cfgRow as the configuration has it.
func stateRow(id, price string) map[string]attr.Value {
	return map[string]attr.Value{"id": sv(id), "price": sv(price), "quantity": types.Int64Value(1), "currency": sv("usd")}
}

func planRow(price string, quantity int64) map[string]attr.Value {
	row := map[string]attr.Value{"id": types.StringUnknown(), "price": sv(price), "quantity": types.Int64Unknown(), "currency": types.StringUnknown()}
	if quantity > 0 {
		row["quantity"] = types.Int64Value(quantity)
	}
	return row
}

func cfgRow(price string, quantity int64) map[string]attr.Value {
	row := map[string]attr.Value{"price": sv(price)}
	if quantity > 0 {
		row["quantity"] = types.Int64Value(quantity)
	}
	return row
}

// keyedUpdate encodes the update from the state items to the plan items; each plan row has a
// configuration row.
func keyedUpdate(t *testing.T, state []map[string]attr.Value, plan []map[string]attr.Value, config []map[string]attr.Value, mutate func(*Declaration)) (wire.Object, error) {
	t.Helper()
	decl := keyedDeclaration()
	if mutate != nil {
		mutate(&decl)
	}
	r := configured(t, decl, listTable(), newStub())
	base := map[string]attr.Value{"name": sv("n")}
	with := func(rows []map[string]attr.Value) basetypes.ObjectValue {
		return r.fill(map[string]attr.Value{"name": base["name"], "items": elements(r, rows...)})
	}
	return r.m.encodeUpdate(with(plan), with(state), raw(t, with(config)))
}

// Items are matched by price, never by position: removing the middle of three deletes that
// one, a swap deletes the old price and adds the new, an insertion adds one, and a reorder
// sends nothing.
func TestKeyedListMatchesItemsByPrice(t *testing.T) {
	abc := []map[string]attr.Value{stateRow("si_A", "pa"), stateRow("si_B", "pb"), stateRow("si_C", "pc")}
	for name, tc := range map[string]struct {
		plan, config []map[string]attr.Value
		want         string
	}{
		"remove the middle": {
			plan:   []map[string]attr.Value{planRow("pa", 0), planRow("pc", 0)},
			config: []map[string]attr.Value{cfgRow("pa", 0), cfgRow("pc", 0)},
			want:   `{"items":[{"id":"si_B","deleted":true}]}`,
		},
		"swap one price": {
			plan:   []map[string]attr.Value{planRow("pa", 0), planRow("pd", 0), planRow("pc", 0)},
			config: []map[string]attr.Value{cfgRow("pa", 0), cfgRow("pd", 0), cfgRow("pc", 0)},
			want:   `{"items":[{"price":"pd"},{"id":"si_B","deleted":true}]}`,
		},
		"insert in the middle": {
			plan:   []map[string]attr.Value{planRow("pa", 0), planRow("px", 0), planRow("pb", 0), planRow("pc", 0)},
			config: []map[string]attr.Value{cfgRow("pa", 0), cfgRow("px", 0), cfgRow("pb", 0), cfgRow("pc", 0)},
			want:   `{"items":[{"price":"px"}]}`,
		},
		"reorder only": {
			plan:   []map[string]attr.Value{planRow("pc", 0), planRow("pa", 0), planRow("pb", 0)},
			config: []map[string]attr.Value{cfgRow("pc", 0), cfgRow("pa", 0), cfgRow("pb", 0)},
			want:   `{}`,
		},
		"change a quantity": {
			plan:   []map[string]attr.Value{planRow("pa", 0), planRow("pb", 4), planRow("pc", 0)},
			config: []map[string]attr.Value{cfgRow("pa", 0), cfgRow("pb", 4), cfgRow("pc", 0)},
			want:   `{"items":[{"price":"pb","quantity":4,"id":"si_B"}]}`,
		},
	} {
		params, err := keyedUpdate(t, abc, tc.plan, tc.config, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := wireOf(t, params); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tc.want)
		}
	}
}

// An item that sets `price_data` has no price of its own to match by. It pairs with a state
// item only when the two are equal; when its data changed, Stripe cannot change the item in
// place, so the old item is deleted and the new one added. Inserting or reordering such items
// changes nothing else.
func TestKeyedListPairsPriceDataItemsOnlyWhenEqual(t *testing.T) {
	data := func(id, price, priceData string) map[string]attr.Value {
		row := stateRow(id, price)
		row["price_data"] = pdata(priceData)
		return row
	}
	planData := func(priceData string) map[string]attr.Value {
		return map[string]attr.Value{"id": types.StringUnknown(), "price": types.StringUnknown(), "quantity": types.Int64Unknown(),
			"currency": types.StringUnknown(), "price_data": pdata(priceData)}
	}
	cfgData := func(priceData string) map[string]attr.Value {
		return map[string]attr.Value{"price_data": pdata(priceData)}
	}
	one := []map[string]attr.Value{data("si_A", "pa", "x")}
	two := []map[string]attr.Value{data("si_A", "pa", "x"), data("si_B", "pb", "y")}
	for name, tc := range map[string]struct {
		state, plan, config []map[string]attr.Value
		want                string
	}{
		"unchanged": {one, []map[string]attr.Value{planData("x")}, []map[string]attr.Value{cfgData("x")}, `{}`},
		"changed data": {one, []map[string]attr.Value{planData("z")}, []map[string]attr.Value{cfgData("z")},
			`{"items":[{"price_data":{"product":"z","unit_amount":100}},{"id":"si_A","deleted":true}]}`},
		"only the quantity changed": {one, []map[string]attr.Value{func() map[string]attr.Value {
			row := planData("x")
			row["quantity"] = types.Int64Value(4)
			return row
		}()}, []map[string]attr.Value{func() map[string]attr.Value {
			row := cfgData("x")
			row["quantity"] = types.Int64Value(4)
			return row
		}()}, `{"items":[{"quantity":4,"id":"si_A"}]}`},
		"insert before": {one, []map[string]attr.Value{planData("z"), planData("x")}, []map[string]attr.Value{cfgData("z"), cfgData("x")},
			`{"items":[{"price_data":{"product":"z","unit_amount":100}}]}`},
		"reorder": {two, []map[string]attr.Value{planData("y"), planData("x")}, []map[string]attr.Value{cfgData("y"), cfgData("x")}, `{}`},
	} {
		params, err := keyedUpdate(t, tc.state, tc.plan, tc.config, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := wireOf(t, params); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tc.want)
		}
	}
}

// Adding an item that sets `price_data` on update: the item Stripe creates has no price in the
// plan to recognise it by, so the update reads the list before and after, and the new item
// is part of the state.
func TestUpdateKeepsANewPriceDataItem(t *testing.T) {
	var calls []string
	list := func(items ...wire.Object) wire.Object {
		data := make(wire.Array, len(items))
		for i, it := range items {
			data[i] = it
		}
		return wire.Object{
			{Name: "id", Value: wire.String("lst_1")},
			{Name: "name", Value: wire.String("n")},
			{Name: "items", Value: wire.Object{
				{Name: "object", Value: wire.String("list")},
				{Name: "has_more", Value: wire.Bool(false)},
				{Name: "data", Value: data},
			}},
		}
	}
	client := clientFunc(func(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		calls = append(calls, req.Method)
		if req.Method == "GET" {
			return &stripeapi.Response{Body: list(wireItem("si_A", "pa", "usd"))}, nil
		}
		return &stripeapi.Response{Body: list(wireItem("si_A", "pa", "usd"), wireItemData("si_N", "pn", "x"))}, nil
	})
	r := configured(t, keyedDeclaration(), listTable(), client)
	state := r.fill(map[string]attr.Value{"id": sv("lst_1"), "name": sv("n"), "items": elements(r, stateRow("si_A", "pa"))})
	added := map[string]attr.Value{"id": types.StringUnknown(), "price": types.StringUnknown(), "quantity": types.Int64Unknown(),
		"currency": types.StringUnknown(), "price_data": pdata("x")}
	plan := r.fill(map[string]attr.Value{"id": sv("lst_1"), "name": sv("n"), "items": elements(r, planRow("pa", 0), added)})
	config := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, cfgRow("pa", 0), map[string]attr.Value{"price_data": pdata("x")})})

	var resp resource.UpdateResponse
	resp.State = tfsdk.State{Schema: r.schema, Raw: raw(t, state)}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)},
		State:  tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
		Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)},
	}, &resp)
	noErrors(t, resp.Diagnostics)
	var next basetypes.ObjectValue
	noErrors(t, resp.State.Get(context.Background(), &next))
	items := next.Attributes()["items"].(basetypes.ListValue).Elements()
	if len(items) != 2 || fieldOf(items[1], "price") != attr.Value(sv("pn")) || !fieldOf(items[1], "price_data").Equal(pdata("x")) {
		t.Fatalf("the new item must be in the state with its price and its price_data: %v", items)
	}
	if strings.Join(calls, ",") != "GET,POST" {
		t.Errorf("calls: %v", calls)
	}
}

// A managed item stays managed by its ID: a price changed from outside shows as drift on the
// same element instead of the element vanishing.
func TestManagedItemKeepsItsPlaceWhenItsPriceChangesOutside(t *testing.T) {
	r := configured(t, keyedDeclaration(), listTable(), newStub())
	row := stateRow("si_1", "pa")
	row["note"] = sv("kept")
	prior := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, row)})
	got := decodeItems(t, r, wire.Array{wireItem("si_1", "pb", "usd")}, prior, r.m.managedKeys(prior))
	if len(got) != 1 || fieldOf(got[0], "price") != attr.Value(sv("pb")) || fieldOf(got[0], "id") != attr.Value(sv("si_1")) || fieldOf(got[0], "note") != attr.Value(sv("kept")) {
		t.Fatalf("the repriced item is the same element, showing the new price: %v", got)
	}
}

// The plan of a list that names one price twice fails: Stripe does not allow it.
func TestPlanRefusesTheSameKeyTwice(t *testing.T) {
	p := newPlanner(t, keyedDeclaration(), listTable())
	itemType := p.attrTypes("items")
	item := func(price string) tftypes.Value {
		return obj(itemType, map[string]tftypes.Value{"price": tfString(price)})
	}
	listType := p.typ.(tftypes.Object).AttributeTypes["items"]
	config := p.value(map[string]tftypes.Value{
		"name":  tfString("n"),
		"items": tftypes.NewValue(listType, []tftypes.Value{item("pa"), item("pa")}),
	})
	dv, err := tfprotov6.NewDynamicValue(p.typ, config)
	if err != nil {
		t.Fatal(err)
	}
	null, err := tfprotov6.NewDynamicValue(p.typ, tftypes.NewValue(p.typ, nil))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName: p.name, PriorState: &null, ProposedNewState: &dv, Config: &dv,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range resp.Diagnostics {
		found = found || strings.Contains(d.Summary, "Duplicate price")
	}
	if !found {
		t.Fatalf("a duplicate price must fail the plan: %+v", resp.Diagnostics)
	}
}

func decodeItems(t *testing.T, r *Resource, items wire.Array, prior basetypes.ObjectValue, keep map[string]*managedSet) []attr.Value {
	t.Helper()
	body := wire.Object{
		{Name: "id", Value: wire.String("lst_1")},
		{Name: "name", Value: wire.String("n")},
		{Name: "items", Value: wire.Object{
			{Name: "object", Value: wire.String("list")},
			{Name: "has_more", Value: wire.Bool(false)},
			{Name: "data", Value: items},
		}},
	}
	got, diags := r.m.decodeState(body, prior, r.objType, false, keep)
	noErrors(t, diags)
	return got.Attributes()["items"].(basetypes.ListValue).Elements()
}

func wireItem(id, price, currency string) wire.Object {
	return wire.Object{
		{Name: "id", Value: wire.String(id)},
		{Name: "price", Value: wire.Object{{Name: "id", Value: wire.String(price)}}},
		{Name: "quantity", Value: wire.Int(1)},
		{Name: "currency", Value: wire.String(currency)},
	}
}

func fieldOf(e attr.Value, name string) attr.Value {
	return e.(basetypes.ObjectValue).Attributes()[name]
}

// Stripe may return the items in another order than the configuration has them. The decoder
// matches by price, so each element keeps its own values and the request-only `note`, and
// the list follows the prior order.
func TestDecodeMatchesKeyedElementsByKey(t *testing.T) {
	r := configured(t, keyedDeclaration(), listTable(), newStub())
	plan := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r,
		map[string]attr.Value{"id": types.StringUnknown(), "price": sv("pa"), "note": sv("first")},
		map[string]attr.Value{"id": types.StringUnknown(), "price": sv("pb"), "note": sv("second")},
	)})
	got := decodeItems(t, r, wire.Array{wireItem("si_B", "pb", "eur"), wireItem("si_A", "pa", "usd")}, plan, nil)
	if len(got) != 2 {
		t.Fatalf("items: %v", got)
	}
	for i, want := range []struct{ id, price, note, currency string }{{"si_A", "pa", "first", "usd"}, {"si_B", "pb", "second", "eur"}} {
		e := got[i]
		if fieldOf(e, "id") != attr.Value(sv(want.id)) || fieldOf(e, "price") != attr.Value(sv(want.price)) ||
			fieldOf(e, "note") != attr.Value(sv(want.note)) || fieldOf(e, "currency") != attr.Value(sv(want.currency)) {
			t.Errorf("item %d: %v, want %+v", i, e, want)
		}
	}
}

// An item added from outside never enters the state of the resource, so an update never
// sees it as removed from the configuration. An import, which has no items yet, adopts all.
func TestManagedKeysKeepOutsideItemsOutOfTheState(t *testing.T) {
	r := configured(t, keyedDeclaration(), listTable(), newStub())
	prior := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, stateRow("si_A", "pa"))})
	keep := r.m.managedKeys(prior)
	items := wire.Array{wireItem("si_A", "pa", "usd"), wireItem("si_X", "px", "usd")}
	if got := decodeItems(t, r, items, prior, keep); len(got) != 1 || fieldOf(got[0], "price") != attr.Value(sv("pa")) {
		t.Errorf("a refresh keeps the managed item only: %v", got)
	}
	if got := decodeItems(t, r, items, r.fill(map[string]attr.Value{"name": sv("n")}), r.m.managedKeys(r.fill(nil))); len(got) != 2 {
		t.Errorf("an import adopts every item: %v", got)
	}
	// After an update the plan's prices are managed too: an added item enters, a removed one leaves.
	plan := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, planRow("pb", 0))})
	keep = r.m.managedKeys(prior, plan)
	got := decodeItems(t, r, wire.Array{wireItem("si_B", "pb", "usd"), wireItem("si_X", "px", "usd")}, plan, keep)
	if len(got) != 1 || fieldOf(got[0], "price") != attr.Value(sv("pb")) {
		t.Errorf("after an update: %v", got)
	}
}

// An embedded list that Stripe cut short must not lose elements without a word, and only a
// `list` object is unwrapped.
func TestEmbeddedListWithMoreElementsIsAnError(t *testing.T) {
	r := configured(t, listDeclaration(), listTable(), newStub())
	envelope := func(extra ...wire.Field) wire.Object {
		return append(wire.Object{{Name: "data", Value: wire.Array{wireItem("si_A", "pa", "usd")}}}, extra...)
	}
	decode := func(items wire.Object) error {
		body := wire.Object{{Name: "id", Value: wire.String("lst_1")}, {Name: "name", Value: wire.String("n")}, {Name: "items", Value: items}}
		_, diags := r.m.decodeState(body, r.fill(nil), r.objType, false, nil)
		if !diags.HasError() {
			return nil
		}
		return &stubError{diags.Errors()[0].Detail()}
	}
	err := decode(envelope(wire.Field{Name: "object", Value: wire.String("list")}, wire.Field{Name: "has_more", Value: wire.Bool(true)}))
	if err == nil || !strings.Contains(err.Error(), "items") || !strings.Contains(err.Error(), "has_more") {
		t.Errorf("has_more must fail and name the field: %v", err)
	}
	if err := decode(envelope(wire.Field{Name: "object", Value: wire.String("list")}, wire.Field{Name: "has_more", Value: wire.Bool(false)})); err != nil {
		t.Errorf("a complete list decodes: %v", err)
	}
	body := wire.Object{{Name: "id", Value: wire.String("lst_1")}, {Name: "name", Value: wire.String("n")}, {Name: "items", Value: envelope()}}
	if _, diags := r.m.decodeState(body, r.fill(nil), r.objType, false, nil); len(diags.Warnings()) == 0 {
		t.Error("an object that is not a list is not unwrapped: it is a field in an unexpected shape")
	}
}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

func phaseTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "phases", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "start_date", Shape: fields.String, Update: true, Response: true},
			{Name: "name", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "currency", Shape: fields.String, Create: true, Response: true, ServerDefaulted: true},
			{Name: "trial", Shape: fields.Bool, Create: true, Update: true, Response: true},
		}},
	}
}

func phases(r *Resource, rows ...map[string]attr.Value) attr.Value {
	phaseType := r.objType.AttrTypes["phases"].(interface{ ElementType() attr.Type }).ElementType()
	elems := make([]attr.Value, len(rows))
	for i, row := range rows {
		elems[i] = object(phaseType, row)
	}
	return types.ListValueMust(phaseType, elems)
}

// Only the phase that has begun keeps its start; a later phase is left for Stripe to derive.
func TestCarryOnlyTheCurrentPhasesStart(t *testing.T) {
	decl := listDeclaration()
	decl.Carry = []Carry{{List: "phases", Field: "start_date", WhenPast: true}}
	r := configured(t, decl, phaseTable(), newStub())
	past, future := "1000", "4102444800"
	state := r.fill(map[string]attr.Value{"phases": phases(r,
		map[string]attr.Value{"start_date": sv(past), "name": sv("a")},
		map[string]attr.Value{"start_date": sv(future), "name": sv("b")},
	)})
	plan := r.fill(map[string]attr.Value{"phases": phases(r,
		map[string]attr.Value{"start_date": types.StringUnknown(), "name": sv("a2")},
		map[string]attr.Value{"start_date": types.StringUnknown(), "name": sv("b2")},
	)})
	params, err := r.m.encodeUpdate(plan, state, tftypes.Value{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := wireOf(t, params), `{"phases":[{"name":"a2","start_date":"1000"},{"name":"b2"}]}`; got != want {
		t.Errorf("update body:\n got %s\nwant %s", got, want)
	}
}

// A phase that began a moment ago, by a clock a little behind Stripe's, is still current.
func TestCarryTreatsAStartWithinAMinuteAsCurrent(t *testing.T) {
	decl := listDeclaration()
	decl.Carry = []Carry{{List: "phases", Field: "start_date", WhenPast: true}}
	r := configured(t, decl, phaseTable(), newStub())
	soon := time.Now().Add(30 * time.Second).Unix()
	later := time.Now().Add(5 * time.Minute).Unix()
	state := r.fill(map[string]attr.Value{"phases": phases(r,
		map[string]attr.Value{"start_date": sv(strconv.FormatInt(soon, 10)), "name": sv("a")},
		map[string]attr.Value{"start_date": sv(strconv.FormatInt(later, 10)), "name": sv("b")},
	)})
	plan := r.fill(map[string]attr.Value{"phases": phases(r,
		map[string]attr.Value{"start_date": types.StringUnknown(), "name": sv("a2")},
		map[string]attr.Value{"start_date": types.StringUnknown(), "name": sv("b2")},
	)})
	params, err := r.m.encodeUpdate(plan, state, tftypes.Value{})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"phases":[{"name":"a2","start_date":"` + strconv.FormatInt(soon, 10) + `"},{"name":"b2"}]}`
	if got := wireOf(t, params); got != want {
		t.Errorf("update body:\n got %s\nwant %s", got, want)
	}
}

// A declaration entry that names nothing it can use fails when the schema is built.
func TestCompileValidatesListEntries(t *testing.T) {
	for name, mutate := range map[string]func(*Declaration){
		"carry of an integer":      func(d *Declaration) { d.Carry = []Carry{{List: "items", Field: "quantity"}} },
		"carry of an unknown list": func(d *Declaration) { d.Carry = []Carry{{List: "nothing", Field: "id"}} },
		"carry of an unknown field": func(d *Declaration) {
			d.Carry = []Carry{{List: "items", Field: "nothing"}}
		},
		"match by an integer":         func(d *Declaration) { d.MatchBy = map[string]string{"items": "quantity"} },
		"match an unknown list":       func(d *Declaration) { d.MatchBy = map[string]string{"nothing": "price"} },
		"match data without match_by": func(d *Declaration) { d.MatchData = map[string]string{"items": "price_data"} },
		"match data of a scalar": func(d *Declaration) {
			d.MatchBy, d.MatchData = map[string]string{"items": "price"}, map[string]string{"items": "note"}
		},
		"send an unknown field": func(d *Declaration) { d.SendWithUpdate, d.SendWithUpdateOn = []string{"nothing"}, []string{"items"} },
		"send on an unknown field": func(d *Declaration) {
			d.SendWithUpdate, d.SendWithUpdateOn = []string{"proration_behavior"}, []string{"nothing"}
		},
		"send on nothing": func(d *Declaration) { d.SendWithUpdate = []string{"proration_behavior"} },
	} {
		decl := listDeclaration()
		mutate(&decl)
		if _, err := NewResource(decl, listTable()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := NewResource(keyedDeclaration(), listTable()); err != nil {
		t.Errorf("a valid declaration: %v", err)
	}
}

// How a change is applied goes with a change to the items, is never sent as null, and is
// not sent with an update of something else.
func TestSendWithUpdate(t *testing.T) {
	r := configured(t, keyedDeclaration(), listTable(), newStub())
	row := func(price string) map[string]attr.Value { return stateRow("si_"+price, price) }
	build := func(name string, metadata attr.Value, items attr.Value, proration, payment attr.Value) basetypes.ObjectValue {
		return r.fill(map[string]attr.Value{"name": sv(name), "items": items, "metadata": metadata,
			"proration_behavior": proration, "payment_behavior": payment})
	}
	nullMeta := types.MapNull(types.StringType)
	oneItem := elements(r, row("pa"))
	twoItems := elements(r, row("pa"), planRow("pb", 0))
	state := build("n", nullMeta, oneItem, sv("none"), sv("allow_incomplete"))

	// Items change: both ride along, set or not.
	params, err := r.m.encodeUpdate(build("n", nullMeta, twoItems, sv("none"), sv("allow_incomplete")), state, tftypes.Value{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"proration_behavior", "payment_behavior"} {
		if _, ok := params.Get(name); !ok {
			t.Errorf("%s must go with an item change: %s", name, wireOf(t, params))
		}
	}

	// Removed from the configuration: never null.
	params, err = r.m.encodeUpdate(build("n", nullMeta, twoItems, types.StringNull(), types.StringNull()), state, tftypes.Value{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range params {
		if _, null := f.Value.(wire.Null); null {
			t.Errorf("%s was sent as null: %s", f.Name, wireOf(t, params))
		}
	}
	if _, ok := params.Get("proration_behavior"); ok {
		t.Errorf("an unset proration_behavior is not sent: %s", wireOf(t, params))
	}

	// Only the metadata changes: neither is sent, though both are set.
	meta := types.MapValueMust(types.StringType, map[string]attr.Value{"k": sv("v")})
	params, err = r.m.encodeUpdate(build("n", meta, oneItem, sv("none"), sv("allow_incomplete")), state, tftypes.Value{})
	if err != nil {
		t.Fatal(err)
	}
	if got := wireOf(t, params); got != `{"metadata":{"k":"v"}}` {
		t.Errorf("a metadata update: %s", got)
	}
}

// A create-only value Stripe fills in for a list element is kept and never replaces at the top
// level. In a list element it replaces only when the configuration sets a different value;
// an unknown value, or one that is left out, or an element that is new, replaces nothing.
func TestServerFilledCreateOnlyValueInAListElementReplacesOnlyWhenSetAndChanged(t *testing.T) {
	r := configured(t, listDeclaration(), listTable(), newStub())
	top, ok := r.schema.Attributes["region"].(rschema.StringAttribute)
	if !ok || !top.Computed || len(top.PlanModifiers) != 2 {
		t.Fatalf("a top-level create-only computed value keeps its value and replaces: %+v", top)
	}
	list, ok := r.schema.Attributes["items"].(rschema.ListNestedAttribute)
	if !ok {
		t.Fatalf("items is %T", r.schema.Attributes["items"])
	}
	inner, ok := list.NestedObject.Attributes["currency"].(rschema.StringAttribute)
	if !ok || !inner.Computed || len(inner.PlanModifiers) != 1 {
		t.Fatalf("a list element's create-only computed value has one plan modifier: %+v", inner)
	}
	replaces := func(config, state, plan attr.Value) bool {
		req := planmodifier.StringRequest{ConfigValue: config.(basetypes.StringValue), StateValue: state.(basetypes.StringValue), PlanValue: plan.(basetypes.StringValue)}
		req.State.Raw = tftypes.NewValue(tftypes.String, "present")
		req.Plan.Raw = tftypes.NewValue(tftypes.String, "present")
		var resp planmodifier.StringResponse
		inner.PlanModifiers[0].PlanModifyString(context.Background(), req, &resp)
		return resp.RequiresReplace
	}
	null, unknown := types.StringNull(), types.StringUnknown()
	if !replaces(sv("eur"), sv("usd"), sv("eur")) {
		t.Error("a configured value that differs from the state replaces")
	}
	if replaces(sv("usd"), sv("usd"), sv("usd")) {
		t.Error("an unchanged value does not replace")
	}
	if replaces(null, sv("usd"), unknown) {
		t.Error("an unknown planned value, with none configured, does not replace")
	}
	if replaces(sv("eur"), null, sv("eur")) {
		t.Error("a new element has no state value to differ from")
	}
}

// Two items added in one apply are told apart by their price_data, not by the order Stripe
// returns them in: each keeps its own request-only values and gets its own price and ID.
func TestKeyedPriceDataItemsAddedTogetherKeepTheirOwnData(t *testing.T) {
	r := configured(t, keyedDeclaration(), listTable(), newStub())
	added := func(name string) map[string]attr.Value {
		return map[string]attr.Value{"id": types.StringUnknown(), "price": types.StringUnknown(), "quantity": types.Int64Unknown(),
			"currency": types.StringUnknown(), "price_data": pdata(name), "note": sv("for " + name)}
	}
	prior := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, planRow("pa", 0), added("x"), added("y"))})
	keep := r.m.managedKeys(r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, stateRow("si_A", "pa"))}))
	keep["items"].hint = []string{"si_A", "", ""}
	keep["items"].ids["si_NY"], keep["items"].ids["si_NX"] = true, true
	// Stripe returns the second new item first.
	got := decodeItems(t, r, wire.Array{wireItem("si_A", "pa", "usd"), wireItemData("si_NY", "price_y", "y"), wireItemData("si_NX", "price_x", "x")}, prior, keep)
	if len(got) != 3 {
		t.Fatalf("items: %v", got)
	}
	for i, want := range []struct{ id, price, note string }{{"si_A", "pa", ""}, {"si_NX", "price_x", "for x"}, {"si_NY", "price_y", "for y"}} {
		e := got[i]
		note := ""
		if s, ok := fieldOf(e, "note").(basetypes.StringValue); ok {
			note = s.ValueString()
		}
		if fieldOf(e, "id") != attr.Value(sv(want.id)) || fieldOf(e, "price") != attr.Value(sv(want.price)) || note != want.note {
			t.Errorf("item %d: %v, want %+v", i, e, want)
		}
	}
}

// Two items built from the same price_data could not be told apart, so the plan refuses them.
func TestPlanRefusesIdenticalPriceData(t *testing.T) {
	p := newPlanner(t, keyedDeclaration(), listTable())
	itemType := p.attrTypes("items")
	dataType := itemType["price_data"].(tftypes.Object).AttributeTypes
	data := func(product string) tftypes.Value {
		return obj(dataType, map[string]tftypes.Value{"product": tfString(product), "unit_amount": tftypes.NewValue(tftypes.Number, 100)})
	}
	decimal := func(product string) tftypes.Value {
		return obj(dataType, map[string]tftypes.Value{"product": tfString(product), "unit_amount_decimal": tfString("100.00")})
	}
	item := func(product string) tftypes.Value {
		if strings.HasSuffix(product, "/decimal") {
			return obj(itemType, map[string]tftypes.Value{"price_data": decimal(strings.TrimSuffix(product, "/decimal"))})
		}
		return obj(itemType, map[string]tftypes.Value{"price_data": data(product)})
	}
	listType := p.typ.(tftypes.Object).AttributeTypes["items"]
	duplicates := func(first, second string) bool {
		config := p.value(map[string]tftypes.Value{"name": tfString("n"), "items": tftypes.NewValue(listType, []tftypes.Value{item(first), item(second)})})
		dv, err := tfprotov6.NewDynamicValue(p.typ, config)
		if err != nil {
			t.Fatal(err)
		}
		null, err := tfprotov6.NewDynamicValue(p.typ, tftypes.NewValue(p.typ, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := p.server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
			TypeName: p.name, PriorState: &null, ProposedNewState: &dv, Config: &dv,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range resp.Diagnostics {
			if strings.Contains(d.Summary, "Duplicate price_data") {
				return true
			}
		}
		return false
	}
	if !duplicates("x", "x") {
		t.Error("identical price_data must fail the plan")
	}
	if duplicates("x", "y") {
		t.Error("different price_data is fine")
	}
	if !duplicates("x", "x/decimal") {
		t.Error("`unit_amount` and `unit_amount_decimal` that name the same amount are the same price")
	}
}

func TestDataMatchesComparesWhatTheObjectCarries(t *testing.T) {
	typ := map[string]attr.Type{"product": types.StringType, "unit_amount_decimal": types.StringType, "tax_behavior": types.StringType}
	data := types.ObjectValueMust(typ, map[string]attr.Value{"product": sv("prod_1"), "unit_amount_decimal": sv("1500"), "tax_behavior": types.StringNull()})
	price := func(product wire.Value, decimal string) wire.Object {
		return wire.Object{{Name: "product", Value: product}, {Name: "unit_amount_decimal", Value: wire.Decimal(decimal)}, {Name: "tax_behavior", Value: wire.String("unspecified")}}
	}
	if !dataMatches(data, price(wire.String("prod_1"), "1500.00")) || !dataMatches(data, price(wire.Object{{Name: "id", Value: wire.String("prod_1")}}, "1500")) {
		t.Error("the same product and amount match, however Stripe writes them")
	}
	if dataMatches(data, price(wire.String("prod_2"), "1500")) || dataMatches(data, price(wire.String("prod_1"), "1600")) {
		t.Error("a different product or amount does not match")
	}
}

// A less specific price_data is not mistaken for a more specific price: the exact match is
// found first, whatever order Stripe returns the items in.
func TestPriceDataMatchPrefersTheExactPrice(t *testing.T) {
	r := configured(t, keyedDeclaration(), listTable(), newStub())
	added := func(data attr.Value, note string) map[string]attr.Value {
		return map[string]attr.Value{"id": types.StringUnknown(), "price": types.StringUnknown(), "quantity": types.Int64Unknown(),
			"currency": types.StringUnknown(), "price_data": data, "note": sv(note)}
	}
	prior := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, added(pdata("p"), "plain"), added(pdataTax("p", "exclusive"), "taxed"))})
	keep := map[string]*managedSet{"items": {keys: map[string]bool{}, ids: map[string]bool{"si_P": true, "si_T": true}, hint: []string{"", ""}}}
	// Stripe returns the taxed price first.
	got := decodeItems(t, r, wire.Array{wireItemTax("si_T", "price_t", "p", "exclusive"), wireItemTax("si_P", "price_p", "p", "unspecified")}, prior, keep)
	if len(got) != 2 {
		t.Fatalf("items: %v", got)
	}
	if fieldOf(got[0], "price") != attr.Value(sv("price_p")) || fieldOf(got[0], "note") != attr.Value(sv("plain")) ||
		fieldOf(got[1], "price") != attr.Value(sv("price_t")) || fieldOf(got[1], "note") != attr.Value(sv("taxed")) {
		t.Errorf("the plain data must keep the plain price and the taxed data the taxed one: %v", got)
	}
}

// An element whose data matches nothing does not take an item that a later element's data
// describes: every data match is made before any element falls back to what is left.
func TestPriceDataFallbackWaitsForEveryDataMatch(t *testing.T) {
	r := configured(t, keyedDeclaration(), listTable(), newStub())
	added := func(data attr.Value, note string) map[string]attr.Value {
		return map[string]attr.Value{"id": types.StringUnknown(), "price": types.StringUnknown(), "quantity": types.Int64Unknown(),
			"currency": types.StringUnknown(), "price_data": data, "note": sv(note)}
	}
	prior := r.fill(map[string]attr.Value{"name": sv("n"), "items": elements(r, added(pdata("ghost"), "first"), added(pdata("y"), "second"))})
	keep := map[string]*managedSet{"items": {keys: map[string]bool{}, ids: map[string]bool{"si_Y": true}, hint: []string{"", ""}}}
	// The price carries a tax behaviour the data leaves out, so it matches loosely, not exactly.
	got := decodeItems(t, r, wire.Array{wireItemTax("si_Y", "price_y", "y", "exclusive")}, prior, keep)
	if len(got) != 1 || fieldOf(got[0], "price") != attr.Value(sv("price_y")) || fieldOf(got[0], "note") != attr.Value(sv("second")) {
		t.Errorf("the item described by the second element's data belongs to it: %v", got)
	}
}
