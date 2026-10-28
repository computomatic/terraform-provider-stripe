package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

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

// closedVaultDeclaration is a closable object whose destroy is refused, as a v2 account's is:
// `country` is create-only, so changing it would replace the object.
func closedVaultDeclaration() (Declaration, []fields.Field) {
	return Declaration{
		TypeName:       "stripe_vault",
		IDPrefix:       "vlt_",
		Paths:          Paths{Create: "/v1/vaults", Retrieve: "/v1/vaults/{id}", Update: "/v1/vaults/{id}"},
		Delete:         Refuse,
		RefuseReason:   "Stripe cannot delete a vault.",
		KeepWhenClosed: "closed",
	}, []fields.Field{
		{Name: "id", Shape: fields.String, Response: true},
		{Name: "name", Shape: fields.String, Create: true, Update: true, Response: true},
		{Name: "country", Shape: fields.String, Create: true, Response: true},
		{Name: "closed", Shape: fields.Bool, Response: true},
	}
}

// A plan that changes an object the state shows closed fails with one error that says so: an
// update, a replacement, whose own refusal it takes the place of, and an import of a closed
// object, whose state lacks what Stripe nulls. A plan that changes nothing passes, and so does
// one whose configuration differs but whose proposal is the state, as `ignore_changes = all`
// makes it. A destroy keeps its own refusal. The same changes to an open object plan as usual.
func TestKeepWhenClosedRefusesChangingAClosedObject(t *testing.T) {
	decl, table := closedVaultDeclaration()
	p := newPlanner(t, decl, table)
	state := func(closed bool, name, country tftypes.Value) tftypes.Value {
		return p.value(map[string]tftypes.Value{"id": tfString("vlt_1"), "name": name, "country": country, "closed": tftypes.NewValue(tftypes.Bool, closed)})
	}
	config := func(name, country string) tftypes.Value {
		return p.value(map[string]tftypes.Value{"name": tfString(name), "country": tfString(country)})
	}
	null := tftypes.NewValue(tftypes.String, nil)
	summaries := func(resp *tfprotov6.PlanResourceChangeResponse) []string {
		var got []string
		for _, d := range resp.Diagnostics {
			got = append(got, d.Summary)
		}
		return got
	}
	only := func(resp *tfprotov6.PlanResourceChangeResponse, summary string) string {
		if got := summaries(resp); len(got) != 1 || got[0] != summary {
			return fmt.Sprintf("want only %q, got %q", summary, got)
		}
		return ""
	}
	for _, tc := range []struct {
		name          string
		prior         tftypes.Value
		config        tftypes.Value
		closedRefuses bool
	}{
		{"update", state(true, tfString("a"), tfString("us")), config("b", "us"), true},
		{"replace", state(true, tfString("a"), tfString("us")), config("a", "de"), true},
		{"import", state(true, null, null), config("a", "us"), true},
		{"unchanged", state(true, tfString("a"), tfString("us")), config("a", "us"), false},
	} {
		proposed := state(true, valueAt(tc.config, tftypes.NewAttributePath().WithAttributeName("name")), valueAt(tc.config, tftypes.NewAttributePath().WithAttributeName("country")))
		resp := p.planResponse(tc.prior, proposed, tc.config)
		if !tc.closedRefuses {
			if len(resp.Diagnostics) > 0 {
				t.Errorf("%s: a closed object that does not change must plan: %q", tc.name, summaries(resp))
			}
			continue
		}
		if msg := only(resp, "Cannot change a closed stripe_vault"); msg != "" {
			t.Errorf("%s: %s", tc.name, msg)
		} else if !strings.Contains(resp.Diagnostics[0].Detail, "Stripe refuses any change to a closed vault") || !strings.Contains(resp.Diagnostics[0].Detail, "removed {") {
			t.Errorf("%s: the refusal says why and how to stop managing it: %s", tc.name, resp.Diagnostics[0].Detail)
		}
		if len(resp.RequiresReplace) > 0 {
			t.Errorf("%s: a closed object is never replaced: %v", tc.name, resp.RequiresReplace)
		}
	}

	closed := state(true, tfString("a"), tfString("us"))
	if resp := p.planResponse(closed, closed, config("b", "de")); len(resp.Diagnostics) > 0 || len(resp.RequiresReplace) > 0 {
		t.Errorf("ignore_changes = all: a proposal that is the state must plan: %q, %v", summaries(resp), resp.RequiresReplace)
	}

	destroy := tftypes.NewValue(p.typ, nil)
	if msg := only(p.planResponse(closed, destroy, destroy), "Cannot destroy stripe_vault"); msg != "" {
		t.Errorf("destroy: %s", msg)
	}

	open := state(false, tfString("a"), tfString("us"))
	if resp := p.planResponse(open, state(false, tfString("b"), tfString("us")), config("b", "us")); len(resp.Diagnostics) > 0 {
		t.Errorf("the control: an open object updates: %q", summaries(resp))
	}
	if msg := only(p.planResponse(open, state(false, tfString("a"), tfString("de")), config("a", "de")), "Cannot replace stripe_vault"); msg != "" {
		t.Errorf("the control: an open object's replacement keeps its own refusal: %s", msg)
	}
}

// The comparison is the one that keeps a plan that changes nothing empty: a plan of a closed
// object that differs from the state only by computed values the configuration leaves unset
// and the framework marks unknown passes, and plans the state; a configured value not known
// at plan time, such as a reference to an object being replaced, is a change and is refused.
func TestKeepWhenClosedComparesWhatTheConfigurationSets(t *testing.T) {
	decl, table := closedVaultDeclaration()
	r := configured(t, decl, table, noCalls(t))
	state := r.fill(map[string]attr.Value{"id": sv("vlt_1"), "name": sv("a"), "country": sv("us"), "closed": types.BoolValue(true)})

	refresh := r.fill(map[string]attr.Value{"name": sv("a"), "country": sv("us")})
	if !refresh.Attributes()["id"].IsUnknown() || !refresh.Attributes()["closed"].IsUnknown() {
		t.Fatalf("the plan must leave the computed values unknown: %v", refresh)
	}
	resp := r.modifyPlan(t, refresh, state, object(r.objType, map[string]attr.Value{"name": sv("a"), "country": sv("us")}))
	noErrors(t, resp.Diagnostics)
	if !resp.Plan.Raw.Equal(raw(t, state)) {
		t.Errorf("a plan unknown only where the configuration is unset plans the state: %v", resp.Plan.Raw)
	}

	pending := r.fill(map[string]attr.Value{"name": types.StringUnknown(), "country": sv("us")})
	resp = r.modifyPlan(t, pending, state, object(r.objType, map[string]attr.Value{"name": types.StringUnknown(), "country": sv("us")}))
	if len(resp.Diagnostics) != 1 || resp.Diagnostics[0].Summary() != "Cannot change a closed stripe_vault" {
		t.Errorf("a configured value not known yet is a change to a closed object: %v", resp.Diagnostics)
	}
}
