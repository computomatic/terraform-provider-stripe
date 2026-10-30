package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// retrieveOnlyDeclaration is a widget whose `provider` Stripe expands only on a retrieve, as
// an Issuing card's `number`, besides an `options` it expands anywhere.
func retrieveOnlyDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.Lookups = nil
	decl.Expand = []string{"options"}
	decl.ExpandOnRetrieve = []string{"provider"}
	return decl
}

// A field Stripe expands only on a retrieve is never asked for on a create or an update, which
// Stripe refuses, but on every retrieve, and create and update retrieve the object at the end
// so state holds it.
func TestExpandOnRetrieveIsAskedForOnlyOnRetrieves(t *testing.T) {
	stub := newStub()
	client := clientFunc(func(ctx context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
		resp, err := stub.Do(ctx, req)
		if err != nil || req.Method != "GET" || !hasExpand(t, req, `["options","provider"]`) {
			return resp, err
		}
		body, _ := resp.Body.(wire.Object)
		return &stripeapi.Response{Status: resp.Status, Body: put(append(wire.Object(nil), body...), "provider", wire.String("shown"))}, nil
	})
	r := configured(t, retrieveOnlyDeclaration(), fixtureTable(), client)

	state, _ := r.create(t, createPlan(r))
	if got := str(t, state, "provider_"); got != "shown" {
		t.Errorf("provider after create: %q", got)
	}
	attrs := with(state, map[string]attr.Value{"name": types.StringValue("renamed"), "provider_": types.StringUnknown()})
	updated := r.update(t, r.fill(attrs), state)
	if got := str(t, updated, "provider_"); got != "shown" {
		t.Errorf("provider after update: %q", got)
	}
	// A plan with nothing to send reads instead, and that read is a retrieve.
	r.update(t, updated, updated)
	r.read(t, updated)
	noErrors(t, r.destroy(t, updated))

	var calls []string
	for _, req := range stub.requests() {
		calls = append(calls, req.Method)
		switch req.Method {
		case "POST":
			if !hasExpand(t, req, `["options"]`) {
				t.Errorf("POST %s must expand only options: %+v", req.Path, req.Params)
			}
		case "GET":
			if !hasExpand(t, req, `["options","provider"]`) {
				t.Errorf("GET %s must expand both: %+v", req.Path, req.Params)
			}
		}
	}
	// Create (with its follow-up update) and each update end with a retrieve.
	if got := strings.Join(calls, ","); got != "POST,POST,GET,POST,GET,GET,GET,GET,DELETE" {
		t.Errorf("calls: %s", got)
	}
}

// A data source reads its object by ID through a retrieve, so it asks for both.
func TestExpandOnRetrieveIsAskedForByTheDataSource(t *testing.T) {
	stub := newStub()
	r := configured(t, retrieveOnlyDeclaration(), fixtureTable(), stub)
	state, _ := r.create(t, createPlan(r))
	stub.reset()
	ds := dataSource(t, retrieveOnlyDeclaration(), stub)
	_, resp := ds.read(t, ds.config(map[string]string{"id": str(t, state, "id")}))
	noErrors(t, resp.Diagnostics)
	reqs := stub.requests()
	if len(reqs) != 1 || !hasExpand(t, reqs[0], `["options","provider"]`) {
		t.Errorf("the data source must retrieve with both expanded: %+v", reqs)
	}
}

func TestExpandOnRetrieveMustNameAFieldReadByID(t *testing.T) {
	for name, change := range map[string]func(*Declaration){
		"no field":       func(d *Declaration) { d.ExpandOnRetrieve = []string{"nothing"} },
		"request only":   func(d *Declaration) { d.ExpandOnRetrieve = []string{"passcode"} },
		"also in expand": func(d *Declaration) { d.Expand = []string{"options", "provider"} },
		"with a lookup":  func(d *Declaration) { d.Lookups = []Lookup{{Field: "kind"}} },
	} {
		decl := retrieveOnlyDeclaration()
		change(&decl)
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("%s: the declaration must fail", name)
		}
	}
	if _, err := NewResource(retrieveOnlyDeclaration(), fixtureTable()); err != nil {
		t.Errorf("the control must compile: %v", err)
	}
}

// A field the response leaves out or returns null keeps its prior value, at any depth, a whole
// object too; a field not listed reads what the response says.
func TestKeepOmittedKeepsAFieldTheResponseLeavesOut(t *testing.T) {
	decl := fixtureDeclaration()
	decl.KeepOmitted = []string{"kind", "address", "address.zip"}
	var body wire.Object
	client := clientFunc(func(context.Context, stripeapi.Request) (*stripeapi.Response, error) {
		return &stripeapi.Response{Body: body}, nil
	})
	r := configured(t, decl, fixtureTable(), client)
	addressType := r.objType.AttrTypes["address"]
	state := r.fill(map[string]attr.Value{
		"id": types.StringValue("wid_1"), "name": types.StringValue("n"), "kind": types.StringValue("a"), "quantity": types.Int64Value(3),
		"address": object(addressType, map[string]attr.Value{"line1": types.StringValue("1 Main"), "zip": types.StringValue("10001")}),
	})
	addressOf := func(v attr.Value) string {
		a := v.(types.Object).Attributes()
		return a["line1"].(types.String).ValueString() + "|" + a["zip"].String()
	}
	read := func(fields ...wire.Field) basetypes.ObjectValue {
		body = append(wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("n")}}, fields...)
		got, _ := r.read(t, state)
		return got
	}

	got := read(wire.Field{Name: "address", Value: wire.Object{{Name: "line1", Value: wire.String("2 Main")}}})
	if str(t, got, "kind") != "a" || addressOf(got.Attributes()["address"]) != `2 Main|"10001"` {
		t.Errorf("omitted fields must keep the state's values: %v", got)
	}
	if !got.Attributes()["quantity"].IsNull() {
		t.Errorf("the control: a field not listed reads the omission as null: %v", got.Attributes()["quantity"])
	}
	got = read(wire.Field{Name: "kind", Value: wire.Null{}}, wire.Field{Name: "address", Value: wire.Object{{Name: "line1", Value: wire.String("2 Main")}, {Name: "zip", Value: wire.Null{}}}})
	if str(t, got, "kind") != "a" || addressOf(got.Attributes()["address"]) != `2 Main|"10001"` {
		t.Errorf("null fields must keep the state's values: %v", got)
	}
	got = read(wire.Field{Name: "address", Value: wire.Null{}})
	if addressOf(got.Attributes()["address"]) != `1 Main|"10001"` {
		t.Errorf("a null object must keep the state's: %v", got)
	}
}

// An apply whose plan holds a kept object with a member not known yet, such as an update that
// closes an account and edits its identity together, keeps nothing from the plan when the
// response returns the object null: the state never holds an unknown value.
func TestKeepOmittedKeepsOnlyAFullyKnownValue(t *testing.T) {
	decl := fixtureDeclaration()
	decl.KeepOmitted = []string{"address"}
	r := configured(t, decl, fixtureTable(), newStub())
	addressType := r.objType.AttrTypes["address"]
	plan := r.fill(map[string]attr.Value{
		"id": types.StringValue("wid_1"), "name": types.StringValue("n"),
		"address": object(addressType, map[string]attr.Value{"line1": types.StringValue("1 Main"), "zip": types.StringUnknown()}),
	})
	body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("n")}, {Name: "address", Value: wire.Null{}}}
	got, diags := r.m.decodeState(body, plan, r.objType, false, nil)
	noErrors(t, diags)
	if !got.Attributes()["address"].IsNull() {
		t.Errorf("a partly unknown prior must not be kept: %v", got.Attributes()["address"])
	}

	known := r.fill(with(plan, map[string]attr.Value{"address": object(addressType, map[string]attr.Value{"line1": types.StringValue("1 Main"), "zip": types.StringValue("10001")})}))
	got, diags = r.m.decodeState(body, known, r.objType, false, nil)
	noErrors(t, diags)
	if !got.Attributes()["address"].Equal(known.Attributes()["address"]) {
		t.Errorf("the control: a fully known prior is kept: %v", got.Attributes()["address"])
	}
}

// A preserved field outlives a response that leaves its object out, such as a v2 recipient's
// `requested` flag once Stripe drops the capability it belongs to, and the rest of the object
// reads as Stripe left it. An object with no such value reads as null.
func TestPreserveConfigOutlivesAnObjectTheResponseLeavesOut(t *testing.T) {
	decl := fixtureDeclaration()
	decl.PreserveConfig = []string{"address.line1"}
	r := configured(t, decl, fixtureTable(), newStub())
	addressType := r.objType.AttrTypes["address"]
	plan := r.fill(map[string]attr.Value{
		"id": types.StringValue("wid_1"), "name": types.StringValue("n"),
		"address": object(addressType, map[string]attr.Value{"line1": types.StringValue("1 Main"), "zip": types.StringUnknown()}),
	})
	body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("n")}}
	got, diags := r.m.decodeState(body, plan, r.objType, false, nil)
	noErrors(t, diags)
	want := object(addressType, map[string]attr.Value{"line1": types.StringValue("1 Main"), "zip": types.StringNull()})
	if !got.Attributes()["address"].Equal(want) {
		t.Errorf("address = %v, want the preserved line and nothing else", got.Attributes()["address"])
	}

	empty := r.fill(with(plan, map[string]attr.Value{"address": types.ObjectNull(addressType.(types.ObjectType).AttrTypes)}))
	got, diags = r.m.decodeState(body, empty, r.objType, false, nil)
	noErrors(t, diags)
	if !got.Attributes()["address"].IsNull() {
		t.Errorf("no preserved value, so the address is null: %v", got.Attributes()["address"])
	}
}

func TestKeepOmittedMustNameAResponseFieldOutsideAList(t *testing.T) {
	for _, name := range []string{"nothing", "passcode", "address.nothing", "items.qty"} {
		decl := fixtureDeclaration()
		decl.KeepOmitted = []string{name}
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("keep-omitted %q must fail", name)
		}
	}
	decl := fixtureDeclaration()
	decl.KeepOmitted = []string{"address.zip"}
	if _, err := NewResource(decl, fixtureTable()); err != nil {
		t.Errorf("the control: a nested field outside a list must compile: %v", err)
	}
}

// A lower-case field refuses a configured value with capitals, in the resource and in a data
// source lookup, and says so in its description.
func TestLowerCaseRefusesCapitals(t *testing.T) {
	decl := fixtureDeclaration()
	decl.LowerCase = []string{"kind"}
	r := configured(t, decl, fixtureTable(), newStub())
	validate := func(kind attr.Value) []string {
		var resp resource.ValidateConfigResponse
		config := r.fill(map[string]attr.Value{"id": types.StringNull(), "name": types.StringValue("n"), "kind": kind})
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)}}, &resp)
		var details []string
		for _, d := range resp.Diagnostics.Errors() {
			details = append(details, d.Detail())
		}
		return details
	}
	if got := validate(types.StringValue("MyList")); len(got) != 1 || !strings.Contains(got[0], `Write it as "mylist"`) {
		t.Errorf("capitals must be refused: %v", got)
	}
	for _, ok := range []attr.Value{types.StringValue("mylist_1"), types.StringUnknown(), types.StringNull()} {
		if got := validate(ok); len(got) != 0 {
			t.Errorf("%v must be accepted: %v", ok, got)
		}
	}
	if d := r.schema.Attributes["kind"].GetMarkdownDescription(); !strings.Contains(d, "must be written in lower case") {
		t.Errorf("resource description: %s", d)
	}

	ds := dataSource(t, decl, newStub())
	dsValidate := func(kind string) bool {
		var resp datasource.ValidateConfigResponse
		ds.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{Config: tfsdk.Config{Schema: ds.schema, Raw: raw(t, ds.config(map[string]string{"kind": kind}))}}, &resp)
		return !resp.Diagnostics.HasError()
	}
	if dsValidate("MyList") || !dsValidate("mylist") {
		t.Error("the data source must refuse capitals in a lookup and accept lower case")
	}
	if d := ds.schema.Attributes["kind"].GetMarkdownDescription(); !strings.Contains(d, "must be written in lower case") {
		t.Errorf("data source description: %s", d)
	}
}

func TestLowerCaseMustNameAConfigurableString(t *testing.T) {
	for _, name := range []string{"nothing", "quantity", "provider"} {
		decl := fixtureDeclaration()
		decl.LowerCase = []string{name}
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("lower-case %q must fail", name)
		}
	}
}

// fixedDecl is a widget whose `label` Stripe changes only from `unspecified`, as a price's
// `tax_behavior`.
func fixedDecl() Declaration {
	d := planDecl()
	d.FixedOnceSet = map[string]string{"label": "unspecified"}
	return d
}

// A configured change of a fixed-once-set field replaces the object only from a value other
// than the one Stripe changes it from; a field left unset never does.
func TestFixedOnceSetReplacesAChangeOfAFixedValue(t *testing.T) {
	p := newPlanner(t, fixedDecl(), planTable())
	for name, tc := range map[string]struct {
		from, to tftypes.Value
		replace  bool
	}{
		"fixed to another":  {tfString("exclusive"), tfString("inclusive"), true},
		"free to a value":   {tfString("unspecified"), tfString("exclusive"), false},
		"fixed to the same": {tfString("exclusive"), tfString("exclusive"), false},
		"fixed to unset":    {tfString("exclusive"), tftypes.NewValue(tftypes.String, nil), false},
		"fixed to unknown":  {tfString("exclusive"), tfUnknown(), false},
	} {
		t.Run(name, func(t *testing.T) {
			state := clone(p.base(), map[string]tftypes.Value{"label": tc.from})
			proposed := clone(state, map[string]tftypes.Value{"label": tc.to})
			if tc.to.IsNull() {
				proposed["label"] = tc.from
			}
			config := clone(proposed, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, nil), "updated": tftypes.NewValue(tftypes.String, nil), "label": tc.to})
			_, replace := p.plan(p.value(state), p.value(proposed), p.value(config))
			if got := len(replace) > 0; got != tc.replace {
				t.Errorf("replace: got %v (%v), want %v", got, replace, tc.replace)
			}
		})
	}
}

// On a resource whose destroy is refused, such a change fails the plan, as any replacement
// does, and a change from the free value plans as an update.
func TestFixedOnceSetIsRefusedWhereDestroyIs(t *testing.T) {
	decl := refusingDeclaration()
	decl.FixedOnceSet = map[string]string{"url": "unspecified"}
	p := newPlanner(t, decl, lifecycleTable())
	state := p.lifecycleState("open", map[string]tftypes.Value{"url": tfString("fixed")})
	resp := p.change(state, map[string]tftypes.Value{"url": tfString("other")})
	if errs := errorsOf(resp); len(errs) != 1 || errs[0].Summary != "Cannot replace stripe_widget" || len(resp.RequiresReplace) != 0 {
		t.Fatalf("want the refusal: %+v %v", resp.Diagnostics, resp.RequiresReplace)
	}
	state["url"] = tfString("unspecified")
	if resp := p.change(state, map[string]tftypes.Value{"url": tfString("other")}); len(errorsOf(resp)) != 0 || len(resp.RequiresReplace) != 0 {
		t.Fatalf("a change from the free value is an update: %+v %v", resp.Diagnostics, resp.RequiresReplace)
	}
}

// A plan that changes a fixed value is a replace, so a refused change in the same plan is not
// refused: the new object is created with it.
func TestFixedOnceSetCountsAsAReplaceForRefuseChange(t *testing.T) {
	decl := fixedDecl()
	decl.RefuseChange = []string{"name"}
	p := newPlanner(t, decl, planTable())
	state := clone(p.base(), map[string]tftypes.Value{"label": tfString("exclusive")})
	changes := map[string]tftypes.Value{"name": tfString("renamed"), "label": tfString("inclusive")}
	proposed := clone(state, changes)
	config := clone(proposed, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, nil), "updated": tftypes.NewValue(tftypes.String, nil)})
	if errs := errorsOf(p.planResponse(p.value(state), p.value(proposed), p.value(config))); len(errs) != 0 {
		t.Errorf("a replace must not be refused: %+v", errs)
	}
	state["label"] = tfString("unspecified")
	proposed = clone(state, changes)
	config = clone(proposed, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, nil), "updated": tftypes.NewValue(tftypes.String, nil)})
	if errs := errorsOf(p.planResponse(p.value(state), p.value(proposed), p.value(config))); len(errs) != 1 {
		t.Errorf("the control: an update that changes name must be refused: %+v", errs)
	}
}

func TestFixedOnceSetDescribesTheReplacement(t *testing.T) {
	r := configured(t, fixedDecl(), planTable(), newStub())
	if d := r.schema.Attributes["label"].GetMarkdownDescription(); !strings.Contains(d, "Stripe changes it only from `unspecified`, so changing it from any other value replaces the resource.") {
		t.Errorf("description: %s", d)
	}
}

func TestFixedOnceSetMustNameAnUpdatableString(t *testing.T) {
	for name, fixed := range map[string]map[string]string{
		"no field":    {"nothing": "x"},
		"create-only": {"kind": "a"},
		"not string":  {"quantity": "1"},
		"no value":    {"url": ""},
	} {
		decl := fixtureDeclaration()
		decl.FixedOnceSet = fixed
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("%s: the declaration must fail", name)
		}
	}
}

// Where Stripe keeps a metadata key sent empty, a plan that drops a key fails and names it; a
// key set to "" plans, as does a map not known yet or a plan that replaces the object.
func TestMetadataKeysStayRefusesDroppingAKey(t *testing.T) {
	decl := fixtureDeclaration()
	decl.MetadataKeysStay = true
	r := configured(t, decl, fixtureTable(), newStub())
	state := r.fill(map[string]attr.Value{
		"id": types.StringValue("wid_1"), "name": types.StringValue("n"), "kind": types.StringValue("a"),
		"metadata": stringMapValue(map[string]string{"team": "a", "tier": "gold"}),
	})
	plan := func(set map[string]attr.Value) []string {
		resp := r.modifyPlan(t, r.fill(with(state, set)), state)
		var details []string
		for _, d := range resp.Diagnostics.Errors() {
			details = append(details, d.Detail())
		}
		return details
	}
	if got := plan(map[string]attr.Value{"metadata": stringMapValue(map[string]string{"team": "b"})}); len(got) != 1 || !strings.Contains(got[0], "cannot remove `tier`") || !strings.Contains(got[0], `Set each key to "" to clear its value instead.`) {
		t.Errorf("dropping a key must be refused: %v", got)
	}
	if got := plan(map[string]attr.Value{"metadata": types.MapNull(types.StringType)}); len(got) != 1 || !strings.Contains(got[0], "`team`, `tier`") {
		t.Errorf("dropping the map must be refused: %v", got)
	}
	for name, set := range map[string]map[string]attr.Value{
		"empty value": {"metadata": stringMapValue(map[string]string{"team": "b", "tier": ""})},
		"unknown":     {"metadata": types.MapUnknown(types.StringType)},
		"replace":     {"kind": types.StringValue("b"), "metadata": stringMapValue(map[string]string{"team": "b"})},
	} {
		if got := plan(set); len(got) != 0 {
			t.Errorf("%s must plan: %v", name, got)
		}
	}

	r = configured(t, fixtureDeclaration(), fixtureTable(), newStub())
	if resp := r.modifyPlan(t, r.fill(with(state, map[string]attr.Value{"metadata": stringMapValue(map[string]string{"team": "b"})})), state); resp.Diagnostics.HasError() {
		t.Errorf("the control: elsewhere a key is dropped by an update: %v", resp.Diagnostics)
	}
}

func TestMetadataKeysStayNeedsMetadata(t *testing.T) {
	decl := planDecl()
	decl.MetadataKeysStay = true
	if _, err := NewResource(decl, planTable()); err == nil {
		t.Error("a table without metadata must fail")
	}
}

// flagTable is a subscription's shape: a flag Stripe defaults, and details it fills in when the
// flag changes, which only an update takes.
func flagTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Required: true, Create: true, Update: true, Response: true},
		{Name: "flag", Shape: fields.Bool, Create: true, Update: true, Response: true, ServerDefaulted: true},
		{Name: "details", Shape: fields.Object, Update: true, Response: true, Fields: []fields.Field{
			{Name: "comment", Shape: fields.String, Update: true, Response: true},
			{Name: "reason", Shape: fields.String, Response: true},
		}},
	}
}

// A computed source the configuration leaves unset is unknown in a plan only because the
// framework marks every unset computed value unknown on a change; that is no change of the
// source, so the filled fields keep their state and a plan of nothing stays the state. A
// configured change of the source forgets them.
func TestFilledFromIgnoresAnUnsetSourceTheFrameworkMarkedUnknown(t *testing.T) {
	decl := planDecl()
	decl.FilledFrom = map[string][]string{"flag": {"details"}}
	r := configured(t, decl, flagTable(), newStub())
	details := object(r.objType.AttrTypes["details"], map[string]attr.Value{"reason": types.StringNull()})
	state := r.fill(map[string]attr.Value{
		"id": types.StringValue("wid_1"), "name": types.StringValue("n"), "flag": types.BoolValue(false), "details": details,
		"timeouts": nullOf(r.objType.AttrTypes["timeouts"]),
	})
	unset := map[string]attr.Value{"id": types.StringNull(), "flag": types.BoolNull(), "details": nullOf(r.objType.AttrTypes["details"])}
	planned := func(plan, config map[string]attr.Value) basetypes.ObjectValue {
		resp := r.modifyPlan(t, r.fill(with(state, plan)), state, r.fill(with(state, config)))
		noErrors(t, resp.Diagnostics)
		var got basetypes.ObjectValue
		noErrors(t, resp.Plan.Get(context.Background(), &got))
		return got
	}

	if got := planned(map[string]attr.Value{"id": types.StringUnknown(), "flag": types.BoolUnknown()}, unset); !got.Equal(state) {
		t.Errorf("a plan of nothing must stay the state: %v", got)
	}
	renamed := map[string]attr.Value{"name": types.StringValue("m")}
	if got := planned(with(r.fill(renamed), map[string]attr.Value{"flag": types.BoolUnknown(), "details": details}), with(r.fill(renamed), unset)); !got.Attributes()["details"].Equal(details) {
		t.Errorf("another change must keep the details: %v", got.Attributes()["details"])
	}
	set := map[string]attr.Value{"flag": types.BoolValue(true)}
	if got := planned(with(r.fill(set), map[string]attr.Value{"details": details}), with(r.fill(set), map[string]attr.Value{"id": types.StringNull(), "details": nullOf(r.objType.AttrTypes["details"])})); !got.Attributes()["details"].IsUnknown() {
		t.Errorf("a configured change of the flag must forget the details: %v", got.Attributes()["details"])
	}
	later := map[string]attr.Value{"flag": types.BoolUnknown()}
	if got := planned(with(r.fill(later), map[string]attr.Value{"details": details}), with(r.fill(later), map[string]attr.Value{"id": types.StringNull(), "details": nullOf(r.objType.AttrTypes["details"])})); !got.Attributes()["details"].IsUnknown() {
		t.Errorf("a configured flag not known yet must forget the details: %v", got.Attributes()["details"])
	}
}
