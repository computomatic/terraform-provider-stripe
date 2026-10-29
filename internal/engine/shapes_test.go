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
		}},
		{Name: "items", Shape: fields.ObjectList, Create: true, Update: true, Fields: []fields.Field{
			{Name: "price", Shape: fields.String, Create: true, Update: true},
			{Name: "price", Attr: "price_data", Shape: fields.Object, Create: true, Update: true, Fields: []fields.Field{
				{Name: "unit_amount", Shape: fields.Int, Create: true, Update: true},
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
	if !ok || !data.Optional || data.Computed || len(data.Validators) != 1 {
		t.Fatalf("source_data: %#v", r.schema.Attributes["source_data"])
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
	affirm := options.AttrTypes["affirm"].(basetypes.ObjectType)
	return types.ObjectValueMust(options.AttrTypes, map[string]attr.Value{"affirm": types.ObjectValueMust(affirm.AttrTypes, map[string]attr.Value{})})
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
	element := types.ObjectValueMust(items.AttrTypes, map[string]attr.Value{
		"price":      types.StringValue("price_1"),
		"price_data": types.ObjectValueMust(priceData.AttrTypes, map[string]attr.Value{"unit_amount": types.Int64Value(100)}),
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
	if !ok || len(data.Validators) != 1 {
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
