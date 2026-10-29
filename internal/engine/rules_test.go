package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// configOf is the configuration a plan was made from: its unknown values null.
func configOf(t *testing.T, plan basetypes.ObjectValue) tftypes.Value {
	t.Helper()
	v, err := tftypes.Transform(raw(t, plan), func(_ *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if !v.IsKnown() {
			return tftypes.NewValue(v.Type(), nil), nil
		}
		return v, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func warningsNamed(diags diag.Diagnostics, summary string) []string {
	var out []string
	for _, d := range diags.Warnings() {
		if d.Summary() == summary {
			out = append(out, d.Detail())
		}
	}
	return out
}

// E4: a create that Stripe answers with another value than configured keeps the configured
// value and warns; in strict mode it fails.
func TestApplyKeepsThePlanAndWarnsWhereStripeStoredAnotherValue(t *testing.T) {
	body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("WIDGET")}, {Name: "metadata", Value: wire.Object{}}}
	for _, strict := range []bool{false, true} {
		r := configured(t, fixtureDeclaration(), fixtureTable(), answering(&body))
		r.api.strict = strict
		plan := r.fill(map[string]attr.Value{"name": types.StringValue("widget")})
		ctx := context.Background()
		resp := resource.CreateResponse{State: tfsdk.State{Schema: r.schema, Raw: tftypes.NewValue(r.schema.Type().TerraformType(ctx), nil)}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}, Config: tfsdk.Config{Schema: r.schema, Raw: configOf(t, plan)}}, &resp)
		want := "Stripe stored `name` as `WIDGET`, not as configured (`widget`). The next plan will show the difference; configure Stripe's form to avoid it."
		if strict {
			if !hasSummary(resp.Diagnostics, "Stripe stored `name` differently") {
				t.Errorf("strict: want the difference as an error, got %v", resp.Diagnostics)
			}
			continue
		}
		noErrors(t, resp.Diagnostics)
		if got := warningsNamed(resp.Diagnostics, "Stripe stored `name` differently"); len(got) != 1 || got[0] != want {
			t.Errorf("warning: %q, want %q", got, want)
		}
		var state basetypes.ObjectValue
		noErrors(t, resp.State.Get(ctx, &state))
		if got := str(t, state, "name"); got != "widget" {
			t.Errorf("state name %q, want the planned widget", got)
		}
		if got := str(t, state, "id"); got != "wid_1" {
			t.Errorf("an unknown planned value takes Stripe's: id %q", got)
		}
	}
}

// E4: an update that Stripe answers with another value for an attribute the configuration
// leaves unset keeps the planned value without a warning; strict mode fails it, so a test
// catches a side effect the evidence did not record.
func TestApplyKeepsAnUnconfiguredPlannedValueQuietly(t *testing.T) {
	body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("w2")}, {Name: "active", Value: wire.Bool(false)}, {Name: "metadata", Value: wire.Object{}}}
	for _, strict := range []bool{false, true} {
		r := configured(t, fixtureDeclaration(), fixtureTable(), answering(&body))
		r.api.strict = strict
		known := map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "active": types.BoolValue(true), "created": types.Int64Null(), "secret": types.StringNull()}
		state := r.fill(known)
		known["name"] = types.StringValue("w2")
		plan := r.fill(known)
		config, err := tftypes.Transform(configOf(t, plan), func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
			if p.Equal(tftypes.NewAttributePath().WithAttributeName("active")) || p.Equal(tftypes.NewAttributePath().WithAttributeName("id")) {
				return tftypes.NewValue(v.Type(), nil), nil
			}
			return v, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		resp := resource.UpdateResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
		r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}, State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}, Config: tfsdk.Config{Schema: r.schema, Raw: config}}, &resp)
		if strict {
			if !hasSummary(resp.Diagnostics, "Stripe returned `active` differently") {
				t.Errorf("strict: want the difference as an error, got %v", resp.Diagnostics)
			}
			continue
		}
		noErrors(t, resp.Diagnostics)
		if len(resp.Diagnostics) != 0 {
			t.Errorf("an unconfigured difference warns: %v", resp.Diagnostics)
		}
		var next basetypes.ObjectValue
		noErrors(t, resp.State.Get(ctx, &next))
		if v := next.Attributes()["active"].(basetypes.BoolValue); !v.ValueBool() {
			t.Errorf("active %v, want the planned true", v)
		}
	}
}

// keepPlanned names the attribute a difference is in, inside lists and objects, and keeps a
// list whose length Stripe changed whole.
func TestKeepPlannedNamesTheDifference(t *testing.T) {
	elem := tftypes.Object{AttributeTypes: map[string]tftypes.Type{"sku": tftypes.String, "qty": tftypes.Number}}
	list := tftypes.List{ElementType: elem}
	item := func(sku string, qty tftypes.Value) tftypes.Value {
		return tftypes.NewValue(elem, map[string]tftypes.Value{"sku": tfString(sku), "qty": qty})
	}
	two := tftypes.NewValue(tftypes.Number, 2)
	planned := tftypes.NewValue(list, []tftypes.Value{item("a", two), item("b", tftypes.NewValue(tftypes.Number, tftypes.UnknownValue))})
	given := tftypes.NewValue(list, []tftypes.Value{item("A", two), item("b", tftypes.NewValue(tftypes.Number, 7))})
	var diffs []planDiff
	got := keepPlanned(planned, given, path.Root("items"), tftypes.NewAttributePath().WithAttributeName("items"), &diffs)
	want := tftypes.NewValue(list, []tftypes.Value{item("a", two), item("b", tftypes.NewValue(tftypes.Number, 7))})
	if !got.Equal(want) {
		t.Errorf("kept %v, want %v", got, want)
	}
	if len(diffs) != 1 || diffs[0].at.String() != "items[0].sku" || display(diffs[0].given) != "A" {
		t.Errorf("diffs %v", diffs)
	}

	diffs = nil
	shorter := tftypes.NewValue(list, []tftypes.Value{item("a", two)})
	full := tftypes.NewValue(list, []tftypes.Value{item("a", two), item("b", two)})
	if got := keepPlanned(full, shorter, path.Root("items"), tftypes.NewAttributePath().WithAttributeName("items"), &diffs); !got.Equal(full) || len(diffs) != 1 || diffs[0].at.String() != "items" {
		t.Errorf("a list Stripe shortened: kept %v, diffs %v", got, diffs)
	}
	if s := displayHCL(full); s != `[{qty = 2, sku = "a"}, {qty = 2, sku = "b"}]` {
		t.Errorf("display %s", s)
	}
}

// E5: an argument the evidence marks stable keeps its state value in an update plan that
// leaves it unset, and one it does not mark is unknown.
func TestUnsetStableArgumentKeepsItsValueInAnUpdatePlan(t *testing.T) {
	table := append(planTable(),
		fields.Field{Name: "tier", Shape: fields.String, Create: true, Update: true, Response: true, ServerDefaulted: true, Stable: true},
		fields.Field{Name: "level", Shape: fields.String, Create: true, Update: true, Response: true, ServerDefaulted: true})
	p := newPlanner(t, planDecl(), table)
	null := func(name string) tftypes.Value {
		return tftypes.NewValue(p.typ.(tftypes.Object).AttributeTypes[name], nil)
	}
	state := clone(p.base(), map[string]tftypes.Value{"tier": tfString("gold"), "level": tfString("high")})
	config := clone(state, map[string]tftypes.Value{"id": null("id"), "updated": null("updated"), "defaults": null("defaults"), "tier": null("tier"), "level": null("level"), "label": tfString("l2")})
	proposed := clone(state, map[string]tftypes.Value{"label": tfString("l2")})
	planned, _ := p.plan(p.value(state), p.value(proposed), p.value(config))
	at := func(name string) tftypes.Value {
		v, err := planned.ApplyTerraform5AttributePathStep(tftypes.AttributeName(name))
		if err != nil {
			t.Fatal(err)
		}
		return v.(tftypes.Value)
	}
	if v := at("tier"); !v.Equal(tfString("gold")) {
		t.Errorf("stable tier planned %v, want its state value", v)
	}
	if v := at("level"); v.IsKnown() {
		t.Errorf("level the evidence does not mark stable planned %v, want unknown", v)
	}
}

// E6: a field Stripe returns in another shape keeps its prior value on read and warns, naming
// the field and what came back; strict mode makes it an error.
func TestWrongShapedFieldWarnsAndKeepsItsPriorValue(t *testing.T) {
	body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("w")}, {Name: "address", Value: wire.String("1 Main St")}, {Name: "quantity", Value: wire.String("three")}}
	for _, strict := range []bool{false, true} {
		r := configured(t, fixtureDeclaration(), fixtureTable(), answering(&body))
		r.api.strict = strict
		addrType := r.objType.AttrTypes["address"].(basetypes.ObjectType)
		address := types.ObjectValueMust(addrType.AttrTypes, map[string]attr.Value{"line1": types.StringValue("1 Main St"), "zip": types.StringNull()})
		state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "address": address, "quantity": types.Int64Value(3)})
		state = types.ObjectValueMust(state.AttributeTypes(context.Background()), nullUnknowns(state.Attributes()))
		ctx := context.Background()
		resp := resource.ReadResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
		r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}, &resp)
		if strict {
			if !hasSummary(resp.Diagnostics, unexpectedShape) {
				t.Errorf("strict: want an error, got %v", resp.Diagnostics)
			}
			continue
		}
		noErrors(t, resp.Diagnostics)
		got := warningsNamed(resp.Diagnostics, unexpectedShape)
		if len(got) != 2 || !strings.Contains(strings.Join(got, "\n"), "`address` in a shape the provider does not read: expected an object, got a string") ||
			!strings.Contains(strings.Join(got, "\n"), "`quantity` in a shape the provider does not read: expected an integer, got a string") {
			t.Errorf("warnings %q", got)
		}
		var next basetypes.ObjectValue
		noErrors(t, resp.State.Get(ctx, &next))
		if !next.Attributes()["address"].Equal(address) || !next.Attributes()["quantity"].Equal(types.Int64Value(3)) {
			t.Errorf("the prior values are not kept: %v", next)
		}
	}
}

func nullUnknowns(attrs map[string]attr.Value) map[string]attr.Value {
	out := make(map[string]attr.Value, len(attrs))
	for k, v := range attrs {
		if v.IsUnknown() {
			v = nullOf(v.Type(context.Background()))
		}
		out[k] = v
	}
	return out
}

// discountsTable is an object whose `discounts` takes Stripe's request shape in the resource
// and the response's IDs in the data source, as the generator splits it.
func discountsTable() []fields.Field {
	str := func(name string) fields.Field {
		return fields.Field{Name: name, Shape: fields.String, Create: true, Update: true}
	}
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
		{Name: "discounts", Shape: fields.ObjectList, Create: true, Update: true, ServerDefaulted: true,
			Fields:     []fields.Field{str("coupon"), str("discount"), str("promotion_code")},
			DataSource: &fields.Field{Name: "discounts", Shape: fields.ScalarList, Elem: fields.String, Response: true}},
	}
}

func discount(id, coupon string, promotion wire.Value) wire.Object {
	return wire.Object{{Name: "id", Value: wire.String(id)}, {Name: "object", Value: wire.String("discount")}, {Name: "promotion_code", Value: promotion},
		{Name: "source", Value: wire.Object{{Name: "coupon", Value: wire.String(coupon)}, {Name: "type", Value: wire.String("coupon")}}}}
}

// The resource maps the discounts Stripe returns back onto the configured elements by each of
// the three keys, whatever their order, drops an element no discount matches and adds one for
// a discount no element names (D3, "Objects in, IDs out").
func TestDiscountsMapBackByKey(t *testing.T) {
	r := configured(t, planDecl(), discountsTable(), newStub())
	if r.m.discounts == nil {
		t.Fatal("the split discounts are not mapped")
	}
	elemType := r.objType.AttrTypes["discounts"].(basetypes.ListType).ElemType.(basetypes.ObjectType)
	elem := func(form, id string) attr.Value {
		attrs := map[string]attr.Value{"coupon": types.StringNull(), "discount": types.StringNull(), "promotion_code": types.StringNull()}
		attrs[form] = types.StringValue(id)
		return types.ObjectValueMust(elemType.AttrTypes, attrs)
	}
	list := func(elems ...attr.Value) attr.Value { return types.ListValueMust(elemType, elems) }
	decode := func(prior attr.Value, found ...wire.Value) attr.Value {
		t.Helper()
		body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("w")}, {Name: "discounts", Value: wire.Array(found)}}
		got, diags := r.m.decodeState(body, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "discounts": prior}), r.objType, false, nil)
		noErrors(t, diags)
		return got.Attributes()["discounts"]
	}
	configured := list(elem("coupon", "co_1"), elem("promotion_code", "promo_2"), elem("discount", "di_3"))
	got := decode(configured,
		discount("di_3", "co_3", wire.Null{}),
		discount("di_2", "co_2", wire.Object{{Name: "id", Value: wire.String("promo_2")}}),
		discount("di_1", "co_1", wire.Null{}),
		discount("di_9", "co_9", wire.Null{}))
	if want := list(elem("coupon", "co_1"), elem("promotion_code", "promo_2"), elem("discount", "di_3"), elem("discount", "di_9")); !got.Equal(want) {
		t.Errorf("mapped %v, want %v", got, want)
	}
	// A coupon matches only a discount without a promotion code.
	if got := decode(list(elem("coupon", "co_2")), discount("di_2", "co_2", wire.String("promo_2"))); !got.Equal(list(elem("discount", "di_2"))) {
		t.Errorf("a coupon matched a promotion code's discount: %v", got)
	}
	// A configured element Stripe no longer has is dropped.
	if got := decode(list(elem("discount", "di_gone"))); !got.Equal(list()) {
		t.Errorf("a removed discount: %v", got)
	}
	// None configured and none returned stays null.
	if got := decode(types.ListUnknown(elemType)); !got.IsNull() {
		t.Errorf("no discounts: %v", got)
	}
	// IDs where the resource expands discounts are a field in an unexpected shape.
	body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("w")}, {Name: "discounts", Value: wire.Array{wire.String("di_1")}}}
	if _, diags := r.m.decodeState(body, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "discounts": configured}), r.objType, false, nil); len(warningsNamed(diags, unexpectedShape)) != 1 {
		t.Errorf("unexpanded discounts: %v", diags)
	}
}

// The resource expands `discounts` on every request that returns the object; the data source,
// which reads the IDs, never does.
func TestDiscountsAreExpandedOnResourceRequestsOnly(t *testing.T) {
	stub := newStub()
	r := configured(t, planDecl(), discountsTable(), stub)
	elemType := r.objType.AttrTypes["discounts"].(basetypes.ListType).ElemType.(basetypes.ObjectType)
	none := types.ListValueMust(elemType, nil)
	state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "discounts": none}))
	r.read(t, state)
	for _, req := range stub.requests() {
		expand, _ := req.Params.Get("expand")
		if arr, ok := expand.(wire.Array); !ok || len(arr) != 1 || arr[0] != wire.String("discounts") {
			t.Errorf("%s %s: expand %v", req.Method, req.Path, expand)
		}
	}
	ds, err := compile(planDecl(), fields.ForDataSource(discountsTable()))
	if err != nil {
		t.Fatal(err)
	}
	if ds.discounts != nil {
		t.Error("the data source maps discounts")
	}
}

// E3: the Import section lists the arguments Stripe never returns, unless the declaration's
// note names them, and flags a replacement as a warning.
func TestImportNoteListsTheArgumentsStripeNeverReturns(t *testing.T) {
	m, err := compile(fixtureDeclaration(), fixtureTable())
	if err != nil {
		t.Fatal(err)
	}
	note, level := m.importNote()
	if want := "Stripe does not return `passcode`, so an imported widget has it null: a configuration that sets it plans an update after the import."; note != want || level != NoteInfo {
		t.Errorf("note %q (%v), want %q", note, level, want)
	}

	decl := fixtureDeclaration()
	decl.ImportNote = "Stripe never returns `passcode`."
	table := append(fixtureTable(), fields.Field{Name: "token", Shape: fields.String, Create: true}, fields.Field{Name: "hint", Shape: fields.String, Update: true})
	if m, err = compile(decl, table); err != nil {
		t.Fatal(err)
	}
	note, level = m.importNote()
	if want := "Stripe never returns `passcode`. Stripe does not return `hint` or `token`, so an imported widget has them null: a configuration that sets `hint` plans an update after the import, and one that sets `token` plans a replacement."; note != want || level != NoteWarning {
		t.Errorf("note %q (%v), want %q", note, level, want)
	}
}
