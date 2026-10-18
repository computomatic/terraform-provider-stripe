package engine

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// requiresReplaceForFixedKeys flags a plan that changes what Stripe cannot change in a
// FixedKeys list, and reports whether it did.
func (r *Resource) requiresReplaceForFixedKeys(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) bool {
	if len(r.m.decl.FixedKeys) == 0 {
		return false
	}
	var plan, state basetypes.ObjectValue
	diags := req.Plan.Get(ctx, &plan)
	diags.Append(req.State.Get(ctx, &state)...)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return false
	}
	replace := false
	for _, list := range r.m.decl.FixedKeys {
		n, ok := r.m.top(list)
		if ok && r.m.fixedKeysChanged(n, plan.Attributes()[n.AttrName()], state.Attributes()[n.AttrName()], req.Config.Raw) {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root(n.AttrName()))
			replace = true
		}
	}
	return replace
}

// fixedKeysChanged reports whether the plan of a FixedKeys list adds, drops or reorders into
// a different set of keys, or changes a field of an element that Stripe cannot change in
// place. An element with no key of its own (it sets `price_data`) pairs with a state element
// only when the two are equal, so any change to it counts.
func (m *model) fixedKeysChanged(n *node, plan, state attr.Value, config tftypes.Value) bool {
	if plan == nil || !known(plan) || state == nil || !known(state) {
		return false
	}
	paired, removed := m.matchKeyed(n, listElements(plan), listElements(state), config)
	if len(removed) > 0 {
		return true
	}
	key := m.decl.MatchBy[n.Name]
	for _, e := range paired {
		if e.state == nil {
			return true
		}
		for _, k := range n.Kids {
			if !k.Fixed || k.Name == key {
				continue
			}
			pv, sv := e.plan.Attributes()[k.AttrName()], e.state.Attributes()[k.AttrName()]
			if pv != nil && !pv.IsUnknown() && !pv.Equal(sv) {
				return true
			}
		}
	}
	return false
}

// encodeFixedList builds the update of a FixedKeys list: nothing when no element changed,
// else every element with its `id` and the fields Stripe changes in place.
func (m *model) encodeFixedList(n *node, pv, sv attr.Value, config tftypes.Value) (wire.Value, bool, error) {
	paired, _ := m.matchKeyed(n, listElements(pv), listElements(sv), config)
	changed := false
	for i, e := range paired {
		if e.state == nil || !sameBesidesComputed(tftypes.NewAttributePath().WithAttributeName(n.AttrName()).WithElementKeyInt(i), e.plan, *e.state, config) {
			changed = true
		}
	}
	if !changed {
		return nil, false, nil
	}
	out := make(wire.Array, 0, len(paired))
	for _, e := range paired {
		obj, err := encodeObject(n.Kids, e.plan.Attributes(), func(k *node) bool { return k.Update && !k.Fixed })
		if err != nil {
			return nil, false, err
		}
		if e.state != nil {
			if _, sent := obj.Get("id"); !sent {
				if id, ok := attrKey(*e.state, "id"); ok {
					obj = append(obj, wire.Field{Name: "id", Value: wire.String(id)})
				}
			}
		}
		out = append(out, obj)
	}
	return out, true, nil
}
