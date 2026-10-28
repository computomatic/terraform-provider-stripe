package engine

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// A response that says the object is closed keeps the prior value of every field it leaves out
// or returns null, a whole object and a member alike, and reads what it does return. A
// response that says the object is open reads a null as null, and so does a data source of a
// closed object.
func TestKeepWhenClosedKeepsWhatAClosedObjectNulls(t *testing.T) {
	decl := fixtureDeclaration()
	decl.KeepWhenClosed = "active"
	var body wire.Object
	r := configured(t, decl, fixtureTable(), clientFunc(func(context.Context, stripeapi.Request) (*stripeapi.Response, error) {
		return &stripeapi.Response{Body: body}, nil
	}))
	addressType := r.objType.AttrTypes["address"]
	address := object(addressType, map[string]attr.Value{"line1": types.StringValue("1 Main"), "zip": types.StringValue("10001")})
	state := r.fill(map[string]attr.Value{
		"id": types.StringValue("wid_1"), "name": types.StringValue("n"), "active": types.BoolValue(true), "kind": types.StringValue("a"),
		"quantity": types.Int64Value(3), "address": address, "metadata": stringMapValue(map[string]string{"k": "v"}),
	})
	respond := func(active bool, fields ...wire.Field) {
		body = append(wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("renamed")}, {Name: "active", Value: wire.Bool(active)}}, fields...)
	}

	respond(true, wire.Field{Name: "kind", Value: wire.Null{}}, wire.Field{Name: "metadata", Value: wire.Null{}}, wire.Field{Name: "address", Value: wire.Null{}})
	got, _ := r.read(t, state)
	for _, name := range []string{"kind", "quantity", "address", "metadata"} {
		if !got.Attributes()[name].Equal(state.Attributes()[name]) {
			t.Errorf("a closed object keeps %s: %v", name, got.Attributes()[name])
		}
	}
	if str(t, got, "name") != "renamed" {
		t.Errorf("a closed object reads what Stripe returns: %v", got.Attributes()["name"])
	}

	respond(true, wire.Field{Name: "address", Value: wire.Object{{Name: "line1", Value: wire.String("2 Main")}, {Name: "zip", Value: wire.Null{}}}})
	got, _ = r.read(t, state)
	if want := object(addressType, map[string]attr.Value{"line1": types.StringValue("2 Main"), "zip": types.StringValue("10001")}); !got.Attributes()["address"].Equal(want) {
		t.Errorf("a closed object keeps a nulled member: %v", got.Attributes()["address"])
	}

	respond(false, wire.Field{Name: "kind", Value: wire.Null{}})
	got, _ = r.read(t, state)
	if !got.Attributes()["kind"].IsNull() || !got.Attributes()["quantity"].IsNull() {
		t.Errorf("the control: an open object reads a null as null: %v", got)
	}

	respond(true, wire.Field{Name: "kind", Value: wire.Null{}})
	shown, diags := r.m.decodeState(body, basetypes.NewObjectNull(r.objType.AttrTypes), r.objType, true, nil)
	noErrors(t, diags)
	if !shown.Attributes()["kind"].IsNull() {
		t.Errorf("a data source shows what Stripe returns: %v", shown.Attributes()["kind"])
	}
}

func TestKeepWhenClosedMustNameAReturnedBool(t *testing.T) {
	table := append(fixtureTable(), fields.Field{Name: "notify", Shape: fields.Bool, Create: true, Update: true})
	for _, name := range []string{"nothing", "name", "notify"} {
		decl := fixtureDeclaration()
		decl.KeepWhenClosed = name
		if _, err := NewResource(decl, table); err == nil {
			t.Errorf("keep-when-closed %q must fail", name)
		}
	}
	decl := fixtureDeclaration()
	decl.KeepWhenClosed = "active"
	if _, err := NewResource(decl, table); err != nil {
		t.Errorf("the control: a returned bool must compile: %v", err)
	}
}
