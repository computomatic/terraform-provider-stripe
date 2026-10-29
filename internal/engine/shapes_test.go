package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// shapesTable holds the shapes of ADR 2026-10-29-full-attribute-coverage, D3, as the
// generator writes them: both forms of a token-or-hash parameter, a field whose response
// takes another shape, a value reported only through `<name>_provided`, and a hash with no
// members.
func shapesTable() []fields.Field {
	return []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "source", Shape: fields.String, Create: true, Update: true, Response: true},
		{Name: "source", Attr: "source_data", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
			{Name: "number", Shape: fields.String, Create: true, Update: true},
		}},
		{Name: "caps", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
			{Name: "a", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
				{Name: "requested", Shape: fields.Bool, Create: true, Update: true},
			}},
		}, DataSource: &fields.Field{Name: "caps", Shape: fields.Object, Response: true, Fields: []fields.Field{
			{Name: "a", Shape: fields.String, Response: true},
		}}},
		{Name: "tax_id", Shape: fields.String, Create: true, Update: true, Sensitive: true},
		{Name: "tax_id_provided", Shape: fields.Bool, Response: true},
		{Name: "options", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
			{Name: "affirm", Shape: fields.Object, Create: true, Update: true},
			{Name: "card", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
				{Name: "request_three_d_secure", Shape: fields.String, Create: true, Update: true},
			}},
		}},
		{Name: "items", Shape: fields.ObjectList, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "price", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "price", Attr: "price_data", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
				{Name: "unit_amount", Shape: fields.Int, Create: true, Update: true},
			}},
			{Name: "quantity", Shape: fields.Int, Create: true, Update: true, Response: true},
		}},
		{Name: "plan_info", Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "price", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "price", Attr: "price_data", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
				{Name: "unit_amount", Shape: fields.Int, Create: true, Update: true},
			}},
			{Name: "note", Shape: fields.String, Create: true, Update: true, Response: true},
		}},
		{Name: "settings", Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
			{Name: "mode", Shape: fields.String, Create: true, Update: true, Response: true},
			{Name: "branding", Shape: fields.Object, Create: true, Update: true, Response: true, ServerDefaulted: true, Fields: []fields.Field{
				{Name: "color", Shape: fields.String, Create: true, Update: true, Response: true},
			}},
		}},
	}
}

func shapesDeclaration() Declaration {
	decl := fixtureDeclaration()
	decl.TypeName, decl.OnceReturned, decl.Sensitive, decl.PreserveConfig, decl.Sets, decl.Rename, decl.Lookups = "stripe_shape", nil, nil, nil, nil, nil, nil
	return decl
}

func shapesResource(t *testing.T) *Resource {
	t.Helper()
	return configured(t, shapesDeclaration(), shapesTable(), newStub())
}

func TestShapesInTheResourceSchema(t *testing.T) {
	r := shapesResource(t)
	if a, ok := r.schema.Attributes["source"].(schema.StringAttribute); !ok || !a.Optional {
		t.Errorf("source: %#v", r.schema.Attributes["source"])
	}
	data, ok := r.schema.Attributes["source_data"].(schema.SingleNestedAttribute)
	if !ok || !data.Optional || data.Computed || len(data.Validators) == 0 {
		t.Fatalf("source_data: %#v", r.schema.Attributes["source_data"])
	}
	if _, isOneForm := data.Validators[0].(oneForm); !isOneForm {
		t.Errorf("source_data's first validator is %T, want oneForm", data.Validators[0])
	}
	if a := r.schema.Attributes["tax_id"]; !a.IsSensitive() {
		t.Error("a value reported only through `tax_id_provided` is sensitive")
	}
	caps := r.schema.Attributes["caps"].(schema.SingleNestedAttribute)
	if _, ok := caps.Attributes["a"].(schema.SingleNestedAttribute); !ok {
		t.Errorf("the resource takes the request's shape of caps.a: %#v", caps.Attributes["a"])
	}
	options := r.schema.Attributes["options"].(schema.SingleNestedAttribute)
	if affirm, ok := options.Attributes["affirm"].(schema.SingleNestedAttribute); !ok || len(affirm.Attributes) != 0 || !affirm.Optional {
		t.Errorf("a hash with no members is an Optional nested attribute with none: %#v", options.Attributes["affirm"])
	}
}

func TestShapesInTheDataSourceSchema(t *testing.T) {
	ctor, err := NewDataSource(shapesDeclaration(), shapesTable())
	if err != nil {
		t.Fatal(err)
	}
	ds := ctor().(*DataSource)
	if _, ok := ds.schema.Attributes["source_data"]; ok {
		t.Error("the data source has no hash form: only a request takes it")
	}
	caps := ds.schema.Attributes["caps"].(dschema.SingleNestedAttribute)
	if a, ok := caps.Attributes["a"].(dschema.StringAttribute); !ok || !a.Computed {
		t.Errorf("the data source reads the response's shape of caps.a: %#v", caps.Attributes["a"])
	}
	obj, diags := ds.m.decodeState(wire.Object{{Name: "id", Value: wire.String("shp_1")}, {Name: "caps", Value: wire.Object{{Name: "a", Value: wire.String("active")}}}}, basetypes.ObjectValue{}, ds.objType, true, nil)
	if diags.HasError() {
		t.Fatal(diags)
	}
	got := obj.Attributes()["caps"].(basetypes.ObjectValue).Attributes()["a"]
	if !got.Equal(types.StringValue("active")) {
		t.Errorf("caps.a = %s", got)
	}
}

// The resource keeps the configured value of a field whose response takes another shape, and
// of the hash form, which the response holds at the token's name.
func TestShapesKeepTheConfiguredValue(t *testing.T) {
	r := shapesResource(t)
	caps := r.objType.AttrTypes["caps"].(basetypes.ObjectType)
	a := caps.AttrTypes["a"].(basetypes.ObjectType)
	configured := types.ObjectValueMust(caps.AttrTypes, map[string]attr.Value{"a": types.ObjectValueMust(a.AttrTypes, map[string]attr.Value{"requested": types.BoolValue(true)})})
	dataType := r.objType.AttrTypes["source_data"].(basetypes.ObjectType)
	data := types.ObjectValueMust(dataType.AttrTypes, map[string]attr.Value{"number": types.StringValue("000123456789")})
	prior := r.fill(map[string]attr.Value{"caps": configured, "source_data": data})
	body := wire.Object{{Name: "id", Value: wire.String("shp_1")}, {Name: "source", Value: wire.String("ba_1")},
		{Name: "caps", Value: wire.Object{{Name: "a", Value: wire.String("active")}}}}
	obj, diags := r.m.decodeState(body, prior, r.objType, false, nil)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if got := obj.Attributes()["caps"]; !got.Equal(configured) {
		t.Errorf("caps = %s, want the configured value", got)
	}
	if got := obj.Attributes()["source_data"]; !got.Equal(data) {
		t.Errorf("source_data = %s, want the configured value", got)
	}
	if got := obj.Attributes()["source"]; !got.Equal(types.StringValue("ba_1")) {
		t.Errorf("source = %s, want what Stripe returned", got)
	}
}

// The hash form is sent under the parameter's name, and switching from the token to the hash
// sends only the hash.
func TestShapesSendTheHashFormUnderItsName(t *testing.T) {
	r := shapesResource(t)
	dataType := r.objType.AttrTypes["source_data"].(basetypes.ObjectType)
	data := types.ObjectValueMust(dataType.AttrTypes, map[string]attr.Value{"number": types.StringValue("000123456789")})
	body, err := r.m.encodeCreate(r.fill(map[string]attr.Value{"source_data": data}))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := wire.EncodeForm(body); err != nil || got != "source%5Bnumber%5D=000123456789" {
		t.Errorf("create body = %q (%v)", got, err)
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "source": types.StringValue("btok_1"), "source_data": types.ObjectNull(dataType.AttrTypes)})
	plan := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "source": types.StringNull(), "source_data": data})
	params, err := r.m.encodeUpdate(plan, state, tftypes.Value{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := wire.EncodeForm(params); err != nil || got != "source%5Bnumber%5D=000123456789" {
		t.Errorf("update body = %q (%v)", got, err)
	}
}

// A configuration sets at most one form of a token-or-hash parameter.
func TestShapesRefuseBothFormsOfOneParameter(t *testing.T) {
	r := shapesResource(t)
	data := r.schema.Attributes["source_data"].(schema.SingleNestedAttribute)
	dataType := r.objType.AttrTypes["source_data"].(basetypes.ObjectType)
	hash := types.ObjectValueMust(dataType.AttrTypes, map[string]attr.Value{"number": types.StringValue("000123456789")})
	for name, tc := range map[string]struct {
		token attr.Value
		fails bool
	}{
		"both forms":       {types.StringValue("btok_1"), true},
		"the hash only":    {types.StringNull(), false},
		"an unknown token": {types.StringUnknown(), false},
	} {
		config := r.fill(map[string]attr.Value{"source": tc.token, "source_data": hash})
		raw, err := config.ToTerraformValue(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		req := validator.ObjectRequest{Path: path.Root("source_data"), ConfigValue: hash, Config: tfsdk.Config{Raw: raw, Schema: r.schema}}
		var resp validator.ObjectResponse
		data.Validators[0].ValidateObject(context.Background(), req, &resp)
		if resp.Diagnostics.HasError() != tc.fails {
			t.Errorf("%s: diagnostics %v", name, resp.Diagnostics)
		}
		if tc.fails && !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "Set `source` or `source_data`, not both") {
			t.Errorf("%s: %s", name, resp.Diagnostics.Errors()[0].Detail())
		}
	}
}

// A hash with no members, configured `{}`, is sent on /v2 as `{}`, which the pruning of empty
// collections keeps.
func TestShapesSendAHashWithNoMembersOnV2(t *testing.T) {
	r := configured(t, shapesV2Declaration(), shapesTable(), newStubV2())
	body, err := r.m.encodeCreate(r.fill(map[string]attr.Value{"options": emptyAffirm(r)}))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(must(wire.EncodeJSON(body))); !strings.Contains(got, `"options":{"affirm":{}}`) {
		t.Errorf("/v2 body = %s", got)
	}
}

func shapesV2Declaration() Declaration {
	decl := shapesDeclaration()
	decl.TypeName = "stripe_shape_v2"
	decl.Paths = Paths{Create: "/v2/core/shapes", Retrieve: "/v2/core/shapes/{id}", Update: "/v2/core/shapes/{id}", Delete: "/v2/core/shapes/{id}", List: "/v2/core/shapes"}
	return decl
}

// emptyAffirm is `options = { affirm = {} }`.
func emptyAffirm(r *Resource) basetypes.ObjectValue {
	options := r.objType.AttrTypes["options"].(basetypes.ObjectType)
	return objectOf(options, map[string]attr.Value{"affirm": objectOf(options.AttrTypes["affirm"], nil)})
}

// On /v1 an empty value unsets a field, so until the conformance suite proves what Stripe takes
// for a hash with no members, a configuration that sets one is refused at plan, naming it, and
// the encoder never sends one (owner's decision, 2026-10-29).
func TestShapesRefuseAHashWithNoMembersOnV1(t *testing.T) {
	r := shapesResource(t)
	options := r.schema.Attributes["options"].(schema.SingleNestedAttribute)
	affirm := options.Attributes["affirm"].(schema.SingleNestedAttribute)
	if len(affirm.Validators) != 1 {
		t.Fatalf("options.affirm has %d validators, want the /v1 refusal", len(affirm.Validators))
	}
	set := emptyAffirm(r)
	value := set.Attributes()["affirm"].(basetypes.ObjectValue)
	var resp validator.ObjectResponse
	affirm.Validators[0].ValidateObject(context.Background(), validator.ObjectRequest{Path: path.Root("options").AtName("affirm"), ConfigValue: value}, &resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "`options.affirm`") {
		t.Errorf("diagnostics: %v", resp.Diagnostics)
	}
	resp = validator.ObjectResponse{}
	affirm.Validators[0].ValidateObject(context.Background(), validator.ObjectRequest{Path: path.Root("options").AtName("affirm"), ConfigValue: types.ObjectNull(value.AttributeTypes(context.Background()))}, &resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("an unset hash is refused: %v", resp.Diagnostics)
	}
	if _, err := r.m.encodeCreate(r.fill(map[string]attr.Value{"options": set})); err == nil || !strings.Contains(err.Error(), "options.affirm") {
		t.Errorf("create: err = %v", err)
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "options": types.ObjectNull(set.AttributeTypes(context.Background()))})
	planned := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "options": set})
	if _, err := r.m.encodeUpdate(planned, state, raw(t, planned)); err == nil || !strings.Contains(err.Error(), "options.affirm") {
		t.Errorf("update: err = %v", err)
	}
	v2 := configured(t, shapesV2Declaration(), shapesTable(), newStubV2())
	if affirm := v2.schema.Attributes["options"].(schema.SingleNestedAttribute).Attributes["affirm"].(schema.SingleNestedAttribute); len(affirm.Validators) != 0 {
		t.Error("/v2 sends `{}` and refuses nothing")
	}
}

// Both forms of one parameter set and known, which the plan-time validator lets through when
// the token was unknown there, fail in the encoder, at any depth, on create and on update.
func TestShapesRefuseBothFormsWhenTheEncoderSeesThem(t *testing.T) {
	r := shapesResource(t)
	dataType := r.objType.AttrTypes["source_data"].(basetypes.ObjectType)
	data := types.ObjectValueMust(dataType.AttrTypes, map[string]attr.Value{"number": types.StringValue("000123456789")})
	both := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "source": types.StringValue("btok_1"), "source_data": data})
	if _, err := r.m.encodeCreate(both); err == nil || !strings.Contains(err.Error(), "both `source` and `source_data`") {
		t.Errorf("create: err = %v", err)
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "source": types.StringValue("btok_1"), "source_data": types.ObjectNull(dataType.AttrTypes)})
	if _, err := r.m.encodeUpdate(both, state, raw(t, both)); err == nil || !strings.Contains(err.Error(), "both `source` and `source_data`") {
		t.Errorf("update: err = %v", err)
	}
	// After a create through the hash form, the plan carries the token Stripe returned; only
	// the configuration says which form is set.
	created := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "source": types.StringValue("ba_1"), "source_data": data, "tax_id": types.StringValue("1")})
	renamed := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "source": types.StringValue("ba_1"), "source_data": data, "tax_id": types.StringValue("2")})
	config := r.fill(map[string]attr.Value{"source": types.StringNull(), "source_data": data, "tax_id": types.StringValue("2")})
	if _, err := r.m.encodeUpdate(renamed, created, raw(t, config)); err != nil {
		t.Errorf("update after a create through the hash form: %v", err)
	}

	items := r.objType.AttrTypes["items"].(basetypes.ListType).ElemType.(basetypes.ObjectType)
	priceData := items.AttrTypes["price_data"].(basetypes.ObjectType)
	element := objectOf(items, map[string]attr.Value{
		"price":      types.StringValue("price_1"),
		"price_data": objectOf(priceData, map[string]attr.Value{"unit_amount": types.Int64Value(100)}),
	})
	nested := r.fill(map[string]attr.Value{"items": types.ListValueMust(items, []attr.Value{element})})
	if _, err := r.m.encodeCreate(nested); err == nil || !strings.Contains(err.Error(), "both `items[0].price` and `items[0].price_data`") {
		t.Errorf("nested create: err = %v", err)
	}
}

// The token of a parameter whose response returns it is Optional and Computed beside its hash
// form: a create through the hash takes the ID Stripe returns. Its state value is kept across a
// plan that changes neither form, and is unknown when the hash changes.
func TestShapesTokenIsComputedBesideItsHashForm(t *testing.T) {
	r := shapesResource(t)
	token, ok := r.schema.Attributes["source"].(schema.StringAttribute)
	if !ok || !token.Optional || !token.Computed || token.Required {
		t.Fatalf("source: %#v", r.schema.Attributes["source"])
	}
	dataType := r.objType.AttrTypes["source_data"].(basetypes.ObjectType)
	hash := func(number string) basetypes.ObjectValue {
		return types.ObjectValueMust(dataType.AttrTypes, map[string]attr.Value{"number": types.StringValue(number)})
	}
	keep := func(planned basetypes.ObjectValue) basetypes.StringValue {
		t.Helper()
		state := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "source": types.StringValue("ba_1"), "source_data": hash("1")})
		plan := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "source_data": planned})
		req := planmodifier.StringRequest{Path: path.Root("source"), ConfigValue: types.StringNull(), StateValue: types.StringValue("ba_1"), PlanValue: types.StringUnknown(),
			Plan: tfsdk.Plan{Raw: must2(plan.ToTerraformValue(context.Background())), Schema: r.schema}, State: tfsdk.State{Raw: must2(state.ToTerraformValue(context.Background())), Schema: r.schema}}
		resp := planmodifier.StringResponse{PlanValue: req.PlanValue}
		for _, mod := range token.PlanModifiers {
			mod.PlanModifyString(context.Background(), req, &resp)
		}
		return resp.PlanValue
	}
	if got := keep(hash("1")); !got.Equal(types.StringValue("ba_1")) {
		t.Errorf("an unchanged hash form plans source = %s, want the state's", got)
	}
	if got := keep(hash("2")); !got.IsUnknown() {
		t.Errorf("a changed hash form plans source = %s, want unknown", got)
	}
}

// A parameter the create request requires takes either form: both are Optional, a
// configuration sets exactly one, and the encoder refuses a create with neither.
func TestShapesARequiredParameterTakesEitherForm(t *testing.T) {
	table := shapesTable()
	table[1].Required, table[2].Required = true, true
	r := configured(t, shapesDeclaration(), table, newStub())
	token, ok := r.schema.Attributes["source"].(schema.StringAttribute)
	if !ok || token.Required || !token.Optional {
		t.Fatalf("source: %#v", r.schema.Attributes["source"])
	}
	if data := r.schema.Attributes["source_data"].(schema.SingleNestedAttribute); data.Required || !data.Optional {
		t.Errorf("source_data: %#v", data)
	}
	dataType := r.objType.AttrTypes["source_data"].(basetypes.ObjectType)
	config := r.fill(map[string]attr.Value{"source": types.StringNull(), "source_data": types.ObjectNull(dataType.AttrTypes)})
	raw := must2(config.ToTerraformValue(context.Background()))
	var resp validator.StringResponse
	for _, v := range token.Validators {
		v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("source"), ConfigValue: types.StringNull(), Config: tfsdk.Config{Raw: raw, Schema: r.schema}}, &resp)
	}
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "Set `source` or `source_data`") {
		t.Errorf("neither form: %v", resp.Diagnostics)
	}
	if _, err := r.m.encodeCreate(config); err == nil || !strings.Contains(err.Error(), "neither `source` nor `source_data`") {
		t.Errorf("create with neither: err = %v", err)
	}
}

// A declaration entry names one form: the token by its path, the hash form by its attribute
// name, `<name>_data`.
func TestShapesDeclarationEntriesNameOneForm(t *testing.T) {
	decl := shapesDeclaration()
	decl.Rename = map[string]string{"source": "source_token"}
	decl.Sensitive = []string{"source_data.number"}
	decl.PreserveConfig = []string{"source"}
	r := configured(t, decl, shapesTable(), newStub())
	if _, ok := r.schema.Attributes["source_token"].(schema.StringAttribute); !ok {
		t.Errorf("the token is renamed: %v", r.schema.Attributes["source_token"])
	}
	data, ok := r.schema.Attributes["source_data"].(schema.SingleNestedAttribute)
	if !ok || len(data.Validators) == 0 || data.Validators[0] != (oneForm{token: "source_token", hash: "source_data"}) {
		t.Fatalf("the hash form keeps its name and pairs with the renamed token: %#v", r.schema.Attributes["source_data"])
	}
	if !data.Attributes["number"].IsSensitive() || r.schema.Attributes["source_token"].IsSensitive() {
		t.Error("`source_data.number` is sensitive, the token is not")
	}
	for _, n := range r.m.nodes {
		if n.Name == "source" && n.Preserve != (n.AttrName() == "source_token") {
			t.Errorf("preserve_config `source` reached %s", n.AttrName())
		}
	}
}

// objectOf builds an object of the attribute types of t from the values given, the rest null.
func objectOf(t attr.Type, set map[string]attr.Value) basetypes.ObjectValue {
	ot := t.(basetypes.ObjectType)
	attrs := make(map[string]attr.Value, len(ot.AttrTypes))
	for name, typ := range ot.AttrTypes {
		if v, ok := set[name]; ok {
			attrs[name] = v
		} else {
			attrs[name] = nullOf(typ)
		}
	}
	return types.ObjectValueMust(ot.AttrTypes, attrs)
}

// Inside a nested object or a list element, the plan of an update carries the token Stripe
// returned beside the configured hash; when the parent is sent again for a change of a sibling,
// only the form the configuration sets, the hash, is sent.
func TestShapesSendOnlyTheConfiguredFormOfANestedPair(t *testing.T) {
	r := shapesResource(t)
	planType := r.objType.AttrTypes["plan_info"]
	priceData := planType.(basetypes.ObjectType).AttrTypes["price_data"]
	hash := objectOf(priceData, map[string]attr.Value{"unit_amount": types.Int64Value(100)})
	info := func(token attr.Value, note string) basetypes.ObjectValue {
		return objectOf(planType, map[string]attr.Value{"price": token, "price_data": hash, "note": types.StringValue(note)})
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "plan_info": info(types.StringValue("pr_1"), "a")})
	plan := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "plan_info": info(types.StringValue("pr_1"), "b")})
	config := r.fill(map[string]attr.Value{"plan_info": info(types.StringNull(), "b")})
	params, err := r.m.encodeUpdate(plan, state, raw(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := wire.EncodeForm(params); got != "plan_info%5Bprice%5D%5Bunit_amount%5D=100&plan_info%5Bnote%5D=b" {
		t.Errorf("nested object: update body = %q", got)
	}

	itemType := r.objType.AttrTypes["items"].(basetypes.ListType).ElemType
	item := func(token attr.Value, quantity int64) basetypes.ObjectValue {
		return objectOf(itemType, map[string]attr.Value{"price": token, "price_data": hash, "quantity": types.Int64Value(quantity)})
	}
	list := func(e basetypes.ObjectValue) basetypes.ListValue {
		return types.ListValueMust(itemType, []attr.Value{e})
	}
	state = r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "items": list(item(types.StringValue("pr_1"), 1))})
	plan = r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "items": list(item(types.StringValue("pr_1"), 2))})
	config = r.fill(map[string]attr.Value{"items": list(item(types.StringNull(), 2))})
	if params, err = r.m.encodeUpdate(plan, state, raw(t, config)); err != nil {
		t.Fatal(err)
	}
	if got, _ := wire.EncodeForm(params); got != "items%5B0%5D%5Bprice%5D%5Bunit_amount%5D=100&items%5B0%5D%5Bquantity%5D=2" {
		t.Errorf("list element: update body = %q", got)
	}
}

// A token inside a list element is Computed but never taken from the state: an insert or a
// reorder would give it another element's value. It is unknown on a change, as a value Stripe
// fills in for a list element is, and a create-only one replaces only when the configuration
// sets another value, so an unknown one plans no replacement.
func TestShapesTokenInAListIsNeverTakenFromAnotherElement(t *testing.T) {
	describe := func(mods []planmodifier.String) []string {
		var out []string
		for _, m := range mods {
			out = append(out, m.Description(context.Background()))
		}
		return out
	}
	r := shapesResource(t)
	price := r.schema.Attributes["items"].(schema.ListNestedAttribute).NestedObject.Attributes["price"].(schema.StringAttribute)
	if !price.Optional || !price.Computed || len(price.PlanModifiers) != 0 {
		t.Errorf("items.price: computed %v, plan modifiers %v", price.Computed, describe(price.PlanModifiers))
	}
	if top := r.schema.Attributes["plan_info"].(schema.SingleNestedAttribute).Attributes["price"].(schema.StringAttribute); len(top.PlanModifiers) != 1 {
		t.Errorf("outside a list the token keeps its state value: %v", describe(top.PlanModifiers))
	}

	table := shapesTable()
	for i := range table {
		if table[i].Name == "items" {
			for j := range table[i].Fields {
				table[i].Fields[j].Update = false
			}
		}
	}
	created := configured(t, shapesDeclaration(), table, newStub())
	price = created.schema.Attributes["items"].(schema.ListNestedAttribute).NestedObject.Attributes["price"].(schema.StringAttribute)
	got := describe(price.PlanModifiers)
	if len(got) != 1 || got[0] != replaceIfSetDescription {
		t.Errorf("a create-only token in a list: plan modifiers %v, want only the replacement when the configuration sets another value", got)
	}
}

// On /v1 an empty value unsets a field, so an object whose configuration would encode to
// nothing, `{}` or every member null, is refused at plan, naming it, in a resource and in a
// data source's lookup; the encoder refuses an empty object it would send, as when an update
// leaves out what Stripe filled in. Maps are not objects here, and /v2 sends `{}`.
func TestShapesRefuseAnObjectThatEncodesToNothingOnV1(t *testing.T) {
	r := shapesResource(t)
	validate := func(a schema.SingleNestedAttribute, at path.Path, v basetypes.ObjectValue) string {
		t.Helper()
		var resp validator.ObjectResponse
		for _, val := range a.Validators {
			val.ValidateObject(context.Background(), validator.ObjectRequest{Path: at, ConfigValue: v}, &resp)
		}
		if !resp.Diagnostics.HasError() {
			return ""
		}
		return resp.Diagnostics.Errors()[0].Detail()
	}
	options := r.schema.Attributes["options"].(schema.SingleNestedAttribute)
	card := options.Attributes["card"].(schema.SingleNestedAttribute)
	cardType := r.objType.AttrTypes["options"].(basetypes.ObjectType).AttrTypes["card"]
	if got := validate(card, path.Root("options").AtName("card"), objectOf(cardType, nil)); !strings.Contains(got, "`options.card`") {
		t.Errorf("options.card = {}: %q", got)
	}
	if got := validate(card, path.Root("options").AtName("card"), objectOf(cardType, map[string]attr.Value{"request_three_d_secure": types.StringValue("any")})); got != "" {
		t.Errorf("options.card with a member: %q", got)
	}
	if got := validate(card, path.Root("options").AtName("card"), objectOf(cardType, map[string]attr.Value{"request_three_d_secure": types.StringUnknown()})); got != "" {
		t.Errorf("options.card with an unknown member: %q", got)
	}
	if got := validate(options, path.Root("options"), objectOf(r.objType.AttrTypes["options"], nil)); !strings.Contains(got, "`options`") {
		t.Errorf("options = {}: %q", got)
	}
	items := r.schema.Attributes["items"].(schema.ListNestedAttribute)
	itemType := r.objType.AttrTypes["items"].(basetypes.ListType).ElemType
	var resp validator.ObjectResponse
	for _, val := range items.NestedObject.Validators {
		val.ValidateObject(context.Background(), validator.ObjectRequest{Path: path.Root("items").AtListIndex(0), ConfigValue: objectOf(itemType, nil)}, &resp)
	}
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "`items[0]`") {
		t.Errorf("items = [{}]: %v", resp.Diagnostics)
	}

	allNull := objectOf(r.objType.AttrTypes["options"], map[string]attr.Value{"card": objectOf(cardType, nil)})
	if _, err := r.m.encodeCreate(r.fill(map[string]attr.Value{"options": allNull})); err == nil || !strings.Contains(err.Error(), "`options.card`") {
		t.Errorf("create: err = %v", err)
	}
	settingsType := r.objType.AttrTypes["settings"]
	branding := objectOf(settingsType.(basetypes.ObjectType).AttrTypes["branding"], map[string]attr.Value{"color": types.StringValue("blue")})
	state := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "settings": objectOf(settingsType, map[string]attr.Value{"mode": types.StringValue("a"), "branding": branding})})
	plan := r.fill(map[string]attr.Value{"id": types.StringValue("shp_1"), "settings": objectOf(settingsType, map[string]attr.Value{"branding": branding})})
	if _, err := r.m.encodeUpdate(plan, state, raw(t, plan)); err == nil || !strings.Contains(err.Error(), "`settings`") {
		t.Errorf("an update that leaves out Stripe's defaults and sends nothing else: err = %v", err)
	}

	v2 := configured(t, shapesV2Declaration(), shapesTable(), newStubV2())
	if card := v2.schema.Attributes["options"].(schema.SingleNestedAttribute).Attributes["card"].(schema.SingleNestedAttribute); len(card.Validators) != 0 {
		t.Error("/v2 refuses nothing")
	}
	if _, err := v2.m.encodeCreate(v2.fill(map[string]attr.Value{"options": allNull})); err != nil {
		t.Errorf("/v2 create: %v", err)
	}

	ds := findDataSource(t, vaultDeclaration(), vaultTable(), newStub())
	scope := ds.schema.Attributes["scope"].(dschema.SingleNestedAttribute)
	var dresp validator.ObjectResponse
	for _, val := range scope.Validators {
		val.ValidateObject(context.Background(), validator.ObjectRequest{Path: path.Root("scope"), ConfigValue: objectOf(ds.objType.AttrTypes["scope"], nil)}, &dresp)
	}
	if !dresp.Diagnostics.HasError() || !strings.Contains(dresp.Diagnostics.Errors()[0].Detail(), "`scope`") {
		t.Errorf("a data source's scope = {}: %v", dresp.Diagnostics)
	}
}

func must2(v tftypes.Value, err error) tftypes.Value {
	if err != nil {
		panic(err)
	}
	return v
}

func must(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}
