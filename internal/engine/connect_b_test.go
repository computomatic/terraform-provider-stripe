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

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// closableTable is the fixture with a v2 account's `closed` flag and its applied
// configurations.
func closableTable() []fields.Field {
	return append(fixtureTable(),
		fields.Field{Name: "closed", Shape: fields.Bool, Response: true},
		fields.Field{Name: "applied", Shape: fields.ScalarList, Elem: fields.String, Response: true},
		fields.Field{Name: "identity", Shape: fields.Object, Response: true, Fields: []fields.Field{{Name: "country", Shape: fields.String, Response: true}}},
	)
}

// closableDeclaration closes a /v2 widget as a v2 account is closed: a bool transition whose
// action must name the applied configurations. It includes a nested field, as a v2 account
// includes its configurations.
func closableDeclaration() Declaration {
	decl := v2Declaration()
	decl.Include = []string{"identity.country"}
	decl.Transitions = []Transition{{Field: "closed", Value: "true", Path: "/v2/core/widgets/{id}/close", Send: []string{"applied"}}}
	return decl
}

// closableStub serves one /v2 widget. Like Stripe's v2 account it returns the included
// `identity` only when asked, and as null otherwise, and closes the widget only when the close names
// exactly its applied configurations.
type closableStub struct {
	closed bool
	log    []stripeapi.Request
}

func (c *closableStub) Do(_ context.Context, req stripeapi.Request) (*stripeapi.Response, error) {
	c.log = append(c.log, req)
	if strings.HasSuffix(req.Path, "/close") {
		applied, _ := req.Params.Get("applied")
		if b, err := wire.EncodeJSON(applied); err != nil || string(b) != `["customer","merchant"]` {
			return nil, &stripeapi.APIError{Status: 400, Code: "configs_must_match_to_close"}
		}
		c.closed = true
	}
	identity := wire.Value(wire.Null{})
	if _, included := req.Params.Get("include"); included {
		identity = wire.Object{{Name: "country", Value: wire.String("US")}}
	}
	return &stripeapi.Response{Body: wire.Object{
		{Name: "id", Value: wire.String("wid_1")},
		{Name: "name", Value: wire.String("w")},
		{Name: "identity", Value: identity},
		{Name: "applied", Value: wire.Array{wire.String("customer"), wire.String("merchant")}},
		{Name: "closed", Value: wire.Bool(c.closed)},
	}}, nil
}

func (c *closableStub) paths() string {
	var out []string
	for _, req := range c.log {
		out = append(out, req.Method+" "+req.Path)
	}
	return strings.Join(out, ";")
}

// A bool transition field is closed by its action, which carries the fields it sends as the
// object holds them and no `include`; the included field the action returns as null keeps the
// object's value.
func TestBoolTransitionSendsTheObjectsFieldsAndKeepsIncludedOnes(t *testing.T) {
	stub := &closableStub{}
	r := configured(t, closableDeclaration(), closableTable(), stub)
	state, _ := r.create(t, r.fill(map[string]attr.Value{"name": types.StringValue("w")}))
	if got := stub.paths(); got != "POST /v2/core/widgets" {
		t.Fatalf("an unset transition field sends no action: %s", got)
	}

	stub.log = nil
	plan := r.fill(map[string]attr.Value{"id": state.Attributes()["id"], "name": types.StringValue("w"), "closed": types.BoolValue(true)})
	next := r.update(t, plan, state)
	if got := stub.paths(); got != "GET /v2/core/widgets/wid_1;POST /v2/core/widgets/wid_1/close" {
		t.Fatalf("requests: %s", got)
	}
	closeReq := stub.log[1]
	if _, sent := closeReq.Params.Get("include"); sent {
		t.Errorf("a /v2 action is sent no include: %+v", closeReq.Params)
	}
	if len(closeReq.Params) != 1 {
		t.Errorf("the action is sent only the fields it names: %+v", closeReq.Params)
	}
	if v, ok := next.Attributes()["closed"].(basetypes.BoolValue); !ok || !v.ValueBool() {
		t.Errorf("closed: %v", next.Attributes()["closed"])
	}
	identity, ok := next.Attributes()["identity"].(basetypes.ObjectValue)
	if !ok || identity.IsNull() || identity.Attributes()["country"].String() != `"US"` {
		t.Errorf("the included identity must survive the action's null: %v", next.Attributes()["identity"])
	}

	stub.log = nil
	r.update(t, plan, next)
	if got := stub.paths(); got != "GET /v2/core/widgets/wid_1" {
		t.Errorf("a closed widget sends no action: %s", got)
	}
}

// A bool transition takes `true` or `false` only and has no final value, the configuration takes only the value a
// transition sets, and a sent field must be a top-level field.
func TestBoolTransitionRules(t *testing.T) {
	for name, transition := range map[string]Transition{
		"not a bool value": {Field: "closed", Value: "yes", Path: "/v2/core/widgets/{id}/close"},
		"a final value":    {Field: "closed", Value: "true", Path: "/v2/core/widgets/{id}/close", Final: []string{"false"}},
		"sends no field":   {Field: "closed", Value: "true", Path: "/v2/core/widgets/{id}/close", Send: []string{"nothing"}},
	} {
		decl := v2Declaration()
		decl.Transitions = []Transition{transition}
		if _, err := NewResource(decl, closableTable()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	r := configured(t, closableDeclaration(), closableTable(), noCalls(t))
	for value, wantErr := range map[bool]bool{true: false, false: true} {
		config := r.fill(map[string]attr.Value{"name": types.StringValue("w"), "closed": types.BoolValue(value)})
		var resp resource.ValidateConfigResponse
		r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: r.schema, Raw: raw(t, config)}}, &resp)
		if resp.Diagnostics.HasError() != wantErr {
			t.Errorf("closed = %v: error = %v, want %v", value, resp.Diagnostics.HasError(), wantErr)
		}
	}
}

// A planned value whose computed members are unknown, as on every change of an object Stripe
// fills in, such as a v2 account's configurations with their capability statuses, is refused
// when what the configuration sets differs from the state. It passes when only the computed
// members are unknown, and a configured value not known yet is left for apply.
func TestRefuseChangeSeesThroughUnknownComputedMembers(t *testing.T) {
	table := append(refuseTable(), fields.Field{Name: "plan", Shape: fields.Object, Create: true, Update: true, Response: true, Fields: []fields.Field{
		{Name: "tier", Shape: fields.String, Create: true, Update: true, Response: true},
		{Name: "status", Shape: fields.String, Response: true},
	}})
	decl := fixtureDeclaration()
	decl.RefuseChange = []string{"plan"}
	r := configured(t, decl, table, noCalls(t))
	planType := r.objType.AttrTypes["plan"].(basetypes.ObjectType).AttrTypes
	value := func(tier, status attr.Value) attr.Value {
		return types.ObjectValueMust(planType, map[string]attr.Value{"tier": tier, "status": status})
	}
	state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "plan": value(types.StringValue("a"), types.StringValue("ok"))})
	modify := func(planned, config attr.Value) *resource.ModifyPlanResponse {
		return r.modifyPlan(t, r.fill(with(state, map[string]attr.Value{"plan": planned})), state, r.fill(with(state, map[string]attr.Value{"plan": config})))
	}
	if resp := modify(value(types.StringValue("b"), types.StringUnknown()), value(types.StringValue("b"), types.StringNull())); !resp.Diagnostics.HasError() {
		t.Error("a configured change beside an unknown computed member must fail")
	}
	if resp := modify(value(types.StringValue("a"), types.StringUnknown()), value(types.StringValue("a"), types.StringNull())); resp.Diagnostics.HasError() {
		t.Errorf("an unknown computed member alone is no change: %v", resp.Diagnostics)
	}
	if resp := modify(value(types.StringUnknown(), types.StringUnknown()), value(types.StringUnknown(), types.StringNull())); resp.Diagnostics.HasError() {
		t.Errorf("a configured value not known yet is left for apply: %v", resp.Diagnostics)
	}
}

// A field Stripe fills in that the configuration leaves unset is unknown on every change of
// the object, such as an invoice's `number`, which finalizing sets. That is no change of it,
// whether the state holds a value or none.
func TestRefuseChangeLeavesAnUnsetServerDefault(t *testing.T) {
	table := append(refuseTable(), fields.Field{Name: "number", Shape: fields.String, Create: true, Update: true, Response: true, ServerDefaulted: true})
	decl := fixtureDeclaration()
	decl.RefuseChange = []string{"number"}
	r := configured(t, decl, table, noCalls(t))
	for name, prior := range map[string]attr.Value{"none": types.StringNull(), "a value": types.StringValue("INV-1")} {
		state := r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "number": prior})
		resp := r.modifyPlan(t, r.fill(with(state, map[string]attr.Value{"number": types.StringUnknown()})), state, r.fill(with(state, map[string]attr.Value{"number": types.StringNull()})))
		if resp.Diagnostics.HasError() {
			t.Errorf("%s in state: an unset server default is no change: %v", name, resp.Diagnostics)
		}
	}
}
