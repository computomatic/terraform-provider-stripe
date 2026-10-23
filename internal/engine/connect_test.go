package engine

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// accountInvalid is the 403 Stripe answers for a connected account that is missing or
// deleted.
func accountInvalid() error {
	return &stripeapi.APIError{Status: http.StatusForbidden, Type: "invalid_request_error", Code: "account_invalid", Message: "The provided key does not have access to account"}
}

// goneStripe answers every retrieve and delete with err.
func goneStripe(stub *stubStripe, err error) clientFunc {
	return func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		if req.Method == "GET" || req.Method == "DELETE" {
			return nil, err
		}
		return stub.Do(ctx, req)
	}
}

// requireGoneWarning checks for the one warning that says a 403 was taken for gone, with the
// code and both explanations.
func requireGoneWarning(t *testing.T, where string, diags diag.Diagnostics) {
	t.Helper()
	warnings := diags.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("%s: want one warning, got %v", where, diags)
	}
	text := warnings[0].Summary() + " " + warnings[0].Detail()
	for _, want := range []string{"account_invalid", "deleted", "revoked"} {
		if !strings.Contains(text, want) {
			t.Errorf("%s: the warning must mention %q: %s", where, want, text)
		}
	}
}

func goneDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.GoneOnForbidden = "account_invalid"
	return decl
}

// A declaration that names a 403 code takes that answer for a missing object on refresh and
// destroy, as it takes a 404. Any other 403, and a 403 of a declaration that names none, is
// an error.
func TestForbiddenCodeMeansGone(t *testing.T) {
	stub := newStub()
	r := configured(t, goneDeclaration(), fixtureTable(), goneStripe(stub, accountInvalid()))
	state, _ := r.create(t, createPlan(r))
	diags := r.readDiags(t, state)
	noErrors(t, diags)
	requireGoneWarning(t, "read", diags)
	diags = r.destroy(t, state)
	noErrors(t, diags)
	requireGoneWarning(t, "destroy", diags)

	other := &stripeapi.APIError{Status: http.StatusForbidden, Type: "invalid_request_error", Code: "permission_denied", Message: "no"}
	r = configured(t, goneDeclaration(), fixtureTable(), goneStripe(stub, other))
	if !r.readDiags(t, state).HasError() {
		t.Error("a 403 with another code must fail the refresh")
	}
	if diags := r.destroy(t, state); !diags.HasError() {
		t.Error("a 403 with another code must fail the destroy")
	}

	r = configured(t, fixtureDeclaration(), fixtureTable(), goneStripe(stub, accountInvalid()))
	if !r.readDiags(t, state).HasError() {
		t.Error("a declaration that names no code must not take a 403 for gone")
	}
	if diags := r.destroy(t, state); !diags.HasError() {
		t.Error("a declaration that names no code must not take a 403 for a destroyed object")
	}
}

func TestForbiddenCodeMeansNotFoundForADataSource(t *testing.T) {
	ds := dataSource(t, goneDeclaration(), goneStripe(newStub(), accountInvalid()))
	_, resp := ds.read(t, ds.config(map[string]string{"id": "wid_1"}))
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "no stripe_widget found for id=wid_1") {
		t.Errorf("want the not found error, got %v", resp.Diagnostics)
	}
	requireGoneWarning(t, "data source", resp.Diagnostics)

	ds = dataSource(t, fixtureDeclaration(), goneStripe(newStub(), accountInvalid()))
	_, resp = ds.read(t, ds.config(map[string]string{"id": "wid_1"}))
	if !resp.Diagnostics.HasError() || strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "no stripe_widget found") {
		t.Errorf("a declaration that names no code reports Stripe's error: %v", resp.Diagnostics)
	}
}

func wrapDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.CreateWrap = &CreateWrap{
		Key:     "bank",
		Fixed:   wire.Object{{Name: "object", Value: wire.String("widget")}},
		Outside: []string{"metadata"},
	}
	return decl
}

// The create fields go under the wrap key with the fixed fields, and the fields listed as
// outside stay beside it. An update is not wrapped.
func TestCreateWrapNestsTheCreateFields(t *testing.T) {
	stub := newStub()
	r := configured(t, wrapDeclaration(), fixtureTable(), stub)
	state, _ := r.create(t, createPlan(r))
	create := stub.requests()[0]
	if create.Method != "POST" || create.Path != "/v1/widgets" {
		t.Fatalf("first request %s %s", create.Method, create.Path)
	}
	bank, ok := create.Params.Get("bank")
	if !ok {
		t.Fatalf("no wrapped hash: %s", wireOf(t, create.Params))
	}
	inner, _ := bank.(wire.Object)
	if kind, _ := inner.GetString("object"); kind != "widget" {
		t.Errorf("the fixed field is missing: %s", wireOf(t, inner))
	}
	for _, name := range []string{"name", "kind", "passcode"} {
		if _, in := inner.Get(name); !in {
			t.Errorf("%s must be inside the hash: %s", name, wireOf(t, create.Params))
		}
		if _, out := create.Params.Get(name); out {
			t.Errorf("%s must not stay beside the hash", name)
		}
	}
	if _, in := inner.Get("metadata"); in {
		t.Error("metadata is listed as outside, so it must not go into the hash")
	}
	if _, out := create.Params.Get("metadata"); !out {
		t.Errorf("metadata must stay beside the hash: %s", wireOf(t, create.Params))
	}

	stub.reset()
	plan := state.Attributes()
	plan["name"] = types.StringValue("renamed")
	r.update(t, r.fill(plan), state)
	for _, req := range stub.requests() {
		if req.Method == "POST" {
			if _, wrapped := req.Params.Get("bank"); wrapped {
				t.Errorf("an update must not be wrapped: %s", wireOf(t, req.Params))
			}
		}
	}
}

func TestCreateWrapMustNameCreateFields(t *testing.T) {
	for name, wrap := range map[string]*CreateWrap{
		"no key":          {Outside: []string{"metadata"}},
		"unknown outside": {Key: "bank", Outside: []string{"nothing"}},
		"not a create":    {Key: "bank", Outside: []string{"created"}},
	} {
		decl := fixtureDeclaration()
		decl.CreateWrap = wrap
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("%s: the wrap must not compile", name)
		}
	}
}

func deleteKeepDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.Delete = Delete
	decl.RefuseWhen = &RefuseWhen{Field: "is_default", Value: wire.Bool(true), Reason: "Stripe refuses to delete the default widget."}
	return decl
}

// deleteRefusing refuses DELETE with err and answers retrieves with is_default set.
func deleteRefusing(stub *stubStripe, err error, isDefault bool) clientFunc {
	return func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		if req.Method == "DELETE" {
			return nil, err
		}
		resp, doErr := stub.Do(ctx, req)
		if doErr != nil || req.Method != "GET" {
			return resp, doErr
		}
		body, _ := resp.Body.(wire.Object)
		return &stripeapi.Response{Status: resp.Status, Body: put(body, "is_default", wire.Bool(isDefault))}, nil
	}
}

var deleteRefusal = &stripeapi.APIError{Status: 400, Type: "invalid_request_error", Message: "You cannot delete the default external account for a currency."}

// A refused DELETE of an object the retrieve shows is protected fails with Stripe's error and
// the guidance, as a refused archive does; one that is not protected fails with Stripe's
// error only.
func TestDeleteRefusedForTheProtectedObject(t *testing.T) {
	stub := newStub()
	r := configured(t, deleteKeepDeclaration(), defaultTable(), deleteRefusing(stub, deleteRefusal, true))
	state, _ := r.create(t, createPlan(r))
	if diags := r.destroy(t, state); !refusedDestroy(diags) || len(diags.Errors()) != 2 {
		t.Errorf("want Stripe's error and the refusal: %v", diags)
	}

	r = configured(t, deleteKeepDeclaration(), defaultTable(), deleteRefusing(stub, deleteRefusal, false))
	if diags := r.destroy(t, state); !diags.HasError() || refusedDestroy(diags) {
		t.Errorf("a refused DELETE of an object that is not protected fails with Stripe's error only: %v", diags)
	}
}

// A data source has no prior value to keep, so a field the response never carries is null
// even when the response has a key of that name in another shape.
func TestDataSourceIgnoresAKeyOfARequestOnlyField(t *testing.T) {
	stub := newStub()
	r := fixtureResource(t, stub)
	r.create(t, createPlan(r))
	ds := dataSource(t, fixtureDeclaration(), clientFunc(func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		resp, err := stub.Do(ctx, req)
		if err != nil || req.Method != "GET" {
			return resp, err
		}
		body, _ := resp.Body.(wire.Object)
		return &stripeapi.Response{Status: resp.Status, Body: put(body, "passcode", wire.Object{{Name: "status", Value: wire.String("active")}})}, nil
	}))
	state, resp := ds.read(t, ds.config(map[string]string{"id": "wid_1"}))
	noErrors(t, resp.Diagnostics)
	if !state.Attributes()["passcode"].IsNull() {
		t.Errorf("passcode: %v", state.Attributes()["passcode"])
	}
	if str(t, state, "name") != "w" {
		t.Errorf("the other fields must still be read: %v", state)
	}
}

// readDiags refreshes the state and returns the diagnostics.
func (r *Resource) readDiags(t *testing.T, state basetypes.ObjectValue) diag.Diagnostics {
	t.Helper()
	resp := resource.ReadResponse{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}
	r.Read(context.Background(), resource.ReadRequest{State: tfsdk.State{Schema: r.schema, Raw: raw(t, state)}}, &resp)
	return resp.Diagnostics
}

// A 404 is plain absence and needs no warning.
func TestNotFoundHasNoGoneWarning(t *testing.T) {
	r := configured(t, goneDeclaration(), fixtureTable(), goneStripe(newStub(), notFound()))
	state, _ := r.create(t, createPlan(r))
	diags := r.readDiags(t, state)
	if len(diags) != 0 {
		t.Errorf("a 404 must not warn: %v", diags)
	}
}

// refuseTable is the fixture table without its replace sources below the top level, which
// RefuseChange does not support.
func refuseTable() []fields.Field {
	table := fixtureTable()
	for i := range table {
		if table[i].Name != "address" {
			continue
		}
		for j := range table[i].Fields {
			table[i].Fields[j].Update = true
		}
	}
	return table
}

func refusingChange() Declaration {
	decl := fixtureDeclaration()
	decl.RefuseChange = []string{"url"}
	return decl
}

// A plan that changes a field listed in RefuseChange on an existing object fails before
// Stripe is called. Creating, leaving it as it is and a value still unknown pass.
func TestRefuseChangeFailsThePlan(t *testing.T) {
	r := configured(t, refusingChange(), refuseTable(), newStub())
	state, _ := r.create(t, createPlan(r))
	plan := func(url attr.Value) basetypes.ObjectValue {
		attrs := map[string]attr.Value{}
		for k, v := range state.Attributes() {
			attrs[k] = v
		}
		attrs["url"] = url
		return r.fill(attrs)
	}
	modify := func(p basetypes.ObjectValue, prior basetypes.ObjectValue) *resource.ModifyPlanResponse {
		resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, p)}}
		r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, p)},
			Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, p)},
			State:  tfsdk.State{Schema: r.schema, Raw: raw(t, prior)},
		}, resp)
		return resp
	}
	if resp := modify(plan(types.StringValue("https://other.example")), state); !resp.Diagnostics.HasError() ||
		!strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "not supported yet") ||
		!strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "ignore_changes") {
		t.Errorf("a changed value must fail the plan: %v", resp.Diagnostics)
	}
	if resp := modify(plan(types.StringNull()), state); !resp.Diagnostics.HasError() {
		t.Error("a removed value is a change")
	}
	if resp := modify(plan(state.Attributes()["url"]), state); resp.Diagnostics.HasError() {
		t.Errorf("an unchanged value must pass: %v", resp.Diagnostics)
	}
	if resp := modify(plan(types.StringUnknown()), state); resp.Diagnostics.HasError() {
		t.Errorf("an unknown value is left for apply: %v", resp.Diagnostics)
	}
	refused := modify(plan(types.StringValue("https://other.example")), state).Diagnostics.Errors()
	switch {
	case len(refused) == 0:
		t.Error("a changed value must fail the plan")
	case strings.Contains(refused[0].Detail(), "-replace"):
		t.Errorf("-replace is not seen by the plan, so the error must not recommend it: %v", refused)
	case !strings.Contains(refused[0].Detail(), "taint"):
		t.Errorf("the error must recommend taint: %v", refused)
	}
	other := fixtureDeclaration()
	r = configured(t, other, refuseTable(), newStub())
	if resp := modify(plan(types.StringValue("https://other.example")), state); resp.Diagnostics.HasError() {
		t.Errorf("a declaration without the entry must not refuse: %v", resp.Diagnostics)
	}
}

// A required field, such as a personalization design's `physical_bundle`, is refused as an
// optional one is.
func TestRefuseChangeTakesARequiredField(t *testing.T) {
	decl := fixtureDeclaration()
	decl.RefuseChange = []string{"name"}
	r := configured(t, decl, refuseTable(), newStub())
	state, _ := r.create(t, createPlan(r))
	attrs := map[string]attr.Value{}
	for k, v := range state.Attributes() {
		attrs[k] = v
	}
	attrs["name"] = types.StringValue("Renamed")
	plan := r.fill(attrs)
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}}
	r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, plan)},
		Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)},
		State:  tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
	}, resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Summary(), "`name`") {
		t.Errorf("a changed required value must fail the plan: %v", resp.Diagnostics)
	}
}

func TestRefuseChangeMustNameAConfigurableField(t *testing.T) {
	for name, field := range map[string]string{"unknown": "nothing", "output only": "created", "nested": "address.line1"} {
		decl := fixtureDeclaration()
		decl.RefuseChange = []string{field}
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("%s: must not compile", name)
		}
	}
}

// DELETE of a protected object gets the guidance only when it was refused with a 400
// invalid_request_error. A 403 from a read-only key or a server error fails the destroy with
// Stripe's error alone, even when the retrieve shows the object is protected.
func TestDeleteRefusesOnlyOnARefusal(t *testing.T) {
	for name, err := range map[string]error{
		"server error":  &stripeapi.APIError{Status: 500, Type: "api_error", Message: "down"},
		"read-only key": &stripeapi.APIError{Status: 403, Type: "invalid_request_error", Code: "permission_denied", Message: "no"},
		"other type":    &stripeapi.APIError{Status: 400, Type: "card_error", Message: "no"},
	} {
		stub := newStub()
		r := configured(t, deleteKeepDeclaration(), defaultTable(), deleteRefusing(stub, err, true))
		state, _ := r.create(t, createPlan(r))
		if diags := r.destroy(t, state); !diags.HasError() || refusedDestroy(diags) {
			t.Errorf("%s: must fail the destroy with Stripe's error only: %v", name, diags)
		}
	}
}

// A plan that changes a field that forces replacement together with a refused field is a
// replace, and the new object is created with the new value, so it is not refused.
func TestRefuseChangeLetsAReplaceThrough(t *testing.T) {
	r := configured(t, refusingChange(), refuseTable(), newStub())
	state, _ := r.create(t, createPlan(r))
	modify := func(set map[string]attr.Value) *resource.ModifyPlanResponse {
		attrs := map[string]attr.Value{}
		for k, v := range state.Attributes() {
			attrs[k] = v
		}
		for k, v := range set {
			attrs[k] = v
		}
		p := r.fill(attrs)
		resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, p)}}
		r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
			Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, p)},
			Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, p)},
			State:  tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
		}, resp)
		return resp
	}
	other := types.StringValue("https://other.example")
	if resp := modify(map[string]attr.Value{"url": other}); !resp.Diagnostics.HasError() {
		t.Fatal("the control: a change of the refused field alone must fail")
	}
	// `kind` is create-only in the fixture.
	if resp := modify(map[string]attr.Value{"url": other, "kind": types.StringValue("b")}); resp.Diagnostics.HasError() {
		t.Errorf("a change together with a create-only change is a replace: %v", resp.Diagnostics)
	}
}

// A value that is known except for a nested part may match after apply, so it is left for
// apply.
func TestRefuseChangeLeavesAPartlyUnknownValue(t *testing.T) {
	decl := fixtureDeclaration()
	decl.RefuseChange = []string{"options"}
	r := configured(t, decl, refuseTable(), newStub())
	state, _ := r.create(t, createPlan(r))
	elemType := r.objType.AttrTypes["options"].(basetypes.MapType).ElemType
	options := func(amount attr.Value) attr.Value {
		return types.MapValueMust(elemType, map[string]attr.Value{"k": object(elemType, map[string]attr.Value{"amount": amount})})
	}
	modify := func(amount attr.Value) *resource.ModifyPlanResponse {
		return r.modifyPlan(t, r.fill(with(state, map[string]attr.Value{"options": options(amount)})), state)
	}
	if resp := modify(types.Int64Value(9)); !resp.Diagnostics.HasError() {
		t.Fatal("the control: a fully known change must fail")
	}
	if resp := modify(types.Int64Unknown()); resp.Diagnostics.HasError() {
		t.Errorf("a partly unknown value is left for apply: %v", resp.Diagnostics)
	}
}

// modifyPlan sends one ModifyPlan with the plan as the configuration unless config is given.
func (r *Resource) modifyPlan(t *testing.T, plan, state basetypes.ObjectValue, config ...basetypes.ObjectValue) *resource.ModifyPlanResponse {
	t.Helper()
	cfg := plan
	if len(config) > 0 {
		cfg = config[0]
	}
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)}}
	r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, cfg)},
		Plan:   tfsdk.Plan{Schema: r.schema, Raw: raw(t, plan)},
		State:  tfsdk.State{Schema: r.schema, Raw: raw(t, state)},
	}, resp)
	return resp
}

// with returns the attributes of an object value with the given ones replaced.
func with(obj basetypes.ObjectValue, set map[string]attr.Value) map[string]attr.Value {
	attrs := map[string]attr.Value{}
	for k, v := range obj.Attributes() {
		attrs[k] = v
	}
	for k, v := range set {
		attrs[k] = v
	}
	return attrs
}

// requireRefusedAlone checks that changing only the refused field fails the plan (the
// control), and that changing it together with a replace source passes.
func requireRefusedAlone(t *testing.T, r *Resource, state basetypes.ObjectValue, refused map[string]attr.Value, source map[string]attr.Value) {
	t.Helper()
	if resp := r.modifyPlan(t, r.fill(with(state, refused)), state); !resp.Diagnostics.HasError() {
		t.Fatal("the control: a change of the refused field alone must fail")
	}
	both := with(state, refused)
	for k, v := range source {
		both[k] = v
	}
	if resp := r.modifyPlan(t, r.fill(both), state); resp.Diagnostics.HasError() {
		t.Errorf("a change together with a replace source is a replace, not refused: %v", resp.Diagnostics)
	}
}

// The parent and the attached object carry RequiresReplace without being fields of the table.
func TestRefuseChangeSeesTheParentAndTheAttachment(t *testing.T) {
	table := []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "label", Shape: fields.String, Update: true, Response: true},
	}
	for _, field := range []string{"owner", "target"} {
		decl := attachDeclaration()
		decl.Paths.Update = "/v1/widgets/{id}"
		decl.RefuseChange = []string{"label"}
		r := configured(t, decl, table, newStub())
		state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "owner": types.StringValue("own_1"), "target": types.StringValue("tgt_1"), "label": types.StringValue("a")})
		requireRefusedAlone(t, r, state, map[string]attr.Value{"label": types.StringValue("b")}, map[string]attr.Value{field: types.StringValue("other")})
	}
}

// A currency-options change replaces the object, which the engine decides before the refusal.
func TestRefuseChangeSeesACurrencyOptionsReplace(t *testing.T) {
	decl := currencyDeclaration()
	decl.RefuseChange = []string{"nickname"}
	r := configured(t, decl, currencyTable(), newStub())
	state := r.fill(map[string]attr.Value{
		"id": types.StringValue("wid_1"), "currency": types.StringValue("usd"), "nickname": types.StringValue("a"),
		"currency_options": optionsValue(r, map[string]int64{"eur": 400}),
	})
	requireRefusedAlone(t, r, state, map[string]attr.Value{"nickname": types.StringValue("b")},
		map[string]attr.Value{"currency_options": optionsValue(r, map[string]int64{"eur": 450})})
	plan := r.fill(with(state, map[string]attr.Value{"nickname": types.StringValue("b"), "currency_options": optionsValue(r, map[string]int64{"eur": 450})}))
	if resp := r.modifyPlan(t, plan, state); len(resp.RequiresReplace) == 0 {
		t.Error("the currency options change must still require replacement")
	}
}

// A FixedKeys list whose keys change replaces the object, which the engine decides before the
// refusal; the same prices reordered do not, so the refusal still applies.
func TestRefuseChangeSeesAFixedKeysReplace(t *testing.T) {
	decl := fixedDeclaration()
	decl.RefuseChange = []string{"proration_behavior"}
	p := newPlanner(t, decl, fixedTable())
	itemType := p.attrTypes("items")
	listType := p.typ.(tftypes.Object).AttributeTypes["items"]
	list := func(computed bool, prices ...string) tftypes.Value {
		var items []tftypes.Value
		for _, price := range prices {
			set := map[string]tftypes.Value{"price": tfString(price), "quantity": tftypes.NewValue(tftypes.Number, 1)}
			if computed {
				set["id"], set["currency"] = tfString("li_"+price), tfString("usd")
			}
			items = append(items, obj(itemType, set))
		}
		return tftypes.NewValue(listType, items)
	}
	state := p.value(map[string]tftypes.Value{"id": tfString("lst_1"), "name": tfString("n"), "items": list(true, "pa", "pb"), "proration_behavior": tfString("none")})
	for name, tc := range map[string]struct {
		prices  []string
		refused bool
	}{
		"a new price": {[]string{"pa", "pc"}, false},
		"reordered":   {[]string{"pb", "pa"}, true},
	} {
		attrs := map[string]tftypes.Value{"id": tfString("lst_1"), "name": tfString("n"), "items": list(false, tc.prices...), "proration_behavior": tfString("create_prorations")}
		resp := p.planResponse(state, p.value(attrs), p.value(attrs))
		if got := len(resp.Diagnostics) > 0; got != tc.refused {
			t.Errorf("%s: refused = %v, want %v (%v)", name, got, tc.refused, resp.Diagnostics)
		}
		if !tc.refused && len(resp.RequiresReplace) == 0 {
			t.Errorf("%s: must plan a replace", name)
		}
	}
}

// A replace field unknown at plan time, set by the configuration (a reference to an object
// being replaced), may force replacement, so the refusal waits for apply. One the
// configuration leaves unset carries no change.
func TestRefuseChangeLeavesAnUnknownReplaceField(t *testing.T) {
	r := configured(t, refusingChange(), refuseTable(), newStub())
	state, _ := r.create(t, createPlan(r))
	plan := r.fill(with(state, map[string]attr.Value{"url": types.StringValue("https://other.example"), "kind": types.StringUnknown()}))
	if resp := r.modifyPlan(t, plan, state); resp.Diagnostics.HasError() {
		t.Errorf("an unknown replace field set by the configuration is left for apply: %v", resp.Diagnostics)
	}
	unset := r.fill(with(state, map[string]attr.Value{"url": types.StringValue("https://other.example"), "kind": types.StringNull()}))
	if resp := r.modifyPlan(t, plan, state, unset); !resp.Diagnostics.HasError() {
		t.Error("an unknown replace field the configuration leaves unset carries no change, so the refusal stands")
	}
}

// RefuseChange checks only top-level replace sources, so a declaration with one below the top
// level, in an object, a list element or a map, does not compile unless it refuses destroy; one
// with only top-level sources does.
func TestRefuseChangeRefusesNestedReplaceSources(t *testing.T) {
	decl := fixtureDeclaration()
	decl.RefuseChange = []string{"url"}
	_, err := NewResource(decl, fixtureTable())
	if err == nil || !strings.Contains(err.Error(), "address.zip") || !strings.Contains(err.Error(), "top-level replace sources") {
		t.Errorf("a create-only field inside an object must be refused, naming it: %v", err)
	}
	if _, err := NewResource(decl, refuseTable()); err != nil {
		t.Errorf("top-level replace sources only must compile: %v", err)
	}
	// A resource that refuses destroy fails the replacement in the attribute plan modifier,
	// before the RefuseChange check, so a nested source compiles there.
	decl.Delete, decl.RefuseReason, decl.Paths.Delete = Refuse, "Stripe cannot delete a widget.", ""
	if _, err := NewResource(decl, fixtureTable()); err != nil {
		t.Errorf("a refusing resource may have a nested replace source: %v", err)
	}

	list := listDeclaration()
	list.RefuseChange = []string{"proration_behavior"}
	_, err = NewResource(list, listTable())
	if err == nil || !strings.Contains(err.Error(), "items.currency") {
		t.Errorf("a server-filled create-only field in a list element must be refused: %v", err)
	}

	table := []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "note", Shape: fields.String, Create: true, Update: true, Response: true},
		{Name: "tiers", Shape: fields.ObjectMap, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "code", Shape: fields.String, Create: true, Response: true},
		}},
	}
	mapDecl := Declaration{TypeName: "stripe_widget", Paths: Paths{Create: "/v1/w", Retrieve: "/v1/w/{id}", Update: "/v1/w/{id}", Delete: "/v1/w/{id}"}, Delete: Delete, RefuseChange: []string{"note"}}
	if _, err = NewResource(mapDecl, table); err == nil || !strings.Contains(err.Error(), "tiers.code") {
		t.Errorf("a create-only field inside a map must be refused: %v", err)
	}
	deep := append(append([]fields.Field(nil), table[:2]...), fields.Field{Name: "outer", Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
		{Name: "inner", Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "code", Shape: fields.String, Create: true, Response: true},
		}},
	}})
	if _, err = NewResource(mapDecl, deep); err == nil || !strings.Contains(err.Error(), "outer.inner.code") {
		t.Errorf("a create-only field two objects down must be refused, naming its path: %v", err)
	}
	mapDecl.RefuseChange = nil
	if _, err = NewResource(mapDecl, table); err != nil {
		t.Errorf("without RefuseChange the same table compiles: %v", err)
	}
}
