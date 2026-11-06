package engine

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// A create-only `discounts`, a subscription's, keeps the list state holds when a read returns
// another, as when Stripe drops a `once` discount that ended or one is added outside Terraform, so
// the next plan neither replaces the object nor shows a diff; only a configuration that changes it
// replaces the object. A list state does not hold yet, as on an import or a create that leaves it
// unset, is read from Stripe.
func TestCreateOnlyDiscountsKeepTheListStateHolds(t *testing.T) {
	table := discountsTable()
	table[2].Update = false
	r := configured(t, planDecl(), table, newStub())
	n := r.m.discounts
	if n == nil || !n.Preserve || n.Replace || !n.ReplaceIfSet {
		t.Fatalf("the create-only discounts node: %+v", n)
	}
	elemType := r.objType.AttrTypes["discounts"].(basetypes.ListType).ElemType.(basetypes.ObjectType)
	elem := func(id string) attr.Value {
		return types.ObjectValueMust(elemType.AttrTypes, map[string]attr.Value{"coupon": types.StringNull(), "discount": types.StringValue(id), "promotion_code": types.StringNull()})
	}
	list := func(elems ...attr.Value) attr.Value { return types.ListValueMust(elemType, elems) }
	decode := func(prior attr.Value, found ...wire.Value) attr.Value {
		t.Helper()
		body := wire.Object{{Name: "id", Value: wire.String("wid_1")}, {Name: "name", Value: wire.String("w")}, {Name: "discounts", Value: wire.Array(found)}}
		got, diags := r.m.decodeState(body, r.fill(map[string]attr.Value{"name": types.StringValue("w"), "discounts": prior}), r.objType, false, nil)
		noErrors(t, diags)
		return got.Attributes()["discounts"]
	}
	held := list(elem("di_x"))
	if got := decode(held); !got.Equal(held) {
		t.Errorf("a discount that ended: %v", got)
	}
	if got := decode(held, discount("di_x", "co_x", wire.Null{}), discount("di_new", "co_new", wire.Null{})); !got.Equal(held) {
		t.Errorf("a discount added outside Terraform: %v", got)
	}
	if got := decode(types.ListNull(elemType), discount("di_x", "co_x", wire.Null{})); !got.Equal(held) {
		t.Errorf("an import: %v", got)
	}

	ctx := context.Background()
	attribute := r.m.resourceSchema(ctx).Attributes["discounts"].(schema.ListNestedAttribute)
	replaces := func(config, state attr.Value) bool {
		t.Helper()
		// The framework's replace modifiers act only on an update: a resource that exists and stays.
		exists := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
		resp := &planmodifier.ListResponse{PlanValue: config.(types.List)}
		for _, mod := range attribute.PlanModifiers {
			mod.PlanModifyList(ctx, planmodifier.ListRequest{
				ConfigValue: config.(types.List), StateValue: state.(types.List), PlanValue: config.(types.List),
				State: tfsdk.State{Raw: exists}, Plan: tfsdk.Plan{Raw: exists},
			}, resp)
		}
		noErrors(t, resp.Diagnostics)
		return resp.RequiresReplace
	}
	if replaces(held, held) {
		t.Error("an unchanged configuration replaces the object")
	}
	if replaces(types.ListNull(elemType), held) {
		t.Error("removing the configuration replaces the object")
	}
	if !replaces(list(elem("di_y")), held) {
		t.Error("a changed configuration does not replace the object")
	}
}
