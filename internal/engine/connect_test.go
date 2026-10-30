package engine

import (
	"context"
	"maps"
	"net/http"
	"slices"
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

// A token takes the hash's place: the create sends it beside the outside fields, with no hash.
func TestCreateWrapSendsATokenInPlaceOfTheHash(t *testing.T) {
	decl := wrapDeclaration()
	decl.CreateWrap.Token, decl.CreateWrap.Requires = "passcode", []string{"kind"}
	m, err := compile(decl, fixtureTable())
	if err != nil {
		t.Fatal(err)
	}
	body := wire.Object{{Name: "passcode", Value: wire.String("tok_1")}, {Name: "metadata", Value: wire.Object{}}}
	if got := m.wrapCreate(body); !slices.EqualFunc(got, body, func(a, b wire.Field) bool { return a.Name == b.Name }) {
		t.Errorf("a create from a token must be sent as it is: %s", wireOf(t, got))
	}
	hash := m.wrapCreate(wire.Object{{Name: "name", Value: wire.String("n")}})
	if _, wrapped := hash.Get("bank"); !wrapped {
		t.Errorf("a create without the token still sends the hash: %s", wireOf(t, hash))
	}
}

// tokenResource is a wrapped resource whose token is `passcode`, with `quantity` the field the
// hash needs and `kind`, which no update takes, beside `name`, which one does.
func tokenResource(t *testing.T, stub *stubStripe) *Resource {
	t.Helper()
	decl := wrapDeclaration()
	decl.CreateWrap.Outside = []string{"metadata", "name"}
	decl.CreateWrap.Token, decl.CreateWrap.Requires = "passcode", []string{"quantity"}
	return configured(t, decl, fixtureTable(), stub)
}

func validated(t *testing.T, r *Resource, config basetypes.ObjectValue) diag.Diagnostics {
	t.Helper()
	var resp resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)}}, &resp)
	return resp.Diagnostics
}

// A token from another resource is unknown at validation and at plan, so nothing there can judge
// it. Whatever it resolves to, the create refuses the two forms together, and neither, before
// any request and without a value in the error.
func TestCreateWrapChecksATokenUnknownAtPlanWhenItResolves(t *testing.T) {
	stub := newStub()
	r := tokenResource(t, stub)
	config := r.fill(map[string]attr.Value{"name": types.StringValue("w"), "passcode": types.StringUnknown(), "kind": types.StringValue("a"), "quantity": types.Int64Value(7)})
	if d := validated(t, r, config); d.HasError() {
		t.Fatalf("an unknown token is left for apply: %v", d)
	}
	var plan resource.ModifyPlanResponse
	r.planCreateWrap(context.Background(), tfsdk.Config{Schema: r.schema, Raw: raw(t, config)}, &plan.Diagnostics)
	if plan.Diagnostics.HasError() {
		t.Fatalf("an unknown token is left for apply: %v", plan.Diagnostics)
	}

	_, err := r.m.encodeCreate(r.fill(with(config, map[string]attr.Value{"passcode": types.StringValue("tok_secret_1")})))
	if err == nil || !strings.Contains(err.Error(), "`kind`, `quantity` cannot be sent beside `passcode`") {
		t.Fatalf("both forms at apply: %v", err)
	}
	if strings.Contains(err.Error(), "tok_secret_1") || strings.Contains(err.Error(), `"a"`) {
		t.Errorf("the error must name attributes, never values: %v", err)
	}

	_, err = r.m.encodeCreate(r.fill(with(config, map[string]attr.Value{"passcode": types.StringNull(), "quantity": types.Int64Null()})))
	if err == nil || !strings.Contains(err.Error(), "`quantity` required unless `passcode` is set") {
		t.Fatalf("a token resolved to null needs the hash: %v", err)
	}

	if _, err = r.m.encodeCreate(r.fill(with(config, map[string]attr.Value{"passcode": types.StringValue("tok_1"), "kind": types.StringNull(), "quantity": types.Int64Null()}))); err != nil {
		t.Errorf("the token alone, beside the outside fields: %v", err)
	}
	if _, err = r.m.encodeCreate(r.fill(with(config, map[string]attr.Value{"passcode": types.StringNull()}))); err != nil {
		t.Errorf("the hash alone: %v", err)
	}
	if got := stub.requests(); len(got) != 0 {
		t.Errorf("encoding sends nothing: %d requests", len(got))
	}
}

// Validation sees no state, so beside a token it refuses only what could never be sent: a
// create-only field. A create refuses every field of the hash and needs the hash without a
// token; an object that exists takes an updatable field beside its token.
func TestCreateWrapJudgesFieldsBesideATokenByWhatTheyCanDo(t *testing.T) {
	r := tokenResource(t, newStub())
	token := map[string]attr.Value{"name": types.StringValue("w"), "passcode": types.StringValue("tok_1")}
	beside := func(field string, v attr.Value) basetypes.ObjectValue {
		config := map[string]attr.Value{field: v}
		maps.Copy(config, token)
		return r.fill(config)
	}
	onCreate := func(config basetypes.ObjectValue) diag.Diagnostics {
		var d diag.Diagnostics
		r.planCreateWrap(context.Background(), tfsdk.Config{Schema: r.schema, Raw: raw(t, config)}, &d)
		return d
	}
	createOnly, updatable := beside("kind", types.StringValue("a")), beside("quantity", types.Int64Value(7))
	if d := validated(t, r, createOnly); !d.HasError() || !strings.Contains(d[0].Detail(), "`kind` cannot be set beside `passcode`") {
		t.Errorf("a create-only field beside the token: %v", d)
	}
	if d := validated(t, r, updatable); d.HasError() {
		t.Errorf("an updatable field beside the token passes validation: %v", d)
	}
	if d := onCreate(updatable); !d.HasError() || !strings.Contains(d[0].Detail(), "`quantity` cannot be set beside `passcode` on a create") {
		t.Errorf("an updatable field beside the token on a create: %v", d)
	}
	if d := onCreate(r.fill(map[string]attr.Value{"name": types.StringValue("w")})); !d.HasError() || !strings.Contains(d[0].Detail(), "`quantity` is required unless `passcode` is set") {
		t.Errorf("neither form on a create: %v", d)
	}
	if d := validated(t, r, r.fill(map[string]attr.Value{"name": types.StringValue("w")})); d.HasError() {
		t.Errorf("an object that exists needs neither form: %v", d)
	}
}

func TestCreateWrapMustNameCreateFields(t *testing.T) {
	for name, wrap := range map[string]*CreateWrap{
		"no key":          {Outside: []string{"metadata"}},
		"unknown outside": {Key: "bank", Outside: []string{"nothing"}},
		"not a create":    {Key: "bank", Outside: []string{"created"}},
		"unknown token":   {Key: "bank", Token: "nothing"},
		"required token":  {Key: "bank", Token: "name"},
		"unknown needed":  {Key: "bank", Token: "passcode", Requires: []string{"nothing"}},
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
	for name, field := range map[string]string{"unknown": "nothing", "output only": "created", "in a list": "items.sku", "in a map": "options.amount"} {
		decl := fixtureDeclaration()
		decl.RefuseChange = []string{field}
		if _, err := NewResource(decl, refuseTable()); err == nil {
			t.Errorf("%s: must not compile", name)
		}
	}
}

// A field below the top level, such as an account's `business_profile.mcc`, is refused on its
// own: a change beside it in the same object passes, and removing the object that holds it is
// a change of it.
func TestRefuseChangeTakesANestedField(t *testing.T) {
	decl := fixtureDeclaration()
	decl.RefuseChange = []string{"address.zip"}
	r := configured(t, decl, refuseTable(), noCalls(t))
	addressType := r.objType.AttrTypes["address"].(basetypes.ObjectType).AttrTypes
	address := func(line1, zip string) attr.Value {
		return types.ObjectValueMust(addressType, map[string]attr.Value{"line1": types.StringValue(line1), "zip": types.StringValue(zip)})
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "address": address("1 Main St", "94080")})
	modify := func(planned attr.Value) *resource.ModifyPlanResponse {
		return r.modifyPlan(t, r.fill(with(state, map[string]attr.Value{"address": planned})), state)
	}
	if resp := modify(address("1 Main St", "10001")); !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Summary(), "`address.zip`") ||
		!strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "ignore_changes = [address.zip]") {
		t.Errorf("a changed nested value must fail the plan and name its path: %v", resp.Diagnostics)
	}
	if resp := modify(address("2 Main St", "94080")); resp.Diagnostics.HasError() {
		t.Errorf("a change beside the refused field must pass: %v", resp.Diagnostics)
	}
	if resp := modify(types.ObjectNull(addressType)); !resp.Diagnostics.HasError() {
		t.Error("removing the object that holds the refused field is a change of it")
	}
	if resp := modify(types.ObjectUnknown(addressType)); resp.Diagnostics.HasError() {
		t.Errorf("an object not known yet is left for apply: %v", resp.Diagnostics)
	}
}

// A refused field is sent on create only: an update that changes a field beside it in the same
// object, such as an account's `business_profile.name` beside its `mcc`, sends the changed
// field alone, and neither a nested nor a top-level refused field is ever in the body.
func TestRefuseChangeKeepsRefusedFieldsOutOfTheUpdate(t *testing.T) {
	decl := fixtureDeclaration()
	decl.RefuseChange = []string{"address.zip", "url"}
	r := configured(t, decl, refuseTable(), noCalls(t))
	addressType := r.objType.AttrTypes["address"].(basetypes.ObjectType).AttrTypes
	address := func(line1, zip string) attr.Value {
		return types.ObjectValueMust(addressType, map[string]attr.Value{"line1": types.StringValue(line1), "zip": types.StringValue(zip)})
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "url": types.StringValue("https://a.example"), "address": address("1 Main St", "94080")})
	for name, tc := range map[string]struct{ zip, url string }{
		"unchanged": {"94080", "https://a.example"},
		"changed":   {"10001", "https://b.example"},
	} {
		plan := r.fill(with(state, map[string]attr.Value{"url": types.StringValue(tc.url), "address": address("2 Main St", tc.zip)}))
		params, err := r.m.encodeUpdate(plan, state, tftypes.Value{})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := wireOf(t, params), `{"address":{"line1":"2 Main St"}}`; got != want {
			t.Errorf("%s refused fields: update body %s, want %s", name, got, want)
		}
	}
}

// An object in SendChangedMembers sends only what changed: a removed member as null, a nested
// object's changed members, one with a Required member whole, a removed map key as null,
// never a member the plan leaves unknown or one that is refused, and nothing when only those
// differ.
func TestSendChangedMembers(t *testing.T) {
	member := func(name string, shape fields.Shape) fields.Field {
		return fields.Field{Name: name, Shape: shape, Create: true, Update: true, Response: true}
	}
	dob := member("dob", fields.Object)
	day, month := member("day", fields.Int), member("month", fields.Int)
	day.Required, month.Required = true, true
	dob.Fields = []fields.Field{day, month}
	home := member("home", fields.Object)
	home.Fields = []fields.Field{member("city", fields.String), member("line1", fields.String)}
	tags := member("tags", fields.StringMap)
	person := member("person", fields.Object)
	person.Fields = []fields.Field{member("first", fields.String), member("last", fields.String), member("secret", fields.String), member("filled", fields.Bool), dob, home, tags}
	decl := fixtureDeclaration()
	decl.RefuseChange = []string{"person.secret"}
	decl.SendChangedMembers = []string{"person"}
	r := configured(t, decl, append(refuseTable(), person), noCalls(t))
	personType := r.objType.AttrTypes["person"].(basetypes.ObjectType)
	dobType := personType.AttrTypes["dob"].(basetypes.ObjectType).AttrTypes
	homeType := personType.AttrTypes["home"].(basetypes.ObjectType).AttrTypes
	build := func(over map[string]attr.Value) attr.Value {
		attrs := map[string]attr.Value{
			"first":  types.StringValue("Jenny"),
			"last":   types.StringValue("Rosen"),
			"secret": types.StringValue("s3"),
			"filled": types.BoolValue(true),
			"dob":    types.ObjectValueMust(dobType, map[string]attr.Value{"day": types.Int64Value(1), "month": types.Int64Value(2)}),
			"home":   types.ObjectValueMust(homeType, map[string]attr.Value{"city": types.StringValue("A"), "line1": types.StringValue("1 Main")}),
			"tags":   types.MapValueMust(types.StringType, map[string]attr.Value{"a": types.StringValue("1"), "b": types.StringValue("2")}),
		}
		maps.Copy(attrs, over)
		return types.ObjectValueMust(personType.AttrTypes, attrs)
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "person": build(nil)})
	for name, tc := range map[string]struct {
		change map[string]attr.Value
		want   string
	}{
		"a scalar":          {map[string]attr.Value{"first": types.StringValue("Jane")}, `{"person":{"first":"Jane"}}`},
		"a removed member":  {map[string]attr.Value{"last": types.StringNull()}, `{"person":{"last":null}}`},
		"a nested member":   {map[string]attr.Value{"home": types.ObjectValueMust(homeType, map[string]attr.Value{"city": types.StringValue("B"), "line1": types.StringValue("1 Main")})}, `{"person":{"home":{"city":"B"}}}`},
		"a required member": {map[string]attr.Value{"dob": types.ObjectValueMust(dobType, map[string]attr.Value{"day": types.Int64Value(3), "month": types.Int64Value(2)})}, `{"person":{"dob":{"day":3,"month":2}}}`},
		"a map key":         {map[string]attr.Value{"tags": types.MapValueMust(types.StringType, map[string]attr.Value{"a": types.StringValue("1")})}, `{"person":{"tags":{"b":null}}}`},
		"a refused member":  {map[string]attr.Value{"secret": types.StringValue("other")}, `{}`},
		"an unknown member": {map[string]attr.Value{"filled": types.BoolUnknown()}, `{}`},
	} {
		plan := r.fill(with(state, map[string]attr.Value{"person": build(tc.change)}))
		params, err := r.m.encodeUpdate(plan, state, tftypes.Value{})
		if err != nil {
			t.Fatal(err)
		}
		if got := wireOf(t, params); got != tc.want {
			t.Errorf("%s: update body %s, want %s", name, got, tc.want)
		}
	}
	decl.SendChangedMembers = []string{"name"}
	if _, err := compile(decl, append(refuseTable(), person)); err == nil {
		t.Error("an entry that is no object must be refused")
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

// A refused DELETE of an object that the retrieve right after it finds gone, such as an
// invoice item Stripe deleted along with its draft invoice, succeeds. One the retrieve still
// finds, or cannot read, fails with Stripe's error.
func TestDeleteOfAnObjectAlreadyGoneSucceeds(t *testing.T) {
	refusal := &stripeapi.APIError{Status: 400, Type: "invalid_request_error", Message: "Can't delete an invoice item that is attached to an invoice that is no longer editable"}
	for name, c := range map[string]struct {
		retrieve func(stub *stubStripe, ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error)
		gone     bool
		warnings int
	}{
		"not found": {func(*stubStripe, context.Context, stripeapi.Request) (*stripeapi.Response, error) {
			return nil, notFound()
		}, true, 0},
		"deleted stub": {func(*stubStripe, context.Context, stripeapi.Request) (*stripeapi.Response, error) {
			return &stripeapi.Response{Status: 200, Body: wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "deleted", Value: wire.Bool(true)}}}, nil
		}, true, 0},
		"gone on forbidden": {func(*stubStripe, context.Context, stripeapi.Request) (*stripeapi.Response, error) {
			return nil, accountInvalid()
		}, true, 1},
		"still there": {func(stub *stubStripe, ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
			return stub.Do(ctx, req)
		}, false, 0},
		"retrieve fails": {func(*stubStripe, context.Context, stripeapi.Request) (*stripeapi.Response, error) {
			return nil, &stripeapi.APIError{Status: 500, Type: "api_error", Message: "down"}
		}, false, 0},
	} {
		stub := newStub()
		created := false
		r := configured(t, goneDeclaration(), fixtureTable(), clientFunc(func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
			switch {
			case req.Method == "DELETE":
				return nil, refusal
			case req.Method == "GET" && created:
				return c.retrieve(stub, ctx, req)
			}
			return stub.Do(ctx, req)
		}))
		state, _ := r.create(t, createPlan(r))
		created = true
		diags := r.destroy(t, state)
		if c.gone && diags.HasError() {
			t.Errorf("%s: the destroy must succeed: %v", name, diags)
		}
		if !c.gone && (len(diags.Errors()) != 1 || !strings.Contains(diags.Errors()[0].Detail(), "no longer editable")) {
			t.Errorf("%s: the destroy must fail with Stripe's error: %v", name, diags)
		}
		if len(diags.Warnings()) != c.warnings {
			t.Errorf("%s: want %d warnings: %v", name, c.warnings, diags)
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
