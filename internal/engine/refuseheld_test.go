package engine

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/computomatic/terraform-provider-stripe/internal/fields"
)

// refusingWhileHeld refuses a change of the fixture's `address` while the state holds its
// create-only `zip`, as a payment intent refuses a change of `payment_method_data` while it holds
// `acss_debit`, on a resource that refuses destroy as an intent does.
func refusingWhileHeld() Declaration {
	decl := fixtureDeclaration()
	decl.Delete, decl.RefuseReason, decl.Paths.Delete = Refuse, "Stripe cannot delete a widget.", ""
	decl.RefuseChangeWhileHeld = map[string]string{"address": "address.zip"}
	return decl
}

// While the state holds the member, a change of the field beside it fails the plan, naming both;
// without the member, or where the member itself changes, which is a replace, the plan passes.
func TestRefuseChangeWhileHeldFailsThePlan(t *testing.T) {
	r := configured(t, refusingWhileHeld(), fixtureTable(), noCalls(t))
	addressType := r.objType.AttrTypes["address"].(basetypes.ObjectType).AttrTypes
	address := func(line1 string, zip attr.Value) attr.Value {
		return types.ObjectValueMust(addressType, map[string]attr.Value{"line1": types.StringValue(line1), "zip": zip})
	}
	state := func(zip attr.Value) basetypes.ObjectValue {
		return r.fill(map[string]attr.Value{"id": types.StringValue("wid_1"), "name": types.StringValue("w"), "address": address("1 Main St", zip)})
	}
	held, none := state(types.StringValue("94080")), state(types.StringNull())
	modify := func(planned attr.Value, prior basetypes.ObjectValue) []string {
		var out []string
		for _, d := range r.modifyPlan(t, r.fill(with(prior, map[string]attr.Value{"address": planned})), prior).Diagnostics.Errors() {
			out = append(out, d.Summary()+" "+d.Detail())
		}
		return out
	}
	if got := modify(address("2 Main St", types.StringValue("94080")), held); len(got) != 1 ||
		!strings.Contains(got[0], "`address` is not supported while `address.zip` is set") || !strings.Contains(got[0], "on create only") ||
		!strings.Contains(got[0], "ignore_changes = [address]") || !strings.Contains(got[0], "removed block") {
		t.Errorf("a change beside the held member must fail the plan and name both: %v", got)
	}
	if got := modify(address("2 Main St", types.StringNull()), none); len(got) != 0 {
		t.Errorf("without the member, a change must pass: %v", got)
	}
	if got := modify(address("2 Main St", types.StringValue("10001")), held); len(got) != 0 {
		t.Errorf("a change of the member itself is the replace it forces, not this refusal: %v", got)
	}
	if got := modify(address("1 Main St", types.StringValue("94080")), held); len(got) != 0 {
		t.Errorf("an unchanged field must pass: %v", got)
	}
	if got := modify(types.ObjectUnknown(addressType), held); len(got) != 0 {
		t.Errorf("a field not known yet is left for apply: %v", got)
	}
}

// The entry names a configurable field and a create-only member of it.
func TestRefuseChangeWhileHeldMustNameACreateOnlyMember(t *testing.T) {
	for name, entry := range map[string]map[string]string{
		"unknown field":      {"nothing": "nothing.zip"},
		"member elsewhere":   {"address": "name"},
		"updatable member":   {"address": "address.line1"},
		"member not present": {"address": "address.none"},
	} {
		decl := refusingWhileHeld()
		decl.RefuseChangeWhileHeld = entry
		if _, err := NewResource(decl, fixtureTable()); err == nil {
			t.Errorf("%s: must not compile", name)
		}
	}
}

// A field the entry names may not be in RefuseChange, which refuses every change of it, nor in
// SendChangedMembers, whose update Stripe takes without the members it leaves out. One in
// SendWholeMembers, whose whole send leaves the create-only member out too, compiles.
func TestRefuseChangeWhileHeldConflicts(t *testing.T) {
	for name, tc := range map[string]struct {
		set  func(*Declaration)
		want string
	}{
		"refuse change":        {func(d *Declaration) { d.RefuseChange = []string{"address"} }, "is in RefuseChange too"},
		"send changed members": {func(d *Declaration) { d.SendChangedMembers = []string{"address"} }, "is in SendChangedMembers"},
		"send whole members":   {func(d *Declaration) { d.SendWholeMembers = []string{"address"} }, ""},
	} {
		decl := refusingWhileHeld()
		tc.set(&decl)
		_, err := NewResource(decl, optionalLine1())
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: must compile: %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: must not compile with %q: %v", name, tc.want, err)
		}
	}
}

// optionalLine1 is the fixture table with `address.line1` optional, so that `address`, which then
// has no Required member, may be sent in part.
func optionalLine1() []fields.Field {
	table := fixtureTable()
	for i := range table {
		if table[i].Name != "address" {
			continue
		}
		for j := range table[i].Fields {
			if table[i].Fields[j].Name == "line1" {
				table[i].Fields[j].Required = false
			}
		}
	}
	return table
}
